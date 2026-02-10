// Package strategies provides the hybrid account selection strategy.
// This file corresponds to src/account-manager/strategies/hybrid-strategy.js in the Node.js version.
package strategies

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/account/strategies/trackers"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// FallbackLevel indicates the level of fallback used in account selection
type FallbackLevel string

const (
	FallbackNormal     FallbackLevel = "normal"
	FallbackQuota      FallbackLevel = "quota"
	FallbackEmergency  FallbackLevel = "emergency"
	FallbackLastResort FallbackLevel = "lastResort"
)

// HybridStrategy provides smart selection based on health score, token bucket, quota, and LRU freshness.
// Combines multiple signals for optimal account distribution.
//
// Scoring formula:
//
//	score = (Health × 2) + ((Tokens / MaxTokens × 100) × 5) + (Quota × 3) + (LRU × 0.1)
type HybridStrategy struct {
	*BaseStrategy
	healthTracker      *trackers.HealthTracker
	tokenBucketTracker *trackers.TokenBucketTracker
	quotaTracker       *trackers.QuotaTracker
	weights            *WeightConfig
	globalThreshold    *float64
}

// NewHybridStrategy creates a new HybridStrategy
func NewHybridStrategy(cfg *Config, redisClient *redis.Client) *HybridStrategy {
	weights := DefaultWeights()
	if cfg != nil && cfg.Weights != nil {
		weights = cfg.Weights
	}

	var healthCfg config.HealthScoreConfig
	var tokenCfg config.TokenBucketConfig
	var quotaCfg config.QuotaConfig

	if cfg != nil {
		healthCfg = cfg.HealthScore
		tokenCfg = cfg.TokenBucket
		quotaCfg = cfg.Quota
	}

	return &HybridStrategy{
		BaseStrategy:       NewBaseStrategy(cfg, redisClient),
		healthTracker:      trackers.NewHealthTracker(healthCfg),
		tokenBucketTracker: trackers.NewTokenBucketTracker(tokenCfg),
		quotaTracker:       trackers.NewQuotaTracker(quotaCfg),
		weights:            weights,
	}
}

// SetGlobalThreshold sets the global quota threshold
func (s *HybridStrategy) SetGlobalThreshold(threshold *float64) {
	s.globalThreshold = threshold
}

// SelectAccount selects an account based on combined health, tokens, and LRU score
func (s *HybridStrategy) SelectAccount(ctx interface{}, accounts []*redis.Account, modelID string, options SelectOptions) *SelectionResult {
	if len(accounts) == 0 {
		return &SelectionResult{Account: nil, Index: 0, WaitMs: 0}
	}

	bgCtx := context.Background()

	// Get candidates that pass all filters
	candidates, fallbackLevel := s.getCandidates(bgCtx, accounts, modelID)

	if len(candidates) == 0 {
		// Diagnose why no candidates are available and compute wait time
		reason, waitMs := s.diagnoseNoCandidates(bgCtx, accounts, modelID)
		utils.Warn("[HybridStrategy] No candidates available: %s", reason)
		return &SelectionResult{Account: nil, Index: 0, WaitMs: waitMs}
	}

	// Score and sort candidates
	type scoredCandidate struct {
		account *redis.Account
		index   int
		score   float64
	}

	scored := make([]scoredCandidate, 0, len(candidates))
	for _, c := range candidates {
		scored = append(scored, scoredCandidate{
			account: c.Account,
			index:   c.Index,
			score:   s.calculateScore(c.Account, modelID),
		})
	}

	// Sort by score descending
	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	// Select the best candidate
	best := scored[0]
	best.account.LastUsed = time.Now().UnixMilli()

	// Consume a token from the bucket (unless in lastResort mode where we bypassed token check)
	if fallbackLevel != FallbackLastResort {
		s.tokenBucketTracker.Consume(best.account.Email)
	}

	if options.OnSave != nil {
		options.OnSave()
	}

	// Calculate throttle wait time based on fallback level
	var waitMs int64
	if fallbackLevel == FallbackLastResort {
		// All accounts exhausted - add significant delay to allow rate limits to clear
		waitMs = 500
	} else if fallbackLevel == FallbackEmergency {
		// All accounts unhealthy - add moderate delay
		waitMs = 250
	}

	position := best.index + 1
	total := len(accounts)
	fallbackInfo := ""
	if fallbackLevel != FallbackNormal {
		fallbackInfo = fmt.Sprintf(", fallback: %s", fallbackLevel)
	}
	utils.Info("[HybridStrategy] Using account: %s (%d/%d, score: %.1f%s)",
		best.account.Email, position, total, best.score, fallbackInfo)

	return &SelectionResult{Account: best.account, Index: best.index, WaitMs: waitMs}
}

// OnSuccess is called after a successful request
func (s *HybridStrategy) OnSuccess(account *redis.Account, modelID string) {
	if account != nil && account.Email != "" {
		s.healthTracker.RecordSuccess(account.Email)
	}
}

// OnRateLimit is called when a request is rate-limited
func (s *HybridStrategy) OnRateLimit(account *redis.Account, modelID string) {
	if account != nil && account.Email != "" {
		s.healthTracker.RecordRateLimit(account.Email)
	}
}

// OnFailure is called when a request fails
func (s *HybridStrategy) OnFailure(account *redis.Account, modelID string) {
	if account != nil && account.Email != "" {
		s.healthTracker.RecordFailure(account.Email)
		// Refund the token since the request didn't complete
		s.tokenBucketTracker.Refund(account.Email)
	}
}

// getCandidates returns candidates that pass all filters
func (s *HybridStrategy) getCandidates(ctx context.Context, accounts []*redis.Account, modelID string) ([]AccountWithIndex, FallbackLevel) {
	candidates := make([]AccountWithIndex, 0)

	for i, account := range accounts {
		// Basic usability check
		if !s.IsAccountUsable(ctx, account, modelID) {
			continue
		}

		// Health score check
		if !s.healthTracker.IsUsable(account.Email) {
			continue
		}

		// Token availability check
		if !s.tokenBucketTracker.HasTokens(account.Email) {
			continue
		}

		// Quota availability check
		effectiveThreshold := s.getEffectiveThreshold(account, modelID)
		if s.quotaTracker.IsQuotaCritical(account, modelID, effectiveThreshold) {
			utils.Debug("[HybridStrategy] Excluding %s: quota critically low for %s (threshold: %v)",
				account.Email, modelID, effectiveThreshold)
			continue
		}

		candidates = append(candidates, AccountWithIndex{Account: account, Index: i})
	}

	if len(candidates) > 0 {
		return candidates, FallbackNormal
	}

	// Fallback: bypass quota check
	fallback := make([]AccountWithIndex, 0)
	for i, account := range accounts {
		if !s.IsAccountUsable(ctx, account, modelID) {
			continue
		}
		if !s.healthTracker.IsUsable(account.Email) {
			continue
		}
		if !s.tokenBucketTracker.HasTokens(account.Email) {
			continue
		}
		fallback = append(fallback, AccountWithIndex{Account: account, Index: i})
	}
	if len(fallback) > 0 {
		utils.Warn("[HybridStrategy] All accounts have critical quota, using fallback")
		return fallback, FallbackQuota
	}

	// Emergency fallback: bypass health check
	emergency := make([]AccountWithIndex, 0)
	for i, account := range accounts {
		if !s.IsAccountUsable(ctx, account, modelID) {
			continue
		}
		if !s.tokenBucketTracker.HasTokens(account.Email) {
			continue
		}
		emergency = append(emergency, AccountWithIndex{Account: account, Index: i})
	}
	if len(emergency) > 0 {
		utils.Warn("[HybridStrategy] EMERGENCY: All accounts unhealthy, using least bad account")
		return emergency, FallbackEmergency
	}

	// Last resort: bypass both health AND token bucket checks
	lastResort := make([]AccountWithIndex, 0)
	for i, account := range accounts {
		if !s.IsAccountUsable(ctx, account, modelID) {
			continue
		}
		lastResort = append(lastResort, AccountWithIndex{Account: account, Index: i})
	}
	if len(lastResort) > 0 {
		utils.Warn("[HybridStrategy] LAST RESORT: All accounts exhausted, using any usable account")
		return lastResort, FallbackLastResort
	}

	return nil, FallbackNormal
}

// getEffectiveThreshold returns the effective quota threshold for an account and model
func (s *HybridStrategy) getEffectiveThreshold(account *redis.Account, modelID string) *float64 {
	// Priority: per-model > per-account > global
	if account.ModelQuotaThresholds != nil {
		if threshold, ok := account.ModelQuotaThresholds[modelID]; ok {
			return &threshold
		}
	}
	if account.QuotaThreshold != nil {
		return account.QuotaThreshold
	}
	return s.globalThreshold
}

// calculateScore calculates the combined score for an account
func (s *HybridStrategy) calculateScore(account *redis.Account, modelID string) float64 {
	email := account.Email

	// Health component (0-100 scaled by weight)
	health := s.healthTracker.GetScore(email)
	healthComponent := health * s.weights.Health

	// Token component (0-100 scaled by weight)
	tokens := s.tokenBucketTracker.GetTokens(email)
	maxTokens := s.tokenBucketTracker.GetMaxTokens()
	tokenRatio := tokens / maxTokens
	tokenComponent := (tokenRatio * 100) * s.weights.Tokens

	// Quota component (0-100 scaled by weight)
	quotaScore := s.quotaTracker.GetScore(account, modelID)
	quotaComponent := quotaScore * s.weights.Quota

	// LRU component (older = higher score)
	lastUsedMs := account.LastUsed
	timeSinceLastUse := time.Now().UnixMilli() - lastUsedMs
	if timeSinceLastUse > 3600000 { // Cap at 1 hour
		timeSinceLastUse = 3600000
	}
	lruSeconds := float64(timeSinceLastUse) / 1000
	lruComponent := lruSeconds * s.weights.LRU

	return healthComponent + tokenComponent + quotaComponent + lruComponent
}

// diagnoseNoCandidates diagnoses why no candidates are available
func (s *HybridStrategy) diagnoseNoCandidates(ctx context.Context, accounts []*redis.Account, modelID string) (string, int64) {
	var unusableCount, unhealthyCount, noTokensCount, criticalQuotaCount int
	accountsWithoutTokens := make([]string, 0)

	for _, account := range accounts {
		if !s.IsAccountUsable(ctx, account, modelID) {
			unusableCount++
			continue
		}
		if !s.healthTracker.IsUsable(account.Email) {
			unhealthyCount++
			continue
		}
		if !s.tokenBucketTracker.HasTokens(account.Email) {
			noTokensCount++
			accountsWithoutTokens = append(accountsWithoutTokens, account.Email)
			continue
		}
		effectiveThreshold := s.getEffectiveThreshold(account, modelID)
		if s.quotaTracker.IsQuotaCritical(account, modelID, effectiveThreshold) {
			criticalQuotaCount++
			continue
		}
	}

	// If all accounts are blocked by token bucket, calculate wait time
	if noTokensCount > 0 && unusableCount == 0 && unhealthyCount == 0 {
		waitMs := s.tokenBucketTracker.GetMinTimeUntilToken(accountsWithoutTokens)
		reason := fmt.Sprintf("all %d account(s) exhausted token bucket, waiting for refill", noTokensCount)
		return reason, waitMs
	}

	// Build reason string
	parts := make([]string, 0)
	if unusableCount > 0 {
		parts = append(parts, fmt.Sprintf("%d unusable/disabled", unusableCount))
	}
	if unhealthyCount > 0 {
		parts = append(parts, fmt.Sprintf("%d unhealthy", unhealthyCount))
	}
	if noTokensCount > 0 {
		parts = append(parts, fmt.Sprintf("%d no tokens", noTokensCount))
	}
	if criticalQuotaCount > 0 {
		parts = append(parts, fmt.Sprintf("%d critical quota", criticalQuotaCount))
	}

	reason := "unknown"
	if len(parts) > 0 {
		reason = strings.Join(parts, ", ")
	}
	return reason, 0
}

// GetHealthTracker returns the health tracker (for testing/debugging)
func (s *HybridStrategy) GetHealthTracker() HealthTracker {
	return s.healthTracker
}

// GetTokenBucketTracker returns the token bucket tracker (for testing/debugging)
func (s *HybridStrategy) GetTokenBucketTracker() *trackers.TokenBucketTracker {
	return s.tokenBucketTracker
}

// GetQuotaTracker returns the quota tracker (for testing/debugging)
func (s *HybridStrategy) GetQuotaTracker() *trackers.QuotaTracker {
	return s.quotaTracker
}

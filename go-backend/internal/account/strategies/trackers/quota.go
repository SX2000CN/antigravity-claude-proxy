// Package trackers provides state tracking for the hybrid strategy.
// This file corresponds to src/account-manager/strategies/trackers/quota-tracker.js in the Node.js version.
package trackers

import (
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// QuotaTracker tracks per-account quota levels to prioritize accounts with available quota.
// Uses quota data from account.Quota.Models[modelID].RemainingFraction.
// Accounts below critical threshold are excluded from selection.
type QuotaTracker struct {
	config config.QuotaConfig
}

// NewQuotaTracker creates a new QuotaTracker with the given configuration
func NewQuotaTracker(cfg config.QuotaConfig) *QuotaTracker {
	// Apply defaults if not set
	if cfg.LowThreshold == 0 {
		cfg.LowThreshold = 0.10 // 10%
	}
	if cfg.CriticalThreshold == 0 {
		cfg.CriticalThreshold = 0.05 // 5%
	}
	if cfg.StaleMs == 0 {
		cfg.StaleMs = 300000 // 5 minutes
	}
	if cfg.UnknownScore == 0 {
		cfg.UnknownScore = 50
	}

	return &QuotaTracker{
		config: cfg,
	}
}

// GetQuotaFraction returns the quota fraction for an account and model
// Returns the remaining fraction (0-1) or -1 if unknown
func (t *QuotaTracker) GetQuotaFraction(account *redis.Account, modelID string) float64 {
	if account == nil || account.Quota == nil || account.Quota.Models == nil {
		return -1
	}

	modelQuota, ok := account.Quota.Models[modelID]
	if !ok || modelQuota == nil {
		return -1
	}

	return modelQuota.RemainingFraction
}

// IsQuotaFresh checks if quota data is fresh enough to be trusted
func (t *QuotaTracker) IsQuotaFresh(account *redis.Account) bool {
	if account == nil || account.Quota == nil || account.Quota.LastChecked == 0 {
		return false
	}

	staleMs := t.config.StaleMs
	lastChecked := time.UnixMilli(account.Quota.LastChecked)
	return time.Since(lastChecked) < time.Duration(staleMs)*time.Millisecond
}

// IsQuotaCritical checks if an account has critically low quota for a model
func (t *QuotaTracker) IsQuotaCritical(account *redis.Account, modelID string, thresholdOverride *float64) bool {
	fraction := t.GetQuotaFraction(account, modelID)

	// Unknown quota = not critical (assume OK)
	if fraction < 0 {
		return false
	}

	// Only apply critical check if data is fresh
	if !t.IsQuotaFresh(account) {
		return false
	}

	threshold := t.config.CriticalThreshold
	if thresholdOverride != nil && *thresholdOverride > 0 {
		threshold = *thresholdOverride
	}

	return fraction <= threshold
}

// IsQuotaLow checks if an account has low (but not critical) quota for a model
func (t *QuotaTracker) IsQuotaLow(account *redis.Account, modelID string) bool {
	fraction := t.GetQuotaFraction(account, modelID)
	if fraction < 0 {
		return false
	}
	return fraction <= t.config.LowThreshold && fraction > t.config.CriticalThreshold
}

// GetScore returns a score (0-100) for an account based on quota
// Higher score = more quota available
func (t *QuotaTracker) GetScore(account *redis.Account, modelID string) float64 {
	fraction := t.GetQuotaFraction(account, modelID)

	// Unknown quota = middle score
	if fraction < 0 {
		return t.config.UnknownScore
	}

	// Convert fraction (0-1) to score (0-100)
	score := fraction * 100

	// Apply small penalty for stale data (reduce confidence)
	if !t.IsQuotaFresh(account) {
		score *= 0.9 // 10% penalty for stale data
	}

	return score
}

// GetCriticalThreshold returns the critical threshold
func (t *QuotaTracker) GetCriticalThreshold() float64 {
	return t.config.CriticalThreshold
}

// GetLowThreshold returns the low threshold
func (t *QuotaTracker) GetLowThreshold() float64 {
	return t.config.LowThreshold
}

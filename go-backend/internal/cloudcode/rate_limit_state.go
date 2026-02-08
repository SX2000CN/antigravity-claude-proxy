// Package cloudcode provides Cloud Code API client implementation.
// This file corresponds to src/cloudcode/rate-limit-state.js in the Node.js version.
package cloudcode

import (
	"math"
	"sync"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// RateLimitState tracks rate limit state per account+model
type RateLimitState struct {
	Consecutive429 int
	LastAt         time.Time
}

// rateLimitStates stores rate limit state per account+model
var rateLimitStates = struct {
	sync.RWMutex
	m map[string]*RateLimitState
}{
	m: make(map[string]*RateLimitState),
}

// BackoffResult contains backoff calculation results
type BackoffResult struct {
	Attempt     int
	DelayMs     int64
	IsDuplicate bool
}

// GetDedupKey returns the deduplication key for rate limit tracking
func GetDedupKey(email, model string) string {
	return email + ":" + model
}

// GetRateLimitBackoff calculates rate limit backoff with deduplication and exponential backoff
func GetRateLimitBackoff(email, model string, serverRetryAfterMs int64) *BackoffResult {
	now := time.Now()
	stateKey := GetDedupKey(email, model)

	rateLimitStates.Lock()
	defer rateLimitStates.Unlock()

	previous := rateLimitStates.m[stateKey]

	// Check if within dedup window - return duplicate status
	if previous != nil && now.Sub(previous.LastAt).Milliseconds() < config.RateLimitDedupWindowMs {
		baseDelay := serverRetryAfterMs
		if baseDelay <= 0 {
			baseDelay = config.FirstRetryDelayMs
		}
		backoffDelay := int64(math.Min(float64(baseDelay)*math.Pow(2, float64(previous.Consecutive429-1)), 60000))
		utils.Debug("[CloudCode] Rate limit on %s:%s within dedup window, attempt=%d, isDuplicate=true",
			email, model, previous.Consecutive429)
		return &BackoffResult{
			Attempt:     previous.Consecutive429,
			DelayMs:     max64(baseDelay, backoffDelay),
			IsDuplicate: true,
		}
	}

	// Determine attempt number - reset after RATE_LIMIT_STATE_RESET_MS of inactivity
	attempt := 1
	if previous != nil && now.Sub(previous.LastAt).Milliseconds() < config.RateLimitStateResetMs {
		attempt = previous.Consecutive429 + 1
	}

	// Update state
	rateLimitStates.m[stateKey] = &RateLimitState{
		Consecutive429: attempt,
		LastAt:         now,
	}

	// Calculate exponential backoff
	baseDelay := serverRetryAfterMs
	if baseDelay <= 0 {
		baseDelay = config.FirstRetryDelayMs
	}
	backoffDelay := int64(math.Min(float64(baseDelay)*math.Pow(2, float64(attempt-1)), 60000))

	utils.Debug("[CloudCode] Rate limit backoff for %s:%s: attempt=%d, delayMs=%d",
		email, model, attempt, max64(baseDelay, backoffDelay))
	return &BackoffResult{
		Attempt:     attempt,
		DelayMs:     max64(baseDelay, backoffDelay),
		IsDuplicate: false,
	}
}

// ClearRateLimitState clears rate limit state after successful request
func ClearRateLimitState(email, model string) {
	key := GetDedupKey(email, model)
	rateLimitStates.Lock()
	delete(rateLimitStates.m, key)
	rateLimitStates.Unlock()
}

// IsPermanentAuthFailure detects permanent authentication failures that require re-authentication
func IsPermanentAuthFailure(errorText string) bool {
	lower := utils.ToLower(errorText)
	return utils.ContainsAny(lower,
		"invalid_grant",
		"token revoked",
		"token has been expired or revoked",
		"token_revoked",
		"invalid_client",
		"credentials are invalid")
}

// IsModelCapacityExhausted detects if 429 error is due to model capacity (not user quota)
func IsModelCapacityExhausted(errorText string) bool {
	lower := utils.ToLower(errorText)
	return utils.ContainsAny(lower,
		"model_capacity_exhausted",
		"capacity_exhausted",
		"model is currently overloaded",
		"service temporarily unavailable")
}

// CalculateSmartBackoff calculates smart backoff based on error type
func CalculateSmartBackoff(errorText string, serverResetMs int64, consecutiveFailures int) int64 {
	// If server provides a reset time, use it (with minimum floor to prevent loops)
	if serverResetMs > 0 {
		return max64(serverResetMs, config.MinBackoffMs)
	}

	reason := ParseRateLimitReason(errorText, 0)

	switch reason {
	case RateLimitReasonQuotaExhausted:
		// Progressive backoff: [60s, 5m, 30m, 2h]
		tierIndex := min(consecutiveFailures, len(config.QuotaExhaustedBackoffTiersMs)-1)
		return config.QuotaExhaustedBackoffTiersMs[tierIndex]
	case RateLimitReasonRateLimitExceeded:
		return config.BackoffByErrorType["RATE_LIMIT_EXCEEDED"]
	case RateLimitReasonModelCapacityExhausted:
		// Apply jitter to prevent thundering herd
		return config.BackoffByErrorType["MODEL_CAPACITY_EXHAUSTED"] + utils.GenerateJitter(config.CapacityJitterMaxMs)
	case RateLimitReasonServerError:
		return config.BackoffByErrorType["SERVER_ERROR"]
	default:
		return config.BackoffByErrorType["UNKNOWN"]
	}
}

// StartRateLimitStateCleanup starts periodic cleanup of stale rate limit state
func StartRateLimitStateCleanup() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			cleanupStaleRateLimitStates()
		}
	}()
}

// cleanupStaleRateLimitStates removes stale rate limit state entries
func cleanupStaleRateLimitStates() {
	cutoff := time.Now().Add(-time.Duration(config.RateLimitStateResetMs) * time.Millisecond)

	rateLimitStates.Lock()
	defer rateLimitStates.Unlock()

	for key, state := range rateLimitStates.m {
		if state.LastAt.Before(cutoff) {
			delete(rateLimitStates.m, key)
		}
	}
}

// Helper functions
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

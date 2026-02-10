// Package trackers provides state tracking for the hybrid strategy.
// This file corresponds to src/account-manager/strategies/trackers/token-bucket-tracker.js in the Node.js version.
package trackers

import (
	"math"
	"sync"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
)

// TokenBucket stores token bucket state for an account
type TokenBucket struct {
	Tokens      float64
	LastUpdated time.Time
}

// TokenBucketTracker provides client-side rate limiting using the token bucket algorithm.
// Each account has a bucket of tokens that regenerate over time.
// Requests consume tokens; accounts without tokens are deprioritized.
type TokenBucketTracker struct {
	mu      sync.RWMutex
	buckets map[string]*TokenBucket
	config  config.TokenBucketConfig
}

// NewTokenBucketTracker creates a new TokenBucketTracker with the given configuration
func NewTokenBucketTracker(cfg config.TokenBucketConfig) *TokenBucketTracker {
	// Apply defaults if not set
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 50
	}
	if cfg.TokensPerMinute == 0 {
		cfg.TokensPerMinute = 6
	}
	if cfg.InitialTokens == 0 {
		cfg.InitialTokens = 50
	}

	return &TokenBucketTracker{
		buckets: make(map[string]*TokenBucket),
		config:  cfg,
	}
}

// GetTokens returns the current token count for an account (with regeneration applied)
func (t *TokenBucketTracker) GetTokens(email string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.getTokensUnlocked(email)
}

// getTokensUnlocked returns tokens without taking the lock (must be called with lock held)
func (t *TokenBucketTracker) getTokensUnlocked(email string) float64 {
	bucket, ok := t.buckets[email]
	if !ok {
		return t.config.InitialTokens
	}

	// Apply token regeneration based on time elapsed
	minutesElapsed := time.Since(bucket.LastUpdated).Minutes()
	regenerated := minutesElapsed * t.config.TokensPerMinute
	currentTokens := bucket.Tokens + regenerated

	if currentTokens > t.config.MaxTokens {
		return t.config.MaxTokens
	}
	return currentTokens
}

// HasTokens checks if an account has tokens available
func (t *TokenBucketTracker) HasTokens(email string) bool {
	return t.GetTokens(email) >= 1
}

// Consume consumes a token from an account's bucket
// Returns true if token was consumed, false if no tokens available
func (t *TokenBucketTracker) Consume(email string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	currentTokens := t.getTokensUnlocked(email)
	if currentTokens < 1 {
		return false
	}

	t.buckets[email] = &TokenBucket{
		Tokens:      currentTokens - 1,
		LastUpdated: time.Now(),
	}
	return true
}

// Refund refunds a token to an account's bucket (e.g., on request failure before processing)
func (t *TokenBucketTracker) Refund(email string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	currentTokens := t.getTokensUnlocked(email)
	newTokens := currentTokens + 1
	if newTokens > t.config.MaxTokens {
		newTokens = t.config.MaxTokens
	}

	t.buckets[email] = &TokenBucket{
		Tokens:      newTokens,
		LastUpdated: time.Now(),
	}
}

// GetMaxTokens returns the maximum token capacity
func (t *TokenBucketTracker) GetMaxTokens() float64 {
	return t.config.MaxTokens
}

// Reset resets the bucket for an account
func (t *TokenBucketTracker) Reset(email string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.buckets[email] = &TokenBucket{
		Tokens:      t.config.InitialTokens,
		LastUpdated: time.Now(),
	}
}

// Clear clears all tracked buckets
func (t *TokenBucketTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buckets = make(map[string]*TokenBucket)
}

// GetTimeUntilNextToken returns time in milliseconds until next token is available for an account
func (t *TokenBucketTracker) GetTimeUntilNextToken(email string) int64 {
	currentTokens := t.GetTokens(email)
	if currentTokens >= 1 {
		return 0
	}

	// Calculate time to regenerate 1 token
	tokensNeeded := 1 - currentTokens
	minutesNeeded := tokensNeeded / t.config.TokensPerMinute
	return int64(math.Ceil(minutesNeeded * 60 * 1000))
}

// GetMinTimeUntilToken returns the minimum time until any account in the list has a token
func (t *TokenBucketTracker) GetMinTimeUntilToken(emails []string) int64 {
	if len(emails) == 0 {
		return 0
	}

	minWait := int64(math.MaxInt64)
	for _, email := range emails {
		wait := t.GetTimeUntilNextToken(email)
		if wait == 0 {
			return 0
		}
		if wait < minWait {
			minWait = wait
		}
	}

	if minWait == int64(math.MaxInt64) {
		return 0
	}
	return minWait
}

// GetAllBuckets returns all token buckets (for debugging/status)
func (t *TokenBucketTracker) GetAllBuckets() map[string]float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]float64)
	for email := range t.buckets {
		result[email] = t.getTokensUnlocked(email)
	}
	return result
}

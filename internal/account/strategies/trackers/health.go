// Package trackers provides state tracking for the hybrid strategy.
// This file corresponds to src/account-manager/strategies/trackers/health-tracker.js in the Node.js version.
package trackers

import (
	"sync"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
)

// HealthRecord stores health state for an account
type HealthRecord struct {
	Score               float64
	LastUpdated         time.Time
	ConsecutiveFailures int
}

// HealthTracker tracks per-account health scores to prioritize healthy accounts.
// Scores increase on success and decrease on failures/rate limits.
// Passive recovery over time helps accounts recover from temporary issues.
type HealthTracker struct {
	mu      sync.RWMutex
	scores  map[string]*HealthRecord
	config  config.HealthScoreConfig
}

// NewHealthTracker creates a new HealthTracker with the given configuration
func NewHealthTracker(cfg config.HealthScoreConfig) *HealthTracker {
	// Apply defaults if not set
	if cfg.Initial == 0 {
		cfg.Initial = 70
	}
	if cfg.SuccessReward == 0 {
		cfg.SuccessReward = 1
	}
	if cfg.RateLimitPenalty == 0 {
		cfg.RateLimitPenalty = -10
	}
	if cfg.FailurePenalty == 0 {
		cfg.FailurePenalty = -20
	}
	if cfg.RecoveryPerHour == 0 {
		cfg.RecoveryPerHour = 10
	}
	if cfg.MinUsable == 0 {
		cfg.MinUsable = 50
	}
	if cfg.MaxScore == 0 {
		cfg.MaxScore = 100
	}

	return &HealthTracker{
		scores: make(map[string]*HealthRecord),
		config: cfg,
	}
}

// GetScore returns the health score for an account (with passive recovery applied)
func (t *HealthTracker) GetScore(email string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	record, ok := t.scores[email]
	if !ok {
		return t.config.Initial
	}

	// Apply passive recovery based on time elapsed
	hoursElapsed := time.Since(record.LastUpdated).Hours()
	recovery := hoursElapsed * t.config.RecoveryPerHour
	recoveredScore := record.Score + recovery

	if recoveredScore > t.config.MaxScore {
		return t.config.MaxScore
	}
	return recoveredScore
}

// GetHealthScore is an alias for GetScore (for interface compatibility)
func (t *HealthTracker) GetHealthScore(email string) float64 {
	return t.GetScore(email)
}

// RecordSuccess records a successful request for an account
func (t *HealthTracker) RecordSuccess(email string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	currentScore := t.getScoreUnlocked(email)
	newScore := currentScore + t.config.SuccessReward
	if newScore > t.config.MaxScore {
		newScore = t.config.MaxScore
	}

	t.scores[email] = &HealthRecord{
		Score:               newScore,
		LastUpdated:         time.Now(),
		ConsecutiveFailures: 0, // Reset on success
	}
}

// RecordRateLimit records a rate limit for an account
func (t *HealthTracker) RecordRateLimit(email string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	record := t.scores[email]
	currentScore := t.getScoreUnlocked(email)
	newScore := currentScore + t.config.RateLimitPenalty
	if newScore < 0 {
		newScore = 0
	}

	consecutiveFailures := 0
	if record != nil {
		consecutiveFailures = record.ConsecutiveFailures
	}

	t.scores[email] = &HealthRecord{
		Score:               newScore,
		LastUpdated:         time.Now(),
		ConsecutiveFailures: consecutiveFailures + 1,
	}
}

// RecordFailure records a failure for an account
func (t *HealthTracker) RecordFailure(email string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	record := t.scores[email]
	currentScore := t.getScoreUnlocked(email)
	newScore := currentScore + t.config.FailurePenalty
	if newScore < 0 {
		newScore = 0
	}

	consecutiveFailures := 0
	if record != nil {
		consecutiveFailures = record.ConsecutiveFailures
	}

	t.scores[email] = &HealthRecord{
		Score:               newScore,
		LastUpdated:         time.Now(),
		ConsecutiveFailures: consecutiveFailures + 1,
	}
}

// IsUsable checks if an account is usable based on health score
func (t *HealthTracker) IsUsable(email string) bool {
	return t.GetScore(email) >= t.config.MinUsable
}

// GetMinUsable returns the minimum usable score threshold
func (t *HealthTracker) GetMinUsable() float64 {
	return t.config.MinUsable
}

// GetMaxScore returns the maximum score cap
func (t *HealthTracker) GetMaxScore() float64 {
	return t.config.MaxScore
}

// Reset resets the score for an account
func (t *HealthTracker) Reset(email string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.scores[email] = &HealthRecord{
		Score:               t.config.Initial,
		LastUpdated:         time.Now(),
		ConsecutiveFailures: 0,
	}
}

// GetConsecutiveFailures returns the consecutive failure count for an account
func (t *HealthTracker) GetConsecutiveFailures(email string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if record, ok := t.scores[email]; ok {
		return record.ConsecutiveFailures
	}
	return 0
}

// Clear clears all tracked scores
func (t *HealthTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.scores = make(map[string]*HealthRecord)
}

// getScoreUnlocked returns the score without taking the lock (must be called with lock held)
func (t *HealthTracker) getScoreUnlocked(email string) float64 {
	record, ok := t.scores[email]
	if !ok {
		return t.config.Initial
	}

	hoursElapsed := time.Since(record.LastUpdated).Hours()
	recovery := hoursElapsed * t.config.RecoveryPerHour
	recoveredScore := record.Score + recovery

	if recoveredScore > t.config.MaxScore {
		return t.config.MaxScore
	}
	return recoveredScore
}

// GetAllRecords returns all health records (for debugging/status)
func (t *HealthTracker) GetAllRecords() map[string]*HealthRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]*HealthRecord)
	for email, record := range t.scores {
		result[email] = &HealthRecord{
			Score:               t.GetScore(email),
			LastUpdated:         record.LastUpdated,
			ConsecutiveFailures: record.ConsecutiveFailures,
		}
	}
	return result
}

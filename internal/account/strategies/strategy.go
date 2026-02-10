// Package strategies provides account selection strategies for the proxy.
// This file corresponds to src/account-manager/strategies/index.js in the Node.js version.
package strategies

import (
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// Strategy names
const (
	StrategySticky     = "sticky"
	StrategyRoundRobin = "round-robin"
	StrategyHybrid     = "hybrid"
)

// Strategy labels for display
var StrategyLabels = map[string]string{
	StrategySticky:     "Sticky (Cache-Optimized)",
	StrategyRoundRobin: "Round-Robin (Load-Balanced)",
	StrategyHybrid:     "Hybrid (Smart Distribution)",
}

// SelectOptions contains options for account selection
type SelectOptions struct {
	CurrentIndex int
	SessionID    string
	OnSave       func()
}

// SelectionResult represents the result of account selection
type SelectionResult struct {
	Account *redis.Account
	Index   int
	WaitMs  int64
}

// Strategy defines the interface for account selection strategies
type Strategy interface {
	// SelectAccount selects an account for a request
	SelectAccount(ctx interface{}, accounts []*redis.Account, modelID string, options SelectOptions) *SelectionResult

	// OnSuccess is called after a successful request
	OnSuccess(account *redis.Account, modelID string)

	// OnRateLimit is called when a request is rate-limited
	OnRateLimit(account *redis.Account, modelID string)

	// OnFailure is called when a request fails (non-rate-limit error)
	OnFailure(account *redis.Account, modelID string)
}

// HealthTracker interface for hybrid strategy health tracking
type HealthTracker interface {
	GetScore(email string) float64
	GetHealthScore(email string) float64 // alias for GetScore
	GetMinUsable() float64
	GetMaxScore() float64
	GetConsecutiveFailures(email string) int
	IsUsable(email string) bool
	RecordSuccess(email string)
	RecordRateLimit(email string)
	RecordFailure(email string)
	Reset(email string)
	Clear()
}

// Config holds configuration for strategies
type Config struct {
	HealthScore config.HealthScoreConfig
	TokenBucket config.TokenBucketConfig
	Quota       config.QuotaConfig
	Weights     *WeightConfig
}

// WeightConfig holds scoring weights for hybrid strategy
type WeightConfig struct {
	Health float64
	Tokens float64
	Quota  float64
	LRU    float64
}

// DefaultWeights returns the default scoring weights
func DefaultWeights() *WeightConfig {
	return &WeightConfig{
		Health: 2.0,
		Tokens: 5.0,
		Quota:  3.0,
		LRU:    0.1,
	}
}

// NewStrategy creates a strategy instance based on the strategy name
func NewStrategy(strategyName string, cfg *Config, redisClient *redis.Client) Strategy {
	name := strategyName
	if name == "" {
		name = config.DefaultSelectionStrategy
	}

	switch name {
	case StrategySticky:
		utils.Debug("[Strategy] Creating StickyStrategy")
		return NewStickyStrategy(cfg)

	case StrategyRoundRobin, "roundrobin":
		utils.Debug("[Strategy] Creating RoundRobinStrategy")
		return NewRoundRobinStrategy(cfg)

	case StrategyHybrid:
		utils.Debug("[Strategy] Creating HybridStrategy")
		return NewHybridStrategy(cfg, redisClient)

	default:
		utils.Warn("[Strategy] Unknown strategy \"%s\", falling back to %s", strategyName, config.DefaultSelectionStrategy)
		return NewHybridStrategy(cfg, redisClient)
	}
}

// IsValidStrategy checks if a strategy name is valid
func IsValidStrategy(name string) bool {
	if name == "" {
		return false
	}
	switch name {
	case StrategySticky, StrategyRoundRobin, StrategyHybrid, "roundrobin":
		return true
	default:
		return false
	}
}

// GetStrategyLabel returns the display label for a strategy
func GetStrategyLabel(name string) string {
	if name == "" {
		name = config.DefaultSelectionStrategy
	}
	if name == "roundrobin" {
		return StrategyLabels[StrategyRoundRobin]
	}
	if label, ok := StrategyLabels[name]; ok {
		return label
	}
	return StrategyLabels[config.DefaultSelectionStrategy]
}

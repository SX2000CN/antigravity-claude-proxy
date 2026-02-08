// Package strategies provides base strategy functionality.
// This file corresponds to src/account-manager/strategies/base-strategy.js in the Node.js version.
package strategies

import (
	"context"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// BaseStrategy provides common functionality for all strategies
type BaseStrategy struct {
	config       *Config
	redisClient  *redis.Client
	accountStore *redis.AccountStore
}

// NewBaseStrategy creates a new BaseStrategy
func NewBaseStrategy(cfg *Config, redisClient *redis.Client) *BaseStrategy {
	var accountStore *redis.AccountStore
	if redisClient != nil {
		accountStore = redis.NewAccountStore(redisClient)
	}
	return &BaseStrategy{
		config:       cfg,
		redisClient:  redisClient,
		accountStore: accountStore,
	}
}

// IsAccountUsable checks if an account is usable for a specific model
func (s *BaseStrategy) IsAccountUsable(ctx context.Context, account *redis.Account, modelID string) bool {
	if account == nil || account.IsInvalid {
		return false
	}

	// Skip disabled accounts
	if !account.Enabled {
		return false
	}

	// Check if account is cooling down
	if s.IsAccountCoolingDown(account) {
		return false
	}

	// Check model-specific rate limit from Redis
	if modelID != "" && s.accountStore != nil {
		info, err := s.accountStore.GetRateLimit(ctx, account.Email, modelID)
		if err == nil && info != nil && info.IsRateLimited {
			if info.ResetTime > 0 && time.Now().Before(time.UnixMilli(info.ResetTime)) {
				return false
			}
		}
	}

	return true
}

// IsAccountCoolingDown checks if an account is currently cooling down
func (s *BaseStrategy) IsAccountCoolingDown(account *redis.Account) bool {
	if account == nil || account.CoolingDownUntil == 0 {
		return false
	}

	if time.Now().After(time.UnixMilli(account.CoolingDownUntil)) {
		// Cooldown expired - clear it
		account.CoolingDownUntil = 0
		account.CooldownReason = ""
		return false
	}

	return true
}

// GetUsableAccounts returns all usable accounts for a model with their original indices
func (s *BaseStrategy) GetUsableAccounts(ctx context.Context, accounts []*redis.Account, modelID string) []AccountWithIndex {
	result := make([]AccountWithIndex, 0)
	for i, account := range accounts {
		if s.IsAccountUsable(ctx, account, modelID) {
			result = append(result, AccountWithIndex{Account: account, Index: i})
		}
	}
	return result
}

// AccountWithIndex represents an account with its original index
type AccountWithIndex struct {
	Account *redis.Account
	Index   int
}

// OnSuccess is called after a successful request (default: no-op)
func (s *BaseStrategy) OnSuccess(account *redis.Account, modelID string) {
	// Default: no-op, override in subclass if needed
}

// OnRateLimit is called when a request is rate-limited (default: no-op)
func (s *BaseStrategy) OnRateLimit(account *redis.Account, modelID string) {
	// Default: no-op, override in subclass if needed
}

// OnFailure is called when a request fails (default: no-op)
func (s *BaseStrategy) OnFailure(account *redis.Account, modelID string) {
	// Default: no-op, override in subclass if needed
}

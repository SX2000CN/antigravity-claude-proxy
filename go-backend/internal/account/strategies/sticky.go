// Package strategies provides the sticky account selection strategy.
// This file corresponds to src/account-manager/strategies/sticky-strategy.js in the Node.js version.
package strategies

import (
	"context"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// StickyStrategy keeps using the same account until it becomes unavailable.
// Best for prompt caching as it maintains cache continuity across requests.
type StickyStrategy struct {
	*BaseStrategy
}

// NewStickyStrategy creates a new StickyStrategy
func NewStickyStrategy(cfg *Config) *StickyStrategy {
	return &StickyStrategy{
		BaseStrategy: NewBaseStrategy(cfg, nil),
	}
}

// SelectAccount selects an account with sticky preference.
// Prefers the current account for cache continuity, only switches when:
// - Current account is rate-limited for > 2 minutes
// - Current account is invalid
// - Current account is disabled
func (s *StickyStrategy) SelectAccount(ctx interface{}, accounts []*redis.Account, modelID string, options SelectOptions) *SelectionResult {
	if len(accounts) == 0 {
		return &SelectionResult{Account: nil, Index: options.CurrentIndex, WaitMs: 0}
	}

	// Clamp index to valid range
	index := options.CurrentIndex
	if index >= len(accounts) {
		index = 0
	}

	currentAccount := accounts[index]
	bgCtx := context.Background()

	// Check if current account is usable
	if s.IsAccountUsable(bgCtx, currentAccount, modelID) {
		currentAccount.LastUsed = time.Now().UnixMilli()
		if options.OnSave != nil {
			options.OnSave()
		}
		return &SelectionResult{Account: currentAccount, Index: index, WaitMs: 0}
	}

	// Current account is not usable - check if others are available
	usableAccounts := s.GetUsableAccounts(bgCtx, accounts, modelID)

	if len(usableAccounts) > 0 {
		// Found a free account - switch immediately
		nextAccount, nextIndex := s.pickNext(bgCtx, accounts, index, modelID, options.OnSave)
		if nextAccount != nil {
			utils.Info("[StickyStrategy] Switched to new account (failover): %s", nextAccount.Email)
			return &SelectionResult{Account: nextAccount, Index: nextIndex, WaitMs: 0}
		}
	}

	// No other accounts available - check if we should wait for current
	shouldWait, waitMs := s.shouldWaitForAccount(bgCtx, currentAccount, modelID)
	if shouldWait {
		utils.Info("[StickyStrategy] Waiting %s for sticky account: %s",
			utils.FormatDuration(waitMs), currentAccount.Email)
		return &SelectionResult{Account: nil, Index: index, WaitMs: waitMs}
	}

	// Current account unavailable for too long, try to find any other
	nextAccount, nextIndex := s.pickNext(bgCtx, accounts, index, modelID, options.OnSave)
	return &SelectionResult{Account: nextAccount, Index: nextIndex, WaitMs: 0}
}

// pickNext picks the next available account starting from after the current index
func (s *StickyStrategy) pickNext(ctx context.Context, accounts []*redis.Account, currentIndex int, modelID string, onSave func()) (*redis.Account, int) {
	for i := 1; i <= len(accounts); i++ {
		idx := (currentIndex + i) % len(accounts)
		account := accounts[idx]

		if s.IsAccountUsable(ctx, account, modelID) {
			account.LastUsed = time.Now().UnixMilli()
			if onSave != nil {
				onSave()
			}

			position := idx + 1
			total := len(accounts)
			utils.Info("[StickyStrategy] Using account: %s (%d/%d)", account.Email, position, total)

			return account, idx
		}
	}

	return nil, currentIndex
}

// shouldWaitForAccount checks if we should wait for an account's rate limit to reset
func (s *StickyStrategy) shouldWaitForAccount(ctx context.Context, account *redis.Account, modelID string) (bool, int64) {
	if account == nil || account.IsInvalid || !account.Enabled {
		return false, 0
	}

	var waitMs int64

	if modelID != "" && s.accountStore != nil {
		info, err := s.accountStore.GetRateLimit(ctx, account.Email, modelID)
		if err == nil && info != nil && info.IsRateLimited && info.ResetTime > 0 {
			waitMs = info.ResetTime - time.Now().UnixMilli()
		}
	}

	// Wait if within threshold (2 minutes)
	if waitMs > 0 && waitMs <= config.MaxWaitBeforeErrorMs {
		return true, waitMs
	}

	return false, 0
}

// OnSuccess is called after a successful request
func (s *StickyStrategy) OnSuccess(account *redis.Account, modelID string) {
	// StickyStrategy doesn't track health scores
}

// OnRateLimit is called when a request is rate-limited
func (s *StickyStrategy) OnRateLimit(account *redis.Account, modelID string) {
	// StickyStrategy doesn't track health scores
}

// OnFailure is called when a request fails
func (s *StickyStrategy) OnFailure(account *redis.Account, modelID string) {
	// StickyStrategy doesn't track health scores
}

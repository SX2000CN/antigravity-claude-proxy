// Package strategies provides the round-robin account selection strategy.
// This file corresponds to src/account-manager/strategies/round-robin-strategy.js in the Node.js version.
package strategies

import (
	"context"
	"sync"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// RoundRobinStrategy rotates to the next account on every request for maximum throughput.
// Does not maintain cache continuity but maximizes concurrent requests.
type RoundRobinStrategy struct {
	*BaseStrategy
	mu     sync.Mutex
	cursor int
}

// NewRoundRobinStrategy creates a new RoundRobinStrategy
func NewRoundRobinStrategy(cfg *Config) *RoundRobinStrategy {
	return &RoundRobinStrategy{
		BaseStrategy: NewBaseStrategy(cfg, nil),
		cursor:       0,
	}
}

// SelectAccount selects the next available account in rotation
func (s *RoundRobinStrategy) SelectAccount(ctx interface{}, accounts []*redis.Account, modelID string, options SelectOptions) *SelectionResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(accounts) == 0 {
		return &SelectionResult{Account: nil, Index: 0, WaitMs: 0}
	}

	// Clamp cursor to valid range
	if s.cursor >= len(accounts) {
		s.cursor = 0
	}

	// Start from the next position after the cursor
	startIndex := (s.cursor + 1) % len(accounts)
	bgCtx := context.Background()

	// Try each account starting from startIndex
	for i := 0; i < len(accounts); i++ {
		idx := (startIndex + i) % len(accounts)
		account := accounts[idx]

		if s.IsAccountUsable(bgCtx, account, modelID) {
			account.LastUsed = time.Now().UnixMilli()
			s.cursor = idx

			if options.OnSave != nil {
				options.OnSave()
			}

			position := idx + 1
			total := len(accounts)
			utils.Info("[RoundRobinStrategy] Using account: %s (%d/%d)", account.Email, position, total)

			return &SelectionResult{Account: account, Index: idx, WaitMs: 0}
		}
	}

	// No usable accounts found
	return &SelectionResult{Account: nil, Index: s.cursor, WaitMs: 0}
}

// ResetCursor resets the cursor position
func (s *RoundRobinStrategy) ResetCursor() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursor = 0
}

// OnSuccess is called after a successful request
func (s *RoundRobinStrategy) OnSuccess(account *redis.Account, modelID string) {
	// RoundRobinStrategy doesn't track health scores
}

// OnRateLimit is called when a request is rate-limited
func (s *RoundRobinStrategy) OnRateLimit(account *redis.Account, modelID string) {
	// RoundRobinStrategy doesn't track health scores
}

// OnFailure is called when a request fails
func (s *RoundRobinStrategy) OnFailure(account *redis.Account, modelID string) {
	// RoundRobinStrategy doesn't track health scores
}

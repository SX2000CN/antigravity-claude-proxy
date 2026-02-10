// Package account provides account management with configurable selection strategies.
// This file corresponds to src/account-manager/index.js in the Node.js version.
package account

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/account/strategies"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// Manager manages multiple Antigravity accounts with configurable selection strategies
type Manager struct {
	mu sync.RWMutex

	// Redis storage
	redisClient  *redis.Client
	accountStore *redis.AccountStore

	// Account state
	accounts     []*redis.Account
	currentIndex int
	settings     map[string]interface{}
	initialized  bool

	// Credentials manager (handles token caching with TTL)
	credentials *Credentials

	// Strategy
	strategy     strategies.Strategy
	strategyName string

	// Configuration
	config *config.Config
}

// NewManager creates a new account manager
func NewManager(redisClient *redis.Client, cfg *config.Config) *Manager {
	return &Manager{
		redisClient:  redisClient,
		accountStore: redis.NewAccountStore(redisClient),
		accounts:     make([]*redis.Account, 0),
		settings:     make(map[string]interface{}),
		credentials:  NewCredentials(redisClient),
		strategyName: config.DefaultSelectionStrategy,
		config:       cfg,
	}
}

// Initialize initializes the account manager by loading config
func (m *Manager) Initialize(ctx context.Context, strategyOverride string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized {
		return nil
	}

	// Load accounts from Redis
	accounts, err := m.accountStore.ListAccounts(ctx)
	if err != nil {
		utils.Warn("[AccountManager] Failed to load accounts: %v", err)
		accounts = make([]*redis.Account, 0)
	}

	m.accounts = accounts

	// Determine strategy: CLI override > env var > config file > default
	configStrategy := m.config.GetStrategy()
	if strategyOverride != "" {
		m.strategyName = strategyOverride
	} else if configStrategy != "" {
		m.strategyName = configStrategy
	}

	// Create the strategy instance
	strategyConfig := &strategies.Config{
		Weights: strategies.DefaultWeights(),
	}
	if m.config.AccountSelection.HealthScore != nil {
		strategyConfig.HealthScore = *m.config.AccountSelection.HealthScore
	}
	if m.config.AccountSelection.TokenBucket != nil {
		strategyConfig.TokenBucket = *m.config.AccountSelection.TokenBucket
	}
	if m.config.AccountSelection.Quota != nil {
		strategyConfig.Quota = *m.config.AccountSelection.Quota
	}
	m.strategy = strategies.NewStrategy(m.strategyName, strategyConfig, m.redisClient)
	utils.Info("[AccountManager] Using %s selection strategy", strategies.GetStrategyLabel(m.strategyName))

	// Clear any expired rate limits
	m.clearExpiredLimitsLocked(ctx)

	m.initialized = true
	return nil
}

// Reload reloads accounts from storage
func (m *Manager) Reload(ctx context.Context) error {
	m.mu.Lock()
	m.initialized = false
	m.mu.Unlock()

	err := m.Initialize(ctx, "")
	if err == nil {
		utils.Info("[AccountManager] Accounts reloaded from storage")
	}
	return err
}

// GetAccountCount returns the number of accounts
func (m *Manager) GetAccountCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.accounts)
}

// GetAllAccounts returns all accounts
func (m *Manager) GetAllAccounts() []*redis.Account {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*redis.Account, len(m.accounts))
	copy(result, m.accounts)
	return result
}

// SelectAccount selects an account based on the current strategy
func (m *Manager) SelectAccount(ctx context.Context, modelID string, options SelectOptions) (*SelectionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.initialized {
		return nil, NewNotInitializedError()
	}

	if len(m.accounts) == 0 {
		return nil, NewNoAccountsError("No accounts configured", false)
	}

	// Clear expired rate limits first
	m.clearExpiredLimitsLocked(ctx)

	// Delegate to strategy
	result := m.strategy.SelectAccount(ctx, m.accounts, modelID, strategies.SelectOptions{
		CurrentIndex: m.currentIndex,
		SessionID:    options.SessionID,
		OnSave:       func() { m.saveToDiskLocked(ctx) },
	})

	if result.Account == nil {
		allRateLimited := m.isAllRateLimitedLocked(modelID)
		return nil, NewNoAccountsError("No available accounts", allRateLimited)
	}

	m.currentIndex = result.Index

	return &SelectionResult{
		Account: result.Account,
		Index:   result.Index,
		WaitMs:  result.WaitMs,
	}, nil
}

// IsAllRateLimited checks if all accounts are rate-limited
func (m *Manager) IsAllRateLimited(modelID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isAllRateLimitedLocked(modelID)
}

func (m *Manager) isAllRateLimitedLocked(modelID string) bool {
	for _, acc := range m.accounts {
		if !acc.Enabled || acc.IsInvalid {
			continue
		}
		if !m.isRateLimitedForModel(acc, modelID) {
			return false
		}
	}
	return true
}

// GetAvailableAccounts returns accounts that are not rate-limited or invalid
func (m *Manager) GetAvailableAccounts(modelID string) []*redis.Account {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*redis.Account, 0)
	for _, acc := range m.accounts {
		if !acc.Enabled || acc.IsInvalid {
			continue
		}
		if !m.isRateLimitedForModel(acc, modelID) {
			result = append(result, acc)
		}
	}
	return result
}

// GetInvalidAccounts returns accounts that are marked as invalid
func (m *Manager) GetInvalidAccounts() []*redis.Account {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*redis.Account, 0)
	for _, acc := range m.accounts {
		if acc.IsInvalid {
			result = append(result, acc)
		}
	}
	return result
}

// MarkRateLimited marks an account as rate-limited for a model
func (m *Manager) MarkRateLimited(ctx context.Context, email string, resetMs int64, modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	resetTime := time.Now().Add(time.Duration(resetMs) * time.Millisecond).UnixMilli()
	info := &redis.RateLimitInfo{
		IsRateLimited: true,
		ResetTime:     resetTime,
		ActualResetMs: resetMs,
	}

	return m.accountStore.SetRateLimit(ctx, email, modelID, info)
}

// MarkInvalid marks an account as invalid
func (m *Manager) MarkInvalid(ctx context.Context, email, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, acc := range m.accounts {
		if acc.Email == email {
			acc.IsInvalid = true
			acc.InvalidReason = reason
			acc.InvalidAt = time.Now().UnixMilli()
			return m.accountStore.SetAccount(ctx, acc)
		}
	}

	return nil
}

// ResetAllRateLimits clears all rate limits
func (m *Manager) ResetAllRateLimits(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, acc := range m.accounts {
		_ = m.accountStore.ClearRateLimits(ctx, acc.Email)
	}
}

// ClearExpiredLimits removes expired rate limits
func (m *Manager) ClearExpiredLimits(ctx context.Context) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clearExpiredLimitsLocked(ctx)
}

func (m *Manager) clearExpiredLimitsLocked(ctx context.Context) int {
	// Rate limits auto-expire via Redis TTL, so this is mostly a no-op
	// But we still check and clear any that might be stale
	var cleared int
	for range m.accounts {
		// The rate limit data will auto-expire, but we can check if it's still valid
		// For now, rely on Redis TTL for cleanup
	}
	return cleared
}

// GetMinWaitTimeMs returns the minimum wait time until a rate limit clears
func (m *Manager) GetMinWaitTimeMs(ctx context.Context, modelID string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var minWait int64 = -1
	now := time.Now()

	for _, acc := range m.accounts {
		if !acc.Enabled || acc.IsInvalid {
			continue
		}

		info, err := m.accountStore.GetRateLimit(ctx, acc.Email, modelID)
		if err != nil || info == nil || !info.IsRateLimited {
			return 0 // At least one account is available
		}

		if info.ResetTime > 0 {
			wait := info.ResetTime - now.UnixMilli()
			if wait > 0 {
				if minWait < 0 || wait < minWait {
					minWait = wait
				}
			}
		}
	}

	if minWait < 0 {
		return 0
	}
	return minWait
}

// GetRateLimitInfo returns rate limit info for an account and model
func (m *Manager) GetRateLimitInfo(ctx context.Context, email, modelID string) *redis.RateLimitInfo {
	info, _ := m.accountStore.GetRateLimit(ctx, email, modelID)
	return info
}

// NotifySuccess notifies the strategy of a successful request
func (m *Manager) NotifySuccess(account *redis.Account, modelID string) {
	if m.strategy != nil {
		m.strategy.OnSuccess(account, modelID)
	}
}

// NotifyRateLimit notifies the strategy of a rate limit
func (m *Manager) NotifyRateLimit(account *redis.Account, modelID string) {
	if m.strategy != nil {
		m.strategy.OnRateLimit(account, modelID)
	}
}

// NotifyFailure notifies the strategy of a failure
func (m *Manager) NotifyFailure(account *redis.Account, modelID string) {
	if m.strategy != nil {
		m.strategy.OnFailure(account, modelID)
	}
}

// GetStrategyName returns the current strategy name
func (m *Manager) GetStrategyName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.strategyName
}

// GetStrategyLabel returns the display label for the current strategy
func (m *Manager) GetStrategyLabel() string {
	return strategies.GetStrategyLabel(m.GetStrategyName())
}

// GetHealthTracker returns the health tracker (for hybrid strategy)
func (m *Manager) GetHealthTracker() strategies.HealthTracker {
	if hs, ok := m.strategy.(interface{ GetHealthTracker() strategies.HealthTracker }); ok {
		return hs.GetHealthTracker()
	}
	return nil
}

// SaveToDisk saves account state to storage
func (m *Manager) SaveToDisk(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveToDiskLocked(ctx)
}

func (m *Manager) saveToDiskLocked(ctx context.Context) error {
	for _, acc := range m.accounts {
		if err := m.accountStore.SetAccount(ctx, acc); err != nil {
			utils.Warn("[AccountManager] Failed to save account %s: %v", acc.Email, err)
		}
	}
	return nil
}

// GetStatus returns the current status of the account manager
func (m *Manager) GetStatus() *ManagerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := &ManagerStatus{
		Total:     len(m.accounts),
		Accounts:  make([]*AccountStatus, 0, len(m.accounts)),
	}

	for _, acc := range m.accounts {
		accStatus := &AccountStatus{
			Email:      acc.Email,
			Source:     acc.Source,
			Enabled:    acc.Enabled,
			ProjectID:  acc.ProjectID,
			IsInvalid:  acc.IsInvalid,
			InvalidReason: acc.InvalidReason,
			QuotaThreshold: acc.QuotaThreshold,
			ModelQuotaThresholds: acc.ModelQuotaThresholds,
			ModelRateLimits: acc.ModelRateLimits,
		}

		// Convert LastUsed
		if acc.LastUsed > 0 {
			accStatus.LastUsed = acc.LastUsed
		}

		if !acc.Enabled || acc.IsInvalid {
			status.Invalid++
		} else {
			status.Available++
		}

		status.Accounts = append(status.Accounts, accStatus)
	}

	status.Summary = utils.TruncateString(
		m.formatStatusSummary(status.Available, status.RateLimited, status.Total),
		100,
	)

	return status
}

func (m *Manager) formatStatusSummary(available, rateLimited, total int) string {
	if total == 0 {
		return "No accounts configured"
	}
	if rateLimited > 0 {
		return utils.FormatDuration(0) // Placeholder
	}
	return "All accounts available"
}

// Helper methods

func (m *Manager) isRateLimitedForModel(acc *redis.Account, modelID string) bool {
	if modelID == "" {
		return false
	}
	info, _ := m.accountStore.GetRateLimit(context.Background(), acc.Email, modelID)
	if info == nil {
		return false
	}
	if !info.IsRateLimited {
		return false
	}
	if info.ResetTime > 0 && time.Now().After(time.UnixMilli(info.ResetTime)) {
		return false
	}
	return true
}

// SelectOptions for account selection
type SelectOptions struct {
	SessionID string
}

// SelectionResult from account selection
type SelectionResult struct {
	Account *redis.Account
	Index   int
	WaitMs  int64
}

// ManagerStatus represents the status of the account manager
type ManagerStatus struct {
	Total       int              `json:"total"`
	Available   int              `json:"available"`
	RateLimited int              `json:"rateLimited"`
	Invalid     int              `json:"invalid"`
	Summary     string           `json:"summary"`
	Accounts    []*AccountStatus `json:"accounts"`
}

// AccountStatus represents the status of a single account
type AccountStatus struct {
	Email                string                          `json:"email"`
	Source               string                          `json:"source"`
	Enabled              bool                            `json:"enabled"`
	ProjectID            string                          `json:"projectId,omitempty"`
	IsInvalid            bool                            `json:"isInvalid"`
	InvalidReason        string                          `json:"invalidReason,omitempty"`
	LastUsed             int64                           `json:"lastUsed,omitempty"`
	QuotaThreshold       *float64                        `json:"quotaThreshold,omitempty"`
	ModelQuotaThresholds map[string]float64              `json:"modelQuotaThresholds,omitempty"`
	ModelRateLimits      map[string]*redis.RateLimitInfo `json:"modelRateLimits,omitempty"`
}

// Error types

type NotInitializedError struct{}

func (e *NotInitializedError) Error() string {
	return "AccountManager not initialized"
}

func NewNotInitializedError() *NotInitializedError {
	return &NotInitializedError{}
}

type NoAccountsError struct {
	Message        string
	AllRateLimited bool
}

func (e *NoAccountsError) Error() string {
	return e.Message
}

func NewNoAccountsError(message string, allRateLimited bool) *NoAccountsError {
	return &NoAccountsError{
		Message:        message,
		AllRateLimited: allRateLimited,
	}
}

// GetTokenForAccount gets an access token for the given account
// 直接委托给 Credentials 管理器，它有正确的 TTL 处理
// 匹配 Node.js 版本的 getTokenForAccount 行为
func (m *Manager) GetTokenForAccount(ctx context.Context, acc *redis.Account) (string, error) {
	// 直接使用 credentials manager 获取 token
	// credentials.GetAccessToken 已经有正确的缓存和 TTL 逻辑
	token, err := m.credentials.GetAccessToken(ctx, acc)
	if err != nil {
		// 检查是否是认证错误，需要标记账户无效
		if isAuthError(err) {
			_ = m.MarkInvalid(ctx, acc.Email, err.Error())
		}
		return "", err
	}

	// 成功获取 token，清除无效标记（如果有的话）
	if acc.IsInvalid {
		acc.IsInvalid = false
		acc.InvalidReason = ""
		_ = m.accountStore.SetAccount(ctx, acc)
	}

	return token, nil
}

// isAuthError 检查是否是认证相关错误
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// OAuth token refresh 失败通常表示凭证无效
	return strings.Contains(errStr, "token refresh failed") ||
		strings.Contains(errStr, "invalid_grant") ||
		strings.Contains(errStr, "Token has been expired or revoked")
}

// ClearTokenCache clears all cached tokens
// 委托给 Credentials 管理器，匹配 Node.js 的 clearTokenCache 行为
func (m *Manager) ClearTokenCache() {
	m.credentials.ClearCache()
}

// ClearProjectCache clears project cache (placeholder for now)
func (m *Manager) ClearProjectCache() {
	// In Go version, we don't have a separate project cache
	// This is a placeholder for API compatibility
}

// UpdateAccountSubscription updates the subscription info for an account
func (m *Manager) UpdateAccountSubscription(email, tier, projectID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, acc := range m.accounts {
		if acc.Email == email {
			if acc.Subscription == nil {
				acc.Subscription = &redis.SubscriptionInfo{}
			}
			acc.Subscription.Tier = tier
			acc.Subscription.ProjectID = projectID
			acc.Subscription.DetectedAt = time.Now().UnixMilli()

			// Save asynchronously
			go func() {
				if err := m.accountStore.SetAccount(context.Background(), acc); err != nil {
					utils.Error("[AccountManager] Failed to save account subscription: %v", err)
				}
			}()
			return
		}
	}
}

// UpdateAccountQuota updates the quota info for an account
// quotas is a map of modelID to quota info with RemainingFraction and ResetTime fields
func (m *Manager) UpdateAccountQuota(email string, quotas map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, acc := range m.accounts {
		if acc.Email == email {
			if acc.Quota == nil {
				acc.Quota = &redis.QuotaInfo{
					Models: make(map[string]*redis.ModelQuotaInfo),
				}
			}
			acc.Quota.LastChecked = time.Now().UnixMilli()

			for modelID, quota := range quotas {
				if quotaMap, ok := quota.(map[string]interface{}); ok {
					info := &redis.ModelQuotaInfo{}
					if rf, ok := quotaMap["remainingFraction"].(float64); ok {
						info.RemainingFraction = rf
					}
					if rt, ok := quotaMap["resetTime"].(string); ok {
						info.ResetTime = rt
					}
					acc.Quota.Models[modelID] = info
				}
			}

			// Save asynchronously
			go func() {
				if err := m.accountStore.SetAccount(context.Background(), acc); err != nil {
					utils.Error("[AccountManager] Failed to save account quota: %v", err)
				}
			}()
			return
		}
	}
}

// ClearTokenCacheFor clears cached token for a specific email
// 委托给 Credentials 管理器
func (m *Manager) ClearTokenCacheFor(email string) {
	m.credentials.ClearCacheForAccount(context.Background(), email)
}

// ClearProjectCacheFor clears project cache for a specific email
func (m *Manager) ClearProjectCacheFor(email string) {
	// Placeholder for API compatibility
	// In Go version, we don't maintain a separate project cache
}

// SetAccountEnabled enables or disables an account
func (m *Manager) SetAccountEnabled(ctx context.Context, email string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, acc := range m.accounts {
		if acc.Email == email {
			acc.Enabled = enabled
			return m.accountStore.SetAccount(ctx, acc)
		}
	}

	return NewNoAccountsError("Account "+email+" not found", false)
}

// RemoveAccount removes an account
func (m *Manager) RemoveAccount(ctx context.Context, email string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, acc := range m.accounts {
		if acc.Email == email {
			m.accounts = append(m.accounts[:i], m.accounts[i+1:]...)
			return m.accountStore.DeleteAccount(ctx, email)
		}
	}

	return NewNoAccountsError("Account "+email+" not found", false)
}

// GetAccountByEmail returns an account by email
func (m *Manager) GetAccountByEmail(ctx context.Context, email string) (*redis.Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, acc := range m.accounts {
		if acc.Email == email {
			return acc, nil
		}
	}

	return nil, NewNoAccountsError("Account "+email+" not found", false)
}

// UpdateAccount updates an account
func (m *Manager) UpdateAccount(ctx context.Context, acc *redis.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, existing := range m.accounts {
		if existing.Email == acc.Email {
			m.accounts[i] = acc
			return m.accountStore.SetAccount(ctx, acc)
		}
	}

	return NewNoAccountsError("Account "+acc.Email+" not found", false)
}

// AddOrUpdateAccount adds a new account or updates an existing one
func (m *Manager) AddOrUpdateAccount(ctx context.Context, acc *redis.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if account exists
	for i, existing := range m.accounts {
		if existing.Email == acc.Email {
			// Update existing account
			m.accounts[i] = acc
			utils.Info("[AccountManager] Account %s updated", acc.Email)
			return m.accountStore.SetAccount(ctx, acc)
		}
	}

	// Check max accounts limit
	if len(m.accounts) >= m.config.MaxAccounts {
		return NewNoAccountsError("Maximum accounts reached", false)
	}

	// Add new account
	m.accounts = append(m.accounts, acc)
	utils.Info("[AccountManager] Account %s added", acc.Email)
	return m.accountStore.SetAccount(ctx, acc)
}

// GetAllAccountsWithContext returns all accounts (context-aware version)
func (m *Manager) GetAllAccountsContext(ctx context.Context) ([]*redis.Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*redis.Account, len(m.accounts))
	copy(result, m.accounts)
	return result, nil
}

// StrategyHealthData represents health data for the strategy inspector
type StrategyHealthData struct {
	Strategy    string                 `json:"strategy"`
	Accounts    []AccountHealthData    `json:"accounts"`
	LastUpdated int64                  `json:"lastUpdated"`
}

// AccountHealthData represents health data for a single account
type AccountHealthData struct {
	Email            string  `json:"email"`
	HealthScore      float64 `json:"healthScore"`
	TokensAvailable  float64 `json:"tokensAvailable"`
	ConsecutiveFails int     `json:"consecutiveFails"`
	LastUsed         int64   `json:"lastUsed"`
}

// GetStrategyHealthData returns health data for the strategy inspector
func (m *Manager) GetStrategyHealthData() *StrategyHealthData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data := &StrategyHealthData{
		Strategy:    m.strategyName,
		Accounts:    make([]AccountHealthData, 0),
		LastUpdated: time.Now().UnixMilli(),
	}

	// Try to get health and token data from hybrid strategy
	var healthGetter interface{ GetHealthScore(string) float64 }
	var tokenGetter interface{ GetTokens(string) float64 }
	var failureGetter interface{ GetConsecutiveFailures(string) int }

	if hs, ok := m.strategy.(interface{ GetHealthTracker() strategies.HealthTracker }); ok {
		if tracker := hs.GetHealthTracker(); tracker != nil {
			healthGetter = tracker
			failureGetter = tracker
		}
	}

	if ts, ok := m.strategy.(interface {
		GetTokenBucketTracker() interface{ GetTokens(string) float64 }
	}); ok {
		if tracker := ts.GetTokenBucketTracker(); tracker != nil {
			tokenGetter = tracker
		}
	}

	for _, acc := range m.accounts {
		accData := AccountHealthData{
			Email:    acc.Email,
			LastUsed: acc.LastUsed,
		}

		if healthGetter != nil {
			accData.HealthScore = healthGetter.GetHealthScore(acc.Email)
		}

		if tokenGetter != nil {
			accData.TokensAvailable = tokenGetter.GetTokens(acc.Email)
		}

		if failureGetter != nil {
			accData.ConsecutiveFails = failureGetter.GetConsecutiveFailures(acc.Email)
		}

		data.Accounts = append(data.Accounts, accData)
	}

	return data
}

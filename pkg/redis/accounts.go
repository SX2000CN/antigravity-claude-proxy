// Package redis provides Redis operations for account storage.
// This file corresponds to src/account-manager/storage.js in the Node.js version.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Account represents a configured account stored in Redis
type Account struct {
	Email        string `json:"email"`
	Source       string `json:"source"` // "oauth", "manual", "database"
	Enabled      bool   `json:"enabled"`
	RefreshToken string `json:"refreshToken,omitempty"`
	APIKey       string `json:"apiKey,omitempty"`
	ProjectID    string `json:"projectId,omitempty"`

	// Subscription info
	Subscription *SubscriptionInfo `json:"subscription,omitempty"`

	// Quota management
	QuotaThreshold       *float64           `json:"quotaThreshold,omitempty"`
	ModelQuotaThresholds map[string]float64 `json:"modelQuotaThresholds,omitempty"`
	Quota                *QuotaInfo         `json:"quota,omitempty"`

	// Rate limit tracking (runtime only, also synced to Redis)
	ModelRateLimits map[string]*RateLimitInfo `json:"modelRateLimits,omitempty"`

	// Status tracking
	LastUsed      int64  `json:"lastUsed,omitempty"` // Unix timestamp in milliseconds
	IsInvalid     bool   `json:"isInvalid"`
	InvalidReason string `json:"invalidReason,omitempty"`
	InvalidAt     int64  `json:"invalidAt,omitempty"` // Unix timestamp in milliseconds

	// Cooldown tracking (runtime only, not persisted)
	CoolingDownUntil int64  `json:"-"`
	CooldownReason   string `json:"-"`
}

// SubscriptionInfo represents subscription tier info
type SubscriptionInfo struct {
	Tier       string `json:"tier"` // "free", "pro", "ultra"
	ProjectID  string `json:"projectId,omitempty"`
	DetectedAt int64  `json:"detectedAt"` // Unix timestamp in milliseconds
}

// QuotaInfo represents model quota information
type QuotaInfo struct {
	Models      map[string]*ModelQuotaInfo `json:"models"`
	LastChecked int64                      `json:"lastChecked,omitempty"` // Unix timestamp in milliseconds
}

// ModelQuotaInfo represents quota for a specific model
type ModelQuotaInfo struct {
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
}

// RateLimitInfo represents model-specific rate limit state
type RateLimitInfo struct {
	IsRateLimited bool  `json:"isRateLimited"`
	ResetTime     int64 `json:"resetTime,omitempty"`     // Unix timestamp in milliseconds
	ActualResetMs int64 `json:"actualResetMs,omitempty"` // Duration in milliseconds
}

// HealthScore represents account health for hybrid strategy
type HealthScore struct {
	Score               float64   `json:"score"`
	LastUpdated         time.Time `json:"lastUpdated"`
	ConsecutiveFailures int       `json:"consecutiveFailures"`
}

// TokenBucket represents token bucket state for hybrid strategy
type TokenBucket struct {
	Tokens      float64   `json:"tokens"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// CachedToken represents a cached access token
type CachedToken struct {
	AccessToken string    `json:"accessToken"`
	ExtractedAt time.Time `json:"extractedAt"`
}

// OAuthState represents OAuth PKCE state
type OAuthState struct {
	Verifier    string    `json:"verifier"`
	RedirectURI string    `json:"redirectUri"`
	CreatedAt   time.Time `json:"createdAt"`
}

// AccountStore provides account-specific Redis operations
type AccountStore struct {
	client *Client
}

// NewAccountStore creates a new AccountStore
func NewAccountStore(client *Client) *AccountStore {
	return &AccountStore{client: client}
}

// IsAvailable returns true if the Redis client is connected and available
func (s *AccountStore) IsAvailable() bool {
	return s != nil && s.client != nil
}

// ============================================================
// Account CRUD Operations
// ============================================================

// GetAccount retrieves an account by email
func (s *AccountStore) GetAccount(ctx context.Context, email string) (*Account, error) {
	if s.client == nil {
		return nil, fmt.Errorf("redis client not available")
	}
	key := PrefixAccounts + email
	data, err := s.client.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	account := &Account{
		Email:                email,
		ModelQuotaThresholds: make(map[string]float64),
	}

	// Parse hash fields
	if v, ok := data["source"]; ok {
		account.Source = v
	}
	if v, ok := data["enabled"]; ok {
		account.Enabled = v == "true"
	}
	if v, ok := data["refreshToken"]; ok {
		account.RefreshToken = v
	}
	if v, ok := data["apiKey"]; ok {
		account.APIKey = v
	}
	if v, ok := data["projectId"]; ok {
		account.ProjectID = v
	}
	if v, ok := data["isInvalid"]; ok {
		account.IsInvalid = v == "true"
	}
	if v, ok := data["invalidReason"]; ok {
		account.InvalidReason = v
	}
	if v, ok := data["lastUsed"]; ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			account.LastUsed = t.UnixMilli()
		}
	}
	if v, ok := data["invalidAt"]; ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			account.InvalidAt = t.UnixMilli()
		}
	}
	if v, ok := data["quotaThreshold"]; ok {
		var f float64
		if err := json.Unmarshal([]byte(v), &f); err == nil {
			account.QuotaThreshold = &f
		}
	}
	if v, ok := data["subscription"]; ok {
		var sub SubscriptionInfo
		if err := json.Unmarshal([]byte(v), &sub); err == nil {
			account.Subscription = &sub
		}
	}
	if v, ok := data["quota"]; ok {
		var quota QuotaInfo
		if err := json.Unmarshal([]byte(v), &quota); err == nil {
			account.Quota = &quota
		}
	}
	if v, ok := data["modelQuotaThresholds"]; ok {
		var thresholds map[string]float64
		if err := json.Unmarshal([]byte(v), &thresholds); err == nil {
			account.ModelQuotaThresholds = thresholds
		}
	}

	return account, nil
}

// SetAccount stores an account
func (s *AccountStore) SetAccount(ctx context.Context, account *Account) error {
	if s.client == nil {
		return fmt.Errorf("redis client not available")
	}
	key := PrefixAccounts + account.Email
	values := map[string]interface{}{
		"email":     account.Email,
		"source":    account.Source,
		"enabled":   fmt.Sprintf("%t", account.Enabled),
		"isInvalid": fmt.Sprintf("%t", account.IsInvalid),
	}

	if account.RefreshToken != "" {
		values["refreshToken"] = account.RefreshToken
	}
	if account.APIKey != "" {
		values["apiKey"] = account.APIKey
	}
	if account.ProjectID != "" {
		values["projectId"] = account.ProjectID
	}
	if account.InvalidReason != "" {
		values["invalidReason"] = account.InvalidReason
	}
	if account.LastUsed > 0 {
		values["lastUsed"] = time.UnixMilli(account.LastUsed).Format(time.RFC3339)
	}
	if account.InvalidAt > 0 {
		values["invalidAt"] = time.UnixMilli(account.InvalidAt).Format(time.RFC3339)
	}
	if account.QuotaThreshold != nil {
		data, _ := json.Marshal(account.QuotaThreshold)
		values["quotaThreshold"] = string(data)
	}
	if account.Subscription != nil {
		data, _ := json.Marshal(account.Subscription)
		values["subscription"] = string(data)
	}
	if account.Quota != nil {
		data, _ := json.Marshal(account.Quota)
		values["quota"] = string(data)
	}
	if len(account.ModelQuotaThresholds) > 0 {
		data, _ := json.Marshal(account.ModelQuotaThresholds)
		values["modelQuotaThresholds"] = string(data)
	}

	if err := s.client.HSet(ctx, key, values); err != nil {
		return err
	}

	// Add to index
	return s.client.SAdd(ctx, PrefixAccountIndex, account.Email)
}

// DeleteAccount removes an account
func (s *AccountStore) DeleteAccount(ctx context.Context, email string) error {
	key := PrefixAccounts + email

	// Delete account data
	if err := s.client.Delete(ctx, key); err != nil {
		return err
	}

	// Remove from index
	if err := s.client.SRem(ctx, PrefixAccountIndex, email); err != nil {
		return err
	}

	// Clean up related data
	_ = s.ClearRateLimits(ctx, email)
	_ = s.ClearQuotas(ctx, email)
	_ = s.ClearHealth(ctx, email)
	_ = s.ClearTokenBucket(ctx, email)
	_ = s.ClearTokenCache(ctx, email)
	_ = s.ClearProjectCache(ctx, email)

	return nil
}

// ListAccounts returns all accounts
func (s *AccountStore) ListAccounts(ctx context.Context) ([]*Account, error) {
	if s.client == nil {
		return make([]*Account, 0), nil
	}
	emails, err := s.client.SMembers(ctx, PrefixAccountIndex)
	if err != nil {
		return nil, err
	}

	accounts := make([]*Account, 0, len(emails))
	for _, email := range emails {
		account, err := s.GetAccount(ctx, email)
		if err != nil {
			continue
		}
		if account != nil {
			accounts = append(accounts, account)
		}
	}

	return accounts, nil
}

// ============================================================
// Rate Limit Operations
// ============================================================

// GetRateLimit retrieves rate limit info for a model
func (s *AccountStore) GetRateLimit(ctx context.Context, email, modelID string) (*RateLimitInfo, error) {
	key := PrefixRateLimits + email + ":" + modelID
	data, err := s.client.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	info := &RateLimitInfo{}
	if v, ok := data["isRateLimited"]; ok {
		info.IsRateLimited = v == "true"
	}
	if v, ok := data["resetTime"]; ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			info.ResetTime = t.UnixMilli()
		}
	}
	if v, ok := data["actualResetMs"]; ok {
		var ms int64
		if err := json.Unmarshal([]byte(v), &ms); err == nil {
			info.ActualResetMs = ms
		}
	}

	return info, nil
}

// SetRateLimit stores rate limit info with auto-expiry
func (s *AccountStore) SetRateLimit(ctx context.Context, email, modelID string, info *RateLimitInfo) error {
	key := PrefixRateLimits + email + ":" + modelID
	values := map[string]interface{}{
		"isRateLimited": fmt.Sprintf("%t", info.IsRateLimited),
		"actualResetMs": fmt.Sprintf("%d", info.ActualResetMs),
	}
	if info.ResetTime > 0 {
		values["resetTime"] = time.UnixMilli(info.ResetTime).Format(time.RFC3339)
	}

	if err := s.client.HSet(ctx, key, values); err != nil {
		return err
	}

	// Set TTL based on reset time
	if info.ResetTime > 0 {
		resetTime := time.UnixMilli(info.ResetTime)
		ttl := time.Until(resetTime)
		if ttl > 0 {
			return s.client.Expire(ctx, key, ttl+time.Minute) // Add 1 minute buffer
		}
	}

	return nil
}

// ClearRateLimit clears rate limit for a specific model
func (s *AccountStore) ClearRateLimit(ctx context.Context, email, modelID string) error {
	key := PrefixRateLimits + email + ":" + modelID
	return s.client.Delete(ctx, key)
}

// ClearRateLimits clears all rate limits for an account
func (s *AccountStore) ClearRateLimits(ctx context.Context, email string) error {
	pattern := PrefixRateLimits + email + ":*"
	keys, err := s.client.ScanAll(ctx, pattern)
	if err != nil {
		return err
	}
	if len(keys) > 0 {
		return s.client.Delete(ctx, keys...)
	}
	return nil
}

// ============================================================
// Quota Operations
// ============================================================

// GetQuotas retrieves quota info for all models
func (s *AccountStore) GetQuotas(ctx context.Context, email string) (*QuotaInfo, error) {
	key := PrefixQuotas + email
	data, err := s.client.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	info := &QuotaInfo{
		Models: make(map[string]*ModelQuotaInfo),
	}

	for field, value := range data {
		if field == "_lastChecked" {
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				info.LastChecked = t.UnixMilli()
			}
		} else {
			var quota ModelQuotaInfo
			if err := json.Unmarshal([]byte(value), &quota); err == nil {
				info.Models[field] = &quota
			}
		}
	}

	return info, nil
}

// SetQuotas stores quota info with TTL
func (s *AccountStore) SetQuotas(ctx context.Context, email string, info *QuotaInfo) error {
	key := PrefixQuotas + email
	values := map[string]interface{}{}

	if info.LastChecked > 0 {
		values["_lastChecked"] = time.UnixMilli(info.LastChecked).Format(time.RFC3339)
	}

	for modelID, quota := range info.Models {
		data, _ := json.Marshal(quota)
		values[modelID] = string(data)
	}

	if err := s.client.HSet(ctx, key, values); err != nil {
		return err
	}

	// Set 5 minute TTL
	return s.client.Expire(ctx, key, 5*time.Minute)
}

// ClearQuotas clears quota cache for an account
func (s *AccountStore) ClearQuotas(ctx context.Context, email string) error {
	key := PrefixQuotas + email
	return s.client.Delete(ctx, key)
}

// ============================================================
// Health Score Operations
// ============================================================

// GetHealth retrieves health score for an account
func (s *AccountStore) GetHealth(ctx context.Context, email string) (*HealthScore, error) {
	key := PrefixHealth + email
	data, err := s.client.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	score := &HealthScore{}
	if v, ok := data["score"]; ok {
		var f float64
		if err := json.Unmarshal([]byte(v), &f); err == nil {
			score.Score = f
		}
	}
	if v, ok := data["lastUpdated"]; ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			score.LastUpdated = t
		}
	}
	if v, ok := data["consecutiveFailures"]; ok {
		var n int
		if err := json.Unmarshal([]byte(v), &n); err == nil {
			score.ConsecutiveFailures = n
		}
	}

	return score, nil
}

// SetHealth stores health score for an account
func (s *AccountStore) SetHealth(ctx context.Context, email string, score *HealthScore) error {
	key := PrefixHealth + email
	values := map[string]interface{}{
		"score":               fmt.Sprintf("%f", score.Score),
		"lastUpdated":         score.LastUpdated.Format(time.RFC3339),
		"consecutiveFailures": fmt.Sprintf("%d", score.ConsecutiveFailures),
	}
	return s.client.HSet(ctx, key, values)
}

// ClearHealth clears health score for an account
func (s *AccountStore) ClearHealth(ctx context.Context, email string) error {
	key := PrefixHealth + email
	return s.client.Delete(ctx, key)
}

// ============================================================
// Token Bucket Operations
// ============================================================

// GetTokenBucket retrieves token bucket state
func (s *AccountStore) GetTokenBucket(ctx context.Context, email string) (*TokenBucket, error) {
	key := PrefixTokens + email
	data, err := s.client.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	bucket := &TokenBucket{}
	if v, ok := data["tokens"]; ok {
		var f float64
		if err := json.Unmarshal([]byte(v), &f); err == nil {
			bucket.Tokens = f
		}
	}
	if v, ok := data["lastUpdated"]; ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			bucket.LastUpdated = t
		}
	}

	return bucket, nil
}

// SetTokenBucket stores token bucket state
func (s *AccountStore) SetTokenBucket(ctx context.Context, email string, bucket *TokenBucket) error {
	key := PrefixTokens + email
	values := map[string]interface{}{
		"tokens":      fmt.Sprintf("%f", bucket.Tokens),
		"lastUpdated": bucket.LastUpdated.Format(time.RFC3339),
	}
	return s.client.HSet(ctx, key, values)
}

// ClearTokenBucket clears token bucket for an account
func (s *AccountStore) ClearTokenBucket(ctx context.Context, email string) error {
	key := PrefixTokens + email
	return s.client.Delete(ctx, key)
}

// ============================================================
// Token Cache Operations
// ============================================================

// GetCachedToken retrieves a cached access token
func (s *AccountStore) GetCachedToken(ctx context.Context, email string) (*CachedToken, error) {
	key := PrefixTokenCache + email
	data, err := s.client.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	token := &CachedToken{}
	if v, ok := data["accessToken"]; ok {
		token.AccessToken = v
	}
	if v, ok := data["extractedAt"]; ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			token.ExtractedAt = t
		}
	}

	return token, nil
}

// SetCachedToken stores an access token with TTL
func (s *AccountStore) SetCachedToken(ctx context.Context, email, token string, ttl time.Duration) error {
	key := PrefixTokenCache + email
	values := map[string]interface{}{
		"accessToken": token,
		"extractedAt": time.Now().Format(time.RFC3339),
	}

	if err := s.client.HSet(ctx, key, values); err != nil {
		return err
	}

	return s.client.Expire(ctx, key, ttl)
}

// ClearTokenCache clears cached token for an account
func (s *AccountStore) ClearTokenCache(ctx context.Context, email string) error {
	key := PrefixTokenCache + email
	return s.client.Delete(ctx, key)
}

// ============================================================
// Project Cache Operations
// ============================================================

// GetCachedProject retrieves a cached project ID
func (s *AccountStore) GetCachedProject(ctx context.Context, email string) (string, error) {
	key := PrefixProjectCache + email
	return s.client.GetString(ctx, key)
}

// SetCachedProject stores a project ID with TTL
func (s *AccountStore) SetCachedProject(ctx context.Context, email, projectID string, ttl time.Duration) error {
	key := PrefixProjectCache + email
	return s.client.SetString(ctx, key, projectID, ttl)
}

// ClearProjectCache clears cached project ID for an account
func (s *AccountStore) ClearProjectCache(ctx context.Context, email string) error {
	key := PrefixProjectCache + email
	return s.client.Delete(ctx, key)
}

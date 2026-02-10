// Package account provides account management with configurable selection strategies.
// This file corresponds to src/account-manager/credentials.js in the Node.js version.
package account

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/auth"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// CachedToken holds a cached access token
type CachedToken struct {
	Token     string
	ExpiresAt time.Time
}

// Credentials manages OAuth tokens and API keys for accounts
type Credentials struct {
	mu           sync.RWMutex
	redisClient  *redis.Client
	accountStore *redis.AccountStore
	tokenCache   map[string]*CachedToken
}

// NewCredentials creates a new credentials manager
func NewCredentials(redisClient *redis.Client) *Credentials {
	var accountStore *redis.AccountStore
	if redisClient != nil {
		accountStore = redis.NewAccountStore(redisClient)
	}
	return &Credentials{
		redisClient:  redisClient,
		accountStore: accountStore,
		tokenCache:   make(map[string]*CachedToken),
	}
}

// GetAccessToken returns an access token for the given account
func (c *Credentials) GetAccessToken(ctx context.Context, acc *redis.Account) (string, error) {
	if acc == nil {
		return "", fmt.Errorf("account is nil")
	}

	// Check in-memory cache first
	c.mu.RLock()
	cached, ok := c.tokenCache[acc.Email]
	c.mu.RUnlock()

	if ok && cached.ExpiresAt.After(time.Now()) {
		return cached.Token, nil
	}

	// Check Redis cache
	if c.accountStore != nil {
		cachedToken, err := c.accountStore.GetCachedToken(ctx, acc.Email)
		if err == nil && cachedToken != nil && cachedToken.AccessToken != "" {
			// Token is valid if extracted less than 5 minutes ago
			if time.Since(cachedToken.ExtractedAt) < 5*time.Minute {
				c.cacheToken(acc.Email, cachedToken.AccessToken, 5*time.Minute)
				return cachedToken.AccessToken, nil
			}
		}
	}

	// Get fresh token based on account type
	token, err := c.getFreshToken(ctx, acc)
	if err != nil {
		return "", err
	}

	// Cache the token
	c.cacheToken(acc.Email, token, 5*time.Minute)

	// Also cache in Redis
	if c.accountStore != nil {
		_ = c.accountStore.SetCachedToken(ctx, acc.Email, token, 5*time.Minute)
	}

	return token, nil
}

// getFreshToken obtains a fresh token from OAuth or uses the API key
func (c *Credentials) getFreshToken(ctx context.Context, acc *redis.Account) (string, error) {
	switch acc.Source {
	case "oauth":
		if acc.RefreshToken == "" {
			return "", fmt.Errorf("no refresh token for account %s", acc.Email)
		}
		// Use the package-level function from auth
		utils.Debug("[Credentials] Refreshing OAuth token for %s", acc.Email)
		result, err := auth.RefreshAccessToken(ctx, acc.RefreshToken)
		if err != nil {
			utils.Error("[Credentials] Failed to refresh token for %s: %v", acc.Email, err)
			return "", err
		}
		utils.Success("[Credentials] Refreshed OAuth token for %s", acc.Email)
		return result.AccessToken, nil

	case "manual":
		if acc.APIKey != "" {
			return acc.APIKey, nil
		}
		return "", fmt.Errorf("no API key for manual account %s", acc.Email)

	case "database":
		// For database accounts, try to extract from token-extractor
		// This is a legacy path for accounts imported from Anthropic Manager
		return "", fmt.Errorf("database token extraction not yet implemented")

	default:
		return "", fmt.Errorf("unknown account source: %s", acc.Source)
	}
}

// cacheToken stores a token in the in-memory cache
func (c *Credentials) cacheToken(email, token string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokenCache[email] = &CachedToken{
		Token:     token,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// ClearCache clears the in-memory token cache
func (c *Credentials) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokenCache = make(map[string]*CachedToken)
}

// ClearCacheForAccount clears the cache for a specific account
func (c *Credentials) ClearCacheForAccount(ctx context.Context, email string) {
	c.mu.Lock()
	delete(c.tokenCache, email)
	c.mu.Unlock()

	if c.accountStore != nil {
		_ = c.accountStore.ClearTokenCache(ctx, email)
	}
}

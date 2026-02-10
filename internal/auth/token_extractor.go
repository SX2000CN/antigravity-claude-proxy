// Package auth provides token extraction from Antigravity's SQLite database.
// This file corresponds to src/auth/token-extractor.js in the Node.js version.
package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// TokenExtractor extracts OAuth tokens from Antigravity's database
type TokenExtractor struct {
	mu               sync.RWMutex
	cachedToken      string
	tokenExtractedAt time.Time
	accountStore     *redis.AccountStore
}

// NewTokenExtractor creates a new TokenExtractor
func NewTokenExtractor(accountStore *redis.AccountStore) *TokenExtractor {
	return &TokenExtractor{
		accountStore: accountStore,
	}
}

// GetToken gets the current OAuth token (with caching)
func (te *TokenExtractor) GetToken(ctx context.Context, email string) (string, error) {
	te.mu.RLock()
	needsRefresh := te.needsRefresh()
	te.mu.RUnlock()

	if !needsRefresh {
		te.mu.RLock()
		token := te.cachedToken
		te.mu.RUnlock()
		return token, nil
	}

	te.mu.Lock()
	defer te.mu.Unlock()

	// Double-check after acquiring write lock
	if !te.needsRefresh() {
		return te.cachedToken, nil
	}

	token, err := te.getTokenData(ctx, email)
	if err != nil {
		return "", err
	}

	te.cachedToken = token
	te.tokenExtractedAt = time.Now()
	return token, nil
}

// ForceRefresh forces a token refresh
func (te *TokenExtractor) ForceRefresh(ctx context.Context, email string) (string, error) {
	te.mu.Lock()
	te.cachedToken = ""
	te.tokenExtractedAt = time.Time{}
	te.mu.Unlock()

	return te.GetToken(ctx, email)
}

// needsRefresh checks if the cached token needs refresh
func (te *TokenExtractor) needsRefresh() bool {
	if te.cachedToken == "" || te.tokenExtractedAt.IsZero() {
		return true
	}
	return time.Since(te.tokenExtractedAt) > time.Duration(config.TokenRefreshIntervalMs)*time.Millisecond
}

// getTokenData gets fresh token data - tries cache first, then DB, then HTML page
func (te *TokenExtractor) getTokenData(ctx context.Context, email string) (string, error) {
	// Try Redis cache first
	if te.accountStore != nil && email != "" {
		cached, err := te.accountStore.GetCachedToken(ctx, email)
		if err == nil && cached != nil && cached.AccessToken != "" {
			utils.Info("[Token] Got cached token from Redis")
			return cached.AccessToken, nil
		}
	}

	// Try database (preferred - always has fresh token)
	dbData, err := GetAuthStatus("")
	if err == nil && dbData != nil && dbData.APIKey != "" {
		utils.Info("[Token] Got fresh token from SQLite database")
		return dbData.APIKey, nil
	}
	utils.Warn("[Token] DB extraction failed, trying HTML page...")

	// Fallback to HTML page
	token, err := te.extractChatParams()
	if err == nil && token != "" {
		utils.Warn("[Token] Got token from HTML page (may be stale)")
		return token, nil
	}
	utils.Warn("[Token] HTML page extraction failed: %v", err)

	return "", fmt.Errorf("could not extract token from Antigravity; make sure Antigravity is running and you are logged in")
}

// extractChatParams extracts chat params from Antigravity's HTML page (fallback method)
func (te *TokenExtractor) extractChatParams() (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/", config.AntigravityAuthPort)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("cannot connect to Antigravity on port %d; make sure Antigravity is running", config.AntigravityAuthPort)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Find the base64-encoded chatParams in the HTML
	re := regexp.MustCompile(`window\.chatParams\s*=\s*'([^']+)'`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not find chatParams in Antigravity page")
	}

	// Decode base64
	decoded, err := base64.StdEncoding.DecodeString(string(matches[1]))
	if err != nil {
		return "", fmt.Errorf("failed to decode chatParams: %w", err)
	}

	// Parse JSON
	var cfgData map[string]interface{}
	if err := json.Unmarshal(decoded, &cfgData); err != nil {
		return "", fmt.Errorf("failed to parse chatParams: %w", err)
	}

	if apiKey, ok := cfgData["apiKey"].(string); ok && apiKey != "" {
		return apiKey, nil
	}

	return "", fmt.Errorf("no apiKey found in chatParams")
}

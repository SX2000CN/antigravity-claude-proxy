// Package handlers provides HTTP handlers for the WebUI.
// This file corresponds to account-related handlers in src/webui/index.js in the Node.js version.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/auth"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// AccountsHandler handles account-related API endpoints
type AccountsHandler struct {
	accountManager *account.Manager
	cfg            *config.Config
	// OAuth state storage (state -> flow data)
	pendingOAuthFlows map[string]*OAuthFlowData
}

// OAuthFlowData represents a pending OAuth flow
type OAuthFlowData struct {
	Verifier       string
	State          string
	CallbackServer *auth.CallbackServer
	Timestamp      int64
}

// NewAccountsHandler creates a new AccountsHandler
func NewAccountsHandler(accountManager *account.Manager, cfg *config.Config) *AccountsHandler {
	return &AccountsHandler{
		accountManager:    accountManager,
		cfg:               cfg,
		pendingOAuthFlows: make(map[string]*OAuthFlowData),
	}
}

// ListAccounts handles GET /api/accounts
func (h *AccountsHandler) ListAccounts(c *gin.Context) {
	status := h.accountManager.GetStatus()

	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"accounts": status.Accounts,
		"summary": gin.H{
			"total":       status.Total,
			"available":   status.Available,
			"rateLimited": status.RateLimited,
			"invalid":     status.Invalid,
		},
	})
}

// RefreshAccount handles POST /api/accounts/:email/refresh
func (h *AccountsHandler) RefreshAccount(c *gin.Context) {
	email := c.Param("email")

	h.accountManager.ClearTokenCacheFor(email)
	h.accountManager.ClearProjectCacheFor(email)

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Token cache cleared for " + email,
	})
}

// ToggleAccountRequest represents the request body for toggling account
type ToggleAccountRequest struct {
	Enabled bool `json:"enabled"`
}

// ToggleAccount handles POST /api/accounts/:email/toggle
func (h *AccountsHandler) ToggleAccount(c *gin.Context) {
	email := c.Param("email")

	var req ToggleAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "enabled must be a boolean",
		})
		return
	}

	ctx := c.Request.Context()
	if err := h.accountManager.SetAccountEnabled(ctx, email, req.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Reload AccountManager to pick up changes
	if err := h.accountManager.Reload(ctx); err != nil {
		utils.Warn("[WebUI] Failed to reload accounts after toggle: %v", err)
	}

	status := "enabled"
	if !req.Enabled {
		status = "disabled"
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Account " + email + " " + status,
	})
}

// DeleteAccount handles DELETE /api/accounts/:email
func (h *AccountsHandler) DeleteAccount(c *gin.Context) {
	email := c.Param("email")

	ctx := c.Request.Context()
	if err := h.accountManager.RemoveAccount(ctx, email); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Reload AccountManager to pick up changes
	if err := h.accountManager.Reload(ctx); err != nil {
		utils.Warn("[WebUI] Failed to reload accounts after delete: %v", err)
	}

	utils.Info("[WebUI] Account %s removed", email)

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Account " + email + " removed",
	})
}

// UpdateAccountRequest represents the request body for updating account thresholds
type UpdateAccountRequest struct {
	QuotaThreshold       *float64           `json:"quotaThreshold"`
	ModelQuotaThresholds map[string]float64 `json:"modelQuotaThresholds"`
}

// UpdateAccount handles PATCH /api/accounts/:email
func (h *AccountsHandler) UpdateAccount(c *gin.Context) {
	email := c.Param("email")

	var req UpdateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid request body",
		})
		return
	}

	ctx := c.Request.Context()
	acc, err := h.accountManager.GetAccountByEmail(ctx, email)
	if err != nil || acc == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error",
			"error":  "Account " + email + " not found",
		})
		return
	}

	// Validate and update quotaThreshold
	if req.QuotaThreshold != nil {
		threshold := *req.QuotaThreshold
		if threshold < 0 || threshold >= 1 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "quotaThreshold must be 0-0.99 or null",
			})
			return
		}
		acc.QuotaThreshold = &threshold
	}

	// Validate and update modelQuotaThresholds
	if req.ModelQuotaThresholds != nil {
		for modelID, threshold := range req.ModelQuotaThresholds {
			if threshold < 0 || threshold >= 1 {
				c.JSON(http.StatusBadRequest, gin.H{
					"status": "error",
					"error":  "Invalid threshold for model " + modelID + ": must be 0-0.99",
				})
				return
			}
		}
		acc.ModelQuotaThresholds = req.ModelQuotaThresholds
	}

	// Save the account
	if err := h.accountManager.UpdateAccount(ctx, acc); err != nil {
		utils.Error("[WebUI] Error updating account thresholds: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Reload AccountManager to pick up changes
	if err := h.accountManager.Reload(ctx); err != nil {
		utils.Warn("[WebUI] Failed to reload accounts after update: %v", err)
	}

	utils.Info("[WebUI] Account %s thresholds updated", email)

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Account " + email + " thresholds updated",
		"account": gin.H{
			"email":                acc.Email,
			"quotaThreshold":       acc.QuotaThreshold,
			"modelQuotaThresholds": acc.ModelQuotaThresholds,
		},
	})
}

// ReloadAccounts handles POST /api/accounts/reload
func (h *AccountsHandler) ReloadAccounts(c *gin.Context) {
	ctx := c.Request.Context()
	if err := h.accountManager.Reload(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	status := h.accountManager.GetStatus()
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Accounts reloaded from disk",
		"summary": status.Summary,
	})
}

// ExportAccounts handles GET /api/accounts/export
func (h *AccountsHandler) ExportAccounts(c *gin.Context) {
	accounts := h.accountManager.GetAllAccounts()

	// Export only essential fields for portability
	exportData := make([]gin.H, 0)
	for _, acc := range accounts {
		if acc.Source == "database" {
			continue
		}

		essential := gin.H{"email": acc.Email}
		if acc.RefreshToken != "" {
			essential["refresh_token"] = acc.RefreshToken
		}
		if acc.APIKey != "" {
			essential["api_key"] = acc.APIKey
		}
		exportData = append(exportData, essential)
	}

	c.JSON(http.StatusOK, exportData)
}

// ImportAccountsRequest represents the request body for importing accounts
type ImportAccountsRequest struct {
	Accounts []ImportAccountData `json:"accounts"`
}

// ImportAccountData represents a single account to import
type ImportAccountData struct {
	Email        string `json:"email"`
	RefreshToken string `json:"refresh_token"`
	RefreshTok   string `json:"refreshToken"` // camelCase variant
	APIKey       string `json:"api_key"`
	ApiKey       string `json:"apiKey"` // camelCase variant
}

// ImportAccounts handles POST /api/accounts/import
func (h *AccountsHandler) ImportAccounts(c *gin.Context) {
	var rawData interface{}
	if err := c.ShouldBindJSON(&rawData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid JSON",
		})
		return
	}

	var importAccounts []map[string]interface{}

	// Support both wrapped format { accounts: [...] } and plain array [...]
	switch data := rawData.(type) {
	case []interface{}:
		for _, item := range data {
			if m, ok := item.(map[string]interface{}); ok {
				importAccounts = append(importAccounts, m)
			}
		}
	case map[string]interface{}:
		if accounts, ok := data["accounts"].([]interface{}); ok {
			for _, item := range accounts {
				if m, ok := item.(map[string]interface{}); ok {
					importAccounts = append(importAccounts, m)
				}
			}
		}
	}

	if len(importAccounts) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "accounts must be a non-empty array",
		})
		return
	}

	ctx := c.Request.Context()
	results := gin.H{
		"added":   []string{},
		"updated": []string{},
		"failed":  []gin.H{},
	}

	added := []string{}
	updated := []string{}
	failed := []gin.H{}

	// Get existing accounts
	existingAccounts := h.accountManager.GetAllAccounts()
	existingEmails := make(map[string]bool)
	for _, acc := range existingAccounts {
		existingEmails[acc.Email] = true
	}

	for _, accData := range importAccounts {
		email, _ := accData["email"].(string)
		if email == "" {
			failed = append(failed, gin.H{"email": "unknown", "reason": "Missing email"})
			continue
		}

		// Support both snake_case and camelCase
		refreshToken, _ := accData["refresh_token"].(string)
		if refreshToken == "" {
			refreshToken, _ = accData["refreshToken"].(string)
		}
		apiKey, _ := accData["api_key"].(string)
		if apiKey == "" {
			apiKey, _ = accData["apiKey"].(string)
		}

		if refreshToken == "" && apiKey == "" {
			failed = append(failed, gin.H{"email": email, "reason": "Missing refresh_token or api_key"})
			continue
		}

		exists := existingEmails[email]

		source := "oauth"
		if apiKey != "" {
			source = "manual"
		}

		newAcc := &redis.Account{
			Email:        email,
			Source:       source,
			RefreshToken: refreshToken,
			APIKey:       apiKey,
			Enabled:      true,
			IsInvalid:    false,
		}

		if err := h.accountManager.AddOrUpdateAccount(ctx, newAcc); err != nil {
			failed = append(failed, gin.H{"email": email, "reason": err.Error()})
			continue
		}

		if exists {
			updated = append(updated, email)
		} else {
			added = append(added, email)
		}
	}

	// Reload AccountManager
	if err := h.accountManager.Reload(ctx); err != nil {
		utils.Warn("[WebUI] Failed to reload accounts after import: %v", err)
	}

	results["added"] = added
	results["updated"] = updated
	results["failed"] = failed

	utils.Info("[WebUI] Import complete: %d added, %d updated, %d failed", len(added), len(updated), len(failed))

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"results": results,
		"message": "Imported " + string(rune(len(added)+len(updated))) + " accounts",
	})
}

// GetAuthURL handles GET /api/auth/url
func (h *AccountsHandler) GetAuthURL(c *gin.Context) {
	// Clean up old flows (> 10 mins)
	now := time.Now().UnixMilli()
	for key, val := range h.pendingOAuthFlows {
		if now-val.Timestamp > 10*60*1000 {
			delete(h.pendingOAuthFlows, key)
		}
	}

	// Generate OAuth URL using default redirect URI (localhost:51121)
	result, err := auth.GetAuthorizationURL("")
	if err != nil {
		utils.Error("[WebUI] Error generating auth URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Create callback server on port 51121 (same as CLI)
	callbackServer := auth.NewCallbackServer(result.State, 120000) // 2 min timeout

	// Store the flow data
	h.pendingOAuthFlows[result.State] = &OAuthFlowData{
		Verifier:       result.Verifier,
		State:          result.State,
		CallbackServer: callbackServer,
		Timestamp:      now,
	}

	// Start async handler for the OAuth callback
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		code, err := callbackServer.Start(ctx)
		if err != nil {
			if err != context.Canceled && err != context.DeadlineExceeded {
				utils.Error("[WebUI] OAuth callback server error: %v", err)
			}
			delete(h.pendingOAuthFlows, result.State)
			return
		}

		// Complete the OAuth flow
		utils.Info("[WebUI] Received OAuth callback, completing flow...")
		accountData, err := auth.CompleteOAuthFlow(ctx, code, result.Verifier)
		if err != nil {
			utils.Error("[WebUI] OAuth flow completion error: %v", err)
			delete(h.pendingOAuthFlows, result.State)
			return
		}

		// Add or update the account
		newAcc := &redis.Account{
			Email:        accountData.Email,
			RefreshToken: accountData.RefreshToken,
			Source:       "oauth",
			Enabled:      true,
		}

		if err := h.accountManager.AddOrUpdateAccount(context.Background(), newAcc); err != nil {
			utils.Error("[WebUI] Failed to add account: %v", err)
		} else {
			utils.Success("[WebUI] Account %s added successfully", accountData.Email)
		}

		// Reload AccountManager to pick up the new account
		if err := h.accountManager.Reload(context.Background()); err != nil {
			utils.Warn("[WebUI] Failed to reload accounts: %v", err)
		}

		delete(h.pendingOAuthFlows, result.State)
	}()

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"url":    result.URL,
		"state":  result.State,
	})
}

// CompleteOAuthRequest represents the request body for completing OAuth
type CompleteOAuthRequest struct {
	CallbackInput string `json:"callbackInput"`
	State         string `json:"state"`
}

// CompleteOAuth handles POST /api/auth/complete
func (h *AccountsHandler) CompleteOAuth(c *gin.Context) {
	var req CompleteOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Missing callbackInput or state",
		})
		return
	}

	if req.CallbackInput == "" || req.State == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Missing callbackInput or state",
		})
		return
	}

	// Find the pending flow
	flowData, ok := h.pendingOAuthFlows[req.State]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "OAuth flow not found. The account may have been already added via auto-callback. Please refresh the account list.",
		})
		return
	}

	// Extract code from input
	codeResult, err := auth.ExtractCodeFromInput(req.CallbackInput)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	ctx := c.Request.Context()

	// Complete the OAuth flow
	accountData, err := auth.CompleteOAuthFlow(ctx, codeResult.Code, flowData.Verifier)
	if err != nil {
		utils.Error("[WebUI] Manual OAuth completion error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Add or update the account
	newAcc := &redis.Account{
		Email:        accountData.Email,
		RefreshToken: accountData.RefreshToken,
		Source:       "oauth",
		Enabled:      true,
	}

	if err := h.accountManager.AddOrUpdateAccount(ctx, newAcc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Reload AccountManager to pick up the new account
	if err := h.accountManager.Reload(ctx); err != nil {
		utils.Warn("[WebUI] Failed to reload accounts: %v", err)
	}

	// Abort the callback server since manual completion succeeded
	if flowData.CallbackServer != nil {
		flowData.CallbackServer.Abort()
	}

	// Clean up
	delete(h.pendingOAuthFlows, req.State)

	utils.Success("[WebUI] Account %s added via manual callback", accountData.Email)

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"email":   accountData.Email,
		"message": "Account " + accountData.Email + " added successfully",
	})
}

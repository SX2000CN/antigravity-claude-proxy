// Package handlers provides HTTP handlers for the WebUI.
// This file corresponds to config-related handlers in src/webui/index.js in the Node.js version.
package handlers

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// Package version - should be set during build
var PackageVersion = "1.0.0"

// ConfigHandler handles configuration-related API endpoints
type ConfigHandler struct {
	cfg            *config.Config
	accountManager *account.Manager
}

// NewConfigHandler creates a new ConfigHandler
func NewConfigHandler(cfg *config.Config, accountManager *account.Manager) *ConfigHandler {
	return &ConfigHandler{
		cfg:            cfg,
		accountManager: accountManager,
	}
}

// GetConfig handles GET /api/config
func (h *ConfigHandler) GetConfig(c *gin.Context) {
	publicConfig := h.cfg.GetPublic()

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"config":  publicConfig,
		"version": PackageVersion,
		"note":    "Edit ~/.config/antigravity-proxy/config.json or use env vars to change these values",
	})
}

// UpdateConfigRequest represents the request body for updating config
type UpdateConfigRequest struct {
	Debug                  *bool                   `json:"debug"`
	DevMode                *bool                   `json:"devMode"`
	LogLevel               *string                 `json:"logLevel"`
	MaxRetries             *int                    `json:"maxRetries"`
	RetryBaseMs            *int64                  `json:"retryBaseMs"`
	RetryMaxMs             *int64                  `json:"retryMaxMs"`
	PersistTokenCache      *bool                   `json:"persistTokenCache"`
	DefaultCooldownMs      *int64                  `json:"defaultCooldownMs"`
	MaxWaitBeforeErrorMs   *int64                  `json:"maxWaitBeforeErrorMs"`
	MaxAccounts            *int                    `json:"maxAccounts"`
	GlobalQuotaThreshold   *float64                `json:"globalQuotaThreshold"`
	AccountSelection       map[string]interface{}  `json:"accountSelection"`
	RateLimitDedupWindowMs *int64                  `json:"rateLimitDedupWindowMs"`
	MaxConsecutiveFailures *int                    `json:"maxConsecutiveFailures"`
	ExtendedCooldownMs     *int64                  `json:"extendedCooldownMs"`
	MaxCapacityRetries     *int                    `json:"maxCapacityRetries"`
}

// UpdateConfig handles POST /api/config
func (h *ConfigHandler) UpdateConfig(c *gin.Context) {
	var req UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid request body",
		})
		return
	}

	updates := make(map[string]interface{})

	// Handle devMode/debug (they are linked)
	if req.DevMode != nil {
		updates["devMode"] = *req.DevMode
		updates["debug"] = *req.DevMode
		utils.SetDebug(*req.DevMode)
	} else if req.Debug != nil {
		updates["debug"] = *req.Debug
		updates["devMode"] = *req.Debug
		utils.SetDebug(*req.Debug)
	}

	if req.LogLevel != nil {
		validLevels := []string{"info", "warn", "error", "debug"}
		valid := false
		for _, l := range validLevels {
			if l == *req.LogLevel {
				valid = true
				break
			}
		}
		if valid {
			updates["logLevel"] = *req.LogLevel
		}
	}

	if req.MaxRetries != nil && *req.MaxRetries >= 0 && *req.MaxRetries <= 20 {
		updates["maxRetries"] = *req.MaxRetries
	}

	if req.RetryBaseMs != nil && *req.RetryBaseMs >= 100 && *req.RetryBaseMs <= 10000 {
		updates["retryBaseMs"] = *req.RetryBaseMs
	}

	if req.RetryMaxMs != nil && *req.RetryMaxMs >= 1000 && *req.RetryMaxMs <= 60000 {
		updates["retryMaxMs"] = *req.RetryMaxMs
	}

	if req.PersistTokenCache != nil {
		updates["persistTokenCache"] = *req.PersistTokenCache
	}

	if req.DefaultCooldownMs != nil && *req.DefaultCooldownMs >= 0 && *req.DefaultCooldownMs <= 600000 {
		updates["defaultCooldownMs"] = *req.DefaultCooldownMs
	}

	if req.MaxWaitBeforeErrorMs != nil && *req.MaxWaitBeforeErrorMs >= 60000 && *req.MaxWaitBeforeErrorMs <= 1800000 {
		updates["maxWaitBeforeErrorMs"] = *req.MaxWaitBeforeErrorMs
	}

	if req.MaxAccounts != nil && *req.MaxAccounts >= 1 && *req.MaxAccounts <= 100 {
		updates["maxAccounts"] = *req.MaxAccounts
	}

	if req.GlobalQuotaThreshold != nil && *req.GlobalQuotaThreshold >= 0 && *req.GlobalQuotaThreshold < 1 {
		updates["globalQuotaThreshold"] = *req.GlobalQuotaThreshold
	}

	if req.RateLimitDedupWindowMs != nil && *req.RateLimitDedupWindowMs >= 1000 && *req.RateLimitDedupWindowMs <= 30000 {
		updates["rateLimitDedupWindowMs"] = *req.RateLimitDedupWindowMs
	}

	if req.MaxConsecutiveFailures != nil && *req.MaxConsecutiveFailures >= 1 && *req.MaxConsecutiveFailures <= 10 {
		updates["maxConsecutiveFailures"] = *req.MaxConsecutiveFailures
	}

	if req.ExtendedCooldownMs != nil && *req.ExtendedCooldownMs >= 10000 && *req.ExtendedCooldownMs <= 300000 {
		updates["extendedCooldownMs"] = *req.ExtendedCooldownMs
	}

	if req.MaxCapacityRetries != nil && *req.MaxCapacityRetries >= 1 && *req.MaxCapacityRetries <= 10 {
		updates["maxCapacityRetries"] = *req.MaxCapacityRetries
	}

	// Account selection strategy validation
	if req.AccountSelection != nil {
		if strategy, ok := req.AccountSelection["strategy"].(string); ok {
			validStrategies := []string{"sticky", "round-robin", "hybrid"}
			valid := false
			for _, s := range validStrategies {
				if s == strategy {
					valid = true
					break
				}
			}
			if valid {
				updates["accountSelection"] = map[string]interface{}{
					"strategy": strategy,
				}
			}
		}
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "No valid configuration updates provided",
		})
		return
	}

	if err := h.cfg.Update(updates); err != nil {
		utils.Error("[WebUI] Error updating config: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "Failed to save configuration file",
		})
		return
	}

	// Hot-reload strategy if it was changed (no server restart needed)
	if req.AccountSelection != nil {
		if strategy, ok := req.AccountSelection["strategy"].(string); ok && h.accountManager != nil {
			if err := h.accountManager.Reload(c.Request.Context()); err != nil {
				utils.Error("[WebUI] Failed to hot-reload strategy: %v", err)
			} else {
				utils.Info("[WebUI] Strategy hot-reloaded to: %s", strategy)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Configuration saved. Restart server to apply some changes.",
		"updates": updates,
		"config":  h.cfg.GetPublic(),
	})
}

// ChangePasswordRequest represents the request body for changing password
type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// ChangePassword handles POST /api/config/password
func (h *ConfigHandler) ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid request body",
		})
		return
	}

	if req.NewPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "New password is required",
		})
		return
	}

	// If current password exists, verify old password
	if h.cfg.WebUIPassword != "" && h.cfg.WebUIPassword != req.OldPassword {
		c.JSON(http.StatusForbidden, gin.H{
			"status": "error",
			"error":  "Invalid current password",
		})
		return
	}

	// Save new password
	if err := h.cfg.Update(map[string]interface{}{
		"webuiPassword": req.NewPassword,
	}); err != nil {
		utils.Error("[WebUI] Error changing password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "Failed to save password to config file",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Password changed successfully",
	})
}

// GetSettings handles GET /api/settings
func (h *ConfigHandler) GetSettings(c *gin.Context) {
	settings := make(map[string]interface{})

	// Get default port from environment or config
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	settings["port"] = port

	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"settings": settings,
	})
}

// GetStrategyHealth handles GET /api/strategy/health
func (h *ConfigHandler) GetStrategyHealth(c *gin.Context) {
	if !h.cfg.DevMode {
		c.JSON(http.StatusForbidden, gin.H{
			"status": "error",
			"error":  "Developer mode is not enabled",
		})
		return
	}

	healthData := h.accountManager.GetStrategyHealthData()

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"strategy":    healthData.Strategy,
		"accounts":    healthData.Accounts,
		"lastUpdated": healthData.LastUpdated,
	})
}

// UpdateModelConfigRequest represents the request body for updating model config
type UpdateModelConfigRequest struct {
	ModelID string                 `json:"modelId"`
	Config  map[string]interface{} `json:"config"`
}

// UpdateModelConfig handles POST /api/models/config
func (h *ConfigHandler) UpdateModelConfig(c *gin.Context) {
	var req UpdateModelConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid parameters",
		})
		return
	}

	if req.ModelID == "" || req.Config == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid parameters",
		})
		return
	}

	// Load current config
	currentMapping := h.cfg.ModelMapping
	if currentMapping == nil {
		currentMapping = make(map[string]string)
	}

	// For model config, we might need to store as JSON string
	// Since ModelMapping is map[string]string, we'll need to handle this differently
	// For now, just acknowledge the request

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"modelConfig": req.Config,
	})
}

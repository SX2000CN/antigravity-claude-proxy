// Package webui provides the web management interface.
// This file corresponds to src/webui/index.js in the Node.js version.
package webui

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/modules"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/internal/webui/handlers"
)

// Router provides the WebUI router and handlers
type Router struct {
	accountManager  *account.Manager
	cfg             *config.Config
	usageStats      *modules.UsageStats
	accountsHandler *handlers.AccountsHandler
	configHandler   *handlers.ConfigHandler
	logsHandler     *handlers.LogsHandler
	claudeHandler   *handlers.ClaudeHandler
	presetsHandler  *handlers.PresetsHandler
}

// NewRouter creates a new WebUI router
func NewRouter(accountManager *account.Manager, cfg *config.Config, usageStats *modules.UsageStats) *Router {
	return &Router{
		accountManager:  accountManager,
		cfg:             cfg,
		usageStats:      usageStats,
		accountsHandler: handlers.NewAccountsHandler(accountManager, cfg),
		configHandler:   handlers.NewConfigHandler(cfg, accountManager),
		logsHandler:     handlers.NewLogsHandler(),
		claudeHandler:   handlers.NewClaudeHandler(),
		presetsHandler:  handlers.NewPresetsHandler(),
	}
}

// Mount mounts WebUI routes on the given Gin router group
func (r *Router) Mount(engine *gin.Engine, publicDir string) {
	// Apply auth middleware
	engine.Use(AuthMiddleware(r.cfg))

	// Store publicDir for static file serving via NoRoute (to avoid route conflicts)
	absPath := ""
	if publicDir != "" {
		var err error
		absPath, err = filepath.Abs(publicDir)
		if err != nil {
			utils.Warn("[WebUI] Failed to get absolute path for public dir: %v", err)
			absPath = publicDir
		}
	}

	// ==========================================
	// Account Management API
	// ==========================================

	// GET /api/accounts - List all accounts with status
	engine.GET("/api/accounts", r.accountsHandler.ListAccounts)

	// POST /api/accounts/:email/refresh - Refresh specific account token
	engine.POST("/api/accounts/:email/refresh", r.accountsHandler.RefreshAccount)

	// POST /api/accounts/:email/toggle - Enable/disable account
	engine.POST("/api/accounts/:email/toggle", r.accountsHandler.ToggleAccount)

	// DELETE /api/accounts/:email - Remove account
	engine.DELETE("/api/accounts/:email", r.accountsHandler.DeleteAccount)

	// PATCH /api/accounts/:email - Update account settings (thresholds)
	engine.PATCH("/api/accounts/:email", r.accountsHandler.UpdateAccount)

	// POST /api/accounts/reload - Reload accounts from disk
	engine.POST("/api/accounts/reload", r.accountsHandler.ReloadAccounts)

	// GET /api/accounts/export - Export accounts
	engine.GET("/api/accounts/export", r.accountsHandler.ExportAccounts)

	// POST /api/accounts/import - Batch import accounts
	engine.POST("/api/accounts/import", r.accountsHandler.ImportAccounts)

	// ==========================================
	// Configuration API
	// ==========================================

	// GET /api/config - Get server configuration
	engine.GET("/api/config", r.configHandler.GetConfig)

	// POST /api/config - Update server configuration
	engine.POST("/api/config", r.configHandler.UpdateConfig)

	// POST /api/config/password - Change WebUI password
	engine.POST("/api/config/password", r.configHandler.ChangePassword)

	// GET /api/settings - Get runtime settings
	engine.GET("/api/settings", r.configHandler.GetSettings)

	// GET /api/strategy/health - Get strategy health data
	engine.GET("/api/strategy/health", r.configHandler.GetStrategyHealth)

	// POST /api/models/config - Update model configuration
	engine.POST("/api/models/config", r.configHandler.UpdateModelConfig)

	// ==========================================
	// Server Configuration Presets API
	// ==========================================

	// GET /api/server/presets - List all server config presets
	engine.GET("/api/server/presets", r.presetsHandler.ListPresets)

	// POST /api/server/presets - Save a custom server config preset
	engine.POST("/api/server/presets", r.presetsHandler.CreatePreset)

	// PATCH /api/server/presets/:name - Update custom preset metadata and/or config
	engine.PATCH("/api/server/presets/:name", r.presetsHandler.UpdatePreset)

	// DELETE /api/server/presets/:name - Delete a custom server config preset
	engine.DELETE("/api/server/presets/:name", r.presetsHandler.DeletePreset)

	// ==========================================
	// Claude CLI Configuration API
	// ==========================================

	// GET /api/claude/config - Get Claude CLI configuration
	engine.GET("/api/claude/config", r.claudeHandler.GetClaudeConfig)

	// POST /api/claude/config - Update Claude CLI configuration
	engine.POST("/api/claude/config", r.claudeHandler.UpdateClaudeConfig)

	// POST /api/claude/config/restore - Restore Claude CLI to default
	engine.POST("/api/claude/config/restore", r.claudeHandler.RestoreClaudeConfig)

	// GET /api/claude/mode - Get current mode (proxy or paid)
	engine.GET("/api/claude/mode", r.claudeHandler.GetClaudeMode)

	// POST /api/claude/mode - Switch between proxy and paid mode
	engine.POST("/api/claude/mode", r.claudeHandler.SetClaudeMode)

	// GET /api/claude/presets - Get all saved presets
	engine.GET("/api/claude/presets", r.claudeHandler.GetPresets)

	// POST /api/claude/presets - Save a new preset
	engine.POST("/api/claude/presets", r.claudeHandler.SavePreset)

	// DELETE /api/claude/presets/:name - Delete a preset
	engine.DELETE("/api/claude/presets/:name", r.claudeHandler.DeletePreset)

	// ==========================================
	// Logs API
	// ==========================================

	// GET /api/logs - Get log history
	engine.GET("/api/logs", r.logsHandler.GetLogs)

	// GET /api/logs/stream - Stream logs via SSE
	engine.GET("/api/logs/stream", r.logsHandler.StreamLogs)

	// ==========================================
	// OAuth API
	// ==========================================

	// GET /api/auth/url - Get OAuth URL to start the flow
	engine.GET("/api/auth/url", r.accountsHandler.GetAuthURL)

	// POST /api/auth/complete - Complete OAuth with manually submitted callback URL/code
	engine.POST("/api/auth/complete", r.accountsHandler.CompleteOAuth)

	// ==========================================
	// Static File Serving (NoRoute fallback)
	// ==========================================
	// Use NoRoute to serve static files as a fallback to avoid conflicts with API routes
	if absPath != "" {
		engine.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path

			// Don't serve static files for API routes
			if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/v1/") {
				c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
				return
			}

			// Try to serve the requested file
			filePath := filepath.Join(absPath, path)
			if _, err := os.Stat(filePath); err == nil {
				c.File(filePath)
				return
			}

			// Fall back to index.html for SPA routing
			indexPath := filepath.Join(absPath, "index.html")
			if _, err := os.Stat(indexPath); err == nil {
				c.File(indexPath)
				return
			}

			c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
		})
	}

	utils.Info("[WebUI] Mounted at /")
}

// MountWebUI is a convenience function to mount WebUI on an existing Gin engine
func MountWebUI(engine *gin.Engine, publicDir string, accountManager *account.Manager, cfg *config.Config, usageStats *modules.UsageStats) {
	router := NewRouter(accountManager, cfg, usageStats)
	router.Mount(engine, publicDir)
}

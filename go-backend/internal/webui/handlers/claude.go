// Package handlers provides HTTP handlers for the WebUI.
// This file corresponds to Claude CLI config handlers in src/webui/index.js in the Node.js version.
package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// ClaudeHandler handles Claude CLI configuration API endpoints
type ClaudeHandler struct{}

// NewClaudeHandler creates a new ClaudeHandler
func NewClaudeHandler() *ClaudeHandler {
	return &ClaudeHandler{}
}

// getClaudeConfigPath returns the path to Claude CLI settings.json
func getClaudeConfigPath() string {
	home := utils.GetHomeDir()

	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, ".claude", "settings.json")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "settings.json")
	default:
		return filepath.Join(home, ".config", "Claude", "settings.json")
	}
}

// readClaudeConfig reads the Claude CLI configuration
func readClaudeConfig() (map[string]interface{}, error) {
	configPath := getClaudeConfigPath()

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, err
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return config, nil
}

// writeClaudeConfig writes the Claude CLI configuration
func writeClaudeConfig(config map[string]interface{}) error {
	configPath := getClaudeConfigPath()

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := utils.EnsureDir(dir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// GetClaudeConfig handles GET /api/claude/config
func (h *ClaudeHandler) GetClaudeConfig(c *gin.Context) {
	config, err := readClaudeConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"config": config,
		"path":   getClaudeConfigPath(),
	})
}

// UpdateClaudeConfig handles POST /api/claude/config
func (h *ClaudeHandler) UpdateClaudeConfig(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid config updates",
		})
		return
	}

	// Read existing config
	config, err := readClaudeConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Merge updates
	for k, v := range updates {
		config[k] = v
	}

	// Write updated config
	if err := writeClaudeConfig(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"config":  config,
		"message": "Claude configuration updated",
	})
}

// RestoreClaudeConfig handles POST /api/claude/config/restore
func (h *ClaudeHandler) RestoreClaudeConfig(c *gin.Context) {
	config, err := readClaudeConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Proxy-related environment variables to remove when restoring defaults
	proxyEnvVars := []string{
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ENABLE_EXPERIMENTAL_MCP_CLI",
	}

	// Remove proxy-related environment variables
	if env, ok := config["env"].(map[string]interface{}); ok {
		for _, key := range proxyEnvVars {
			delete(env, key)
		}
		if len(env) == 0 {
			delete(config, "env")
		}
	}

	// Write updated config
	if err := writeClaudeConfig(config); err != nil {
		utils.Error("[WebUI] Error restoring Claude config: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	utils.Info("[WebUI] Restored Claude CLI config to defaults at %s", getClaudeConfigPath())

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"config":  config,
		"message": "Claude CLI configuration restored to defaults",
	})
}

// GetClaudeMode handles GET /api/claude/mode
func (h *ClaudeHandler) GetClaudeMode(c *gin.Context) {
	config, err := readClaudeConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	baseURL := ""
	if env, ok := config["env"].(map[string]interface{}); ok {
		if url, ok := env["ANTHROPIC_BASE_URL"].(string); ok {
			baseURL = url
		}
	}

	// Determine mode based on ANTHROPIC_BASE_URL
	isProxy := baseURL != "" && (strings.Contains(baseURL, "localhost") ||
		strings.Contains(baseURL, "127.0.0.1") ||
		strings.Contains(baseURL, "::1") ||
		strings.Contains(baseURL, "0.0.0.0"))

	mode := "paid"
	if isProxy {
		mode = "proxy"
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"mode":   mode,
	})
}

// SetClaudeModeRequest represents the request body for setting Claude mode
type SetClaudeModeRequest struct {
	Mode string `json:"mode"`
}

// Default proxy config (matches DEFAULT_PRESETS[0] in Node.js version)
var defaultProxyEnv = map[string]interface{}{
	"ANTHROPIC_BASE_URL":              "http://localhost:8080",
	"ANTHROPIC_AUTH_TOKEN":            "sk-antigravity",
	"ANTHROPIC_MODEL":                 "claude-opus-4-5-thinking",
	"CLAUDE_CODE_SUBAGENT_MODEL":      "claude-sonnet-4-5-thinking",
	"ANTHROPIC_DEFAULT_OPUS_MODEL":    "claude-opus-4-5-thinking",
	"ANTHROPIC_DEFAULT_SONNET_MODEL":  "claude-sonnet-4-5-thinking",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL":   "gemini-3-flash",
	"ENABLE_EXPERIMENTAL_MCP_CLI":     "1",
}

// SetClaudeMode handles POST /api/claude/mode
func (h *ClaudeHandler) SetClaudeMode(c *gin.Context) {
	var req SetClaudeModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid request body",
		})
		return
	}

	if req.Mode != "proxy" && req.Mode != "paid" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "mode must be \"proxy\" or \"paid\"",
		})
		return
	}

	config, err := readClaudeConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	if req.Mode == "proxy" {
		// Switch to proxy mode - use default preset config
		config["env"] = defaultProxyEnv
	} else {
		// Switch to paid mode - remove env entirely
		delete(config, "env")
	}

	// Write updated config
	if err := writeClaudeConfig(config); err != nil {
		utils.Error("[WebUI] Error switching mode: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	utils.Info("[WebUI] Switched Claude CLI to %s mode", req.Mode)

	message := "Switched to Paid (Anthropic API) mode. Restart Claude CLI to apply."
	if req.Mode == "proxy" {
		message = "Switched to Proxy mode. Restart Claude CLI to apply."
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"mode":    req.Mode,
		"config":  config,
		"message": message,
	})
}

// Presets handling

// getPresetsPath returns the path to presets.json
func getPresetsPath() string {
	home := utils.GetHomeDir()
	return filepath.Join(home, ".config", "antigravity-proxy", "presets.json")
}

// readPresets reads saved presets
func readPresets() ([]map[string]interface{}, error) {
	presetsPath := getPresetsPath()

	data, err := os.ReadFile(presetsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []map[string]interface{}{}, nil
		}
		return nil, err
	}

	var presets []map[string]interface{}
	if err := json.Unmarshal(data, &presets); err != nil {
		return nil, err
	}

	return presets, nil
}

// savePresets writes presets to file
func savePresets(presets []map[string]interface{}) error {
	presetsPath := getPresetsPath()

	// Ensure directory exists
	dir := filepath.Dir(presetsPath)
	if err := utils.EnsureDir(dir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(presets, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(presetsPath, data, 0644)
}

// GetPresets handles GET /api/claude/presets
func (h *ClaudeHandler) GetPresets(c *gin.Context) {
	presets, err := readPresets()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"presets": presets,
	})
}

// SavePresetRequest represents the request body for saving a preset
type SavePresetRequest struct {
	Name   string                 `json:"name"`
	Config map[string]interface{} `json:"config"`
}

// SavePreset handles POST /api/claude/presets
func (h *ClaudeHandler) SavePreset(c *gin.Context) {
	var req SavePresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid request body",
		})
		return
	}

	if req.Name == "" || strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Preset name is required",
		})
		return
	}

	if req.Config == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Config object is required",
		})
		return
	}

	presets, err := readPresets()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Check if preset with same name exists
	found := false
	for i, p := range presets {
		if name, ok := p["name"].(string); ok && name == strings.TrimSpace(req.Name) {
			// Update existing preset
			presets[i]["config"] = req.Config
			found = true
			break
		}
	}

	if !found {
		// Add new preset
		presets = append(presets, map[string]interface{}{
			"name":   strings.TrimSpace(req.Name),
			"config": req.Config,
		})
	}

	if err := savePresets(presets); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"presets": presets,
		"message": "Preset \"" + strings.TrimSpace(req.Name) + "\" saved",
	})
}

// DeletePreset handles DELETE /api/claude/presets/:name
func (h *ClaudeHandler) DeletePreset(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Preset name is required",
		})
		return
	}

	presets, err := readPresets()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// Find and remove preset
	found := false
	for i, p := range presets {
		if pname, ok := p["name"].(string); ok && pname == name {
			presets = append(presets[:i], presets[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error",
			"error":  "Preset not found",
		})
		return
	}

	if err := savePresets(presets); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"presets": presets,
		"message": "Preset \"" + name + "\" deleted",
	})
}

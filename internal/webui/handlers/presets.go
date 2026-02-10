// Package handlers provides HTTP handlers for the WebUI.
// This file corresponds to server presets handlers in src/webui/index.js in the Node.js version.
package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// PresetsHandler handles server presets API endpoints
type PresetsHandler struct {
	presetsManager *config.ServerPresetsManager
}

// NewPresetsHandler creates a new PresetsHandler
func NewPresetsHandler() *PresetsHandler {
	return &PresetsHandler{
		presetsManager: config.GetServerPresetsManager(),
	}
}

// ListPresets handles GET /api/server/presets - List all server config presets
func (h *PresetsHandler) ListPresets(c *gin.Context) {
	presets, err := h.presetsManager.ReadServerPresets()
	if err != nil {
		utils.Error("[WebUI] Error reading server presets: %v", err)
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

// CreatePresetRequest represents the request body for creating a preset
type CreatePresetRequest struct {
	Name        string                   `json:"name"`
	Config      config.ServerPresetConfig `json:"config"`
	Description string                   `json:"description,omitempty"`
}

// CreatePreset handles POST /api/server/presets - Save a custom server config preset
func (h *PresetsHandler) CreatePreset(c *gin.Context) {
	var req CreatePresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid request body",
		})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Preset name is required",
		})
		return
	}

	// Validate name length (max 50 characters)
	if len(name) > 50 {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Preset name must be 50 characters or less",
		})
		return
	}

	// Validate config
	if err := validatePresetConfig(&req.Config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	presets, err := h.presetsManager.SaveServerPreset(name, req.Config, req.Description)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "cannot overwrite") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{
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

// UpdatePresetRequest represents the request body for updating a preset
type UpdatePresetRequest struct {
	Name        *string                    `json:"name,omitempty"`
	Description *string                    `json:"description,omitempty"`
	Config      *config.ServerPresetConfig `json:"config,omitempty"`
}

// UpdatePreset handles PATCH /api/server/presets/:name - Update custom preset metadata and/or config
func (h *PresetsHandler) UpdatePreset(c *gin.Context) {
	currentName := c.Param("name")
	if currentName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Preset name is required",
		})
		return
	}

	var req UpdatePresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Invalid request body",
		})
		return
	}

	// Validate new name if provided
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "Preset name cannot be empty",
			})
			return
		}
		// Validate name length (max 50 characters)
		if len(trimmed) > 50 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "Preset name must be 50 characters or less",
			})
			return
		}
		req.Name = &trimmed
	}

	// Validate config if provided
	if req.Config != nil {
		if err := validatePresetConfig(req.Config); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  err.Error(),
			})
			return
		}
	}

	updates := config.UpdateServerPresetRequest{
		Name:        req.Name,
		Description: req.Description,
		Config:      req.Config,
	}

	presets, err := h.presetsManager.UpdateServerPreset(currentName, updates)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "cannot edit") ||
			strings.Contains(err.Error(), "cannot use") {
			status = http.StatusConflict
		} else if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{
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

// DeletePreset handles DELETE /api/server/presets/:name - Delete a custom server config preset
func (h *PresetsHandler) DeletePreset(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "Preset name is required",
		})
		return
	}

	presets, err := h.presetsManager.DeleteServerPreset(name)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "cannot delete") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{
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

// validatePresetConfig validates the preset configuration values
// Ranges synchronized with Node.js version (src/webui/index.js validateConfigFields)
func validatePresetConfig(cfg *config.ServerPresetConfig) error {
	// Basic validation - ranges match Node.js version
	if cfg.MaxRetries < 1 || cfg.MaxRetries > 20 {
		return &validationError{"maxRetries must be between 1 and 20"}
	}
	if cfg.RetryBaseMs < 100 || cfg.RetryBaseMs > 10000 {
		return &validationError{"retryBaseMs must be between 100 and 10000"}
	}
	if cfg.RetryMaxMs < 1000 || cfg.RetryMaxMs > 120000 {
		return &validationError{"retryMaxMs must be between 1000 and 120000"}
	}
	if cfg.DefaultCooldownMs < 1000 || cfg.DefaultCooldownMs > 300000 {
		return &validationError{"defaultCooldownMs must be between 1000 and 300000"}
	}
	if cfg.MaxWaitBeforeErrorMs < 0 || cfg.MaxWaitBeforeErrorMs > 600000 {
		return &validationError{"maxWaitBeforeErrorMs must be between 0 and 600000"}
	}
	if cfg.MaxAccounts < 1 || cfg.MaxAccounts > 100 {
		return &validationError{"maxAccounts must be between 1 and 100"}
	}
	if cfg.GlobalQuotaThreshold < 0 || cfg.GlobalQuotaThreshold >= 1 {
		return &validationError{"globalQuotaThreshold must be between 0 and 0.99"}
	}
	if cfg.RateLimitDedupWindowMs < 1000 || cfg.RateLimitDedupWindowMs > 30000 {
		return &validationError{"rateLimitDedupWindowMs must be between 1000 and 30000"}
	}
	if cfg.MaxConsecutiveFailures < 1 || cfg.MaxConsecutiveFailures > 10 {
		return &validationError{"maxConsecutiveFailures must be between 1 and 10"}
	}
	if cfg.ExtendedCooldownMs < 10000 || cfg.ExtendedCooldownMs > 300000 {
		return &validationError{"extendedCooldownMs must be between 10000 and 300000"}
	}
	if cfg.MaxCapacityRetries < 1 || cfg.MaxCapacityRetries > 10 {
		return &validationError{"maxCapacityRetries must be between 1 and 10"}
	}
	if cfg.SwitchAccountDelayMs < 1000 || cfg.SwitchAccountDelayMs > 60000 {
		return &validationError{"switchAccountDelayMs must be between 1000 and 60000"}
	}

	// Validate capacityBackoffTiersMs array
	if len(cfg.CapacityBackoffTiersMs) < 1 || len(cfg.CapacityBackoffTiersMs) > 10 {
		return &validationError{"capacityBackoffTiersMs must have 1 to 10 elements"}
	}
	for i, v := range cfg.CapacityBackoffTiersMs {
		if v < 1000 || v > 300000 {
			return &validationError{fmt.Sprintf("capacityBackoffTiersMs[%d] must be between 1000 and 300000", i)}
		}
	}

	// Validate strategy
	validStrategies := map[string]bool{"sticky": true, "round-robin": true, "hybrid": true}
	if !validStrategies[cfg.AccountSelection.Strategy] {
		return &validationError{"accountSelection.strategy must be one of: sticky, round-robin, hybrid"}
	}

	return nil
}

type validationError struct {
	message string
}

func (e *validationError) Error() string {
	return e.message
}

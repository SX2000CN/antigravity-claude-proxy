// Package handlers provides HTTP request handlers for the server.
// This file handles model listing endpoints.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/cloudcode"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// ModelsHandler handles model listing endpoints
type ModelsHandler struct {
	accountManager *account.Manager
}

// NewModelsHandler creates a new ModelsHandler
func NewModelsHandler(accountManager *account.Manager) *ModelsHandler {
	return &ModelsHandler{
		accountManager: accountManager,
	}
}

// ListModels handles GET /v1/models - OpenAI-compatible format
func (h *ModelsHandler) ListModels(c *gin.Context) {
	ctx := c.Request.Context()

	// Select an account to get token
	result, err := h.accountManager.SelectAccount(ctx, "", account.SelectOptions{})
	if err != nil || result.Account == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "api_error",
				"message": "No accounts available",
			},
		})
		return
	}

	token, err := h.accountManager.GetTokenForAccount(ctx, result.Account)
	if err != nil {
		utils.Error("[API] Error getting token for models:", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "api_error",
				"message": err.Error(),
			},
		})
		return
	}

	models, err := cloudcode.ListModels(ctx, token)
	if err != nil {
		utils.Error("[API] Error listing models:", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "api_error",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, models)
}

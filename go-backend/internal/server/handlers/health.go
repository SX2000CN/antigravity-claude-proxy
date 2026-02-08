// Package handlers provides HTTP request handlers for the server.
// This file handles health check endpoints.
package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/cloudcode"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// HealthHandler handles health check endpoints
type HealthHandler struct {
	accountManager *account.Manager
}

// NewHealthHandler creates a new HealthHandler
func NewHealthHandler(accountManager *account.Manager) *HealthHandler {
	return &HealthHandler{
		accountManager: accountManager,
	}
}

// Health handles GET /health - Detailed status check
func (h *HealthHandler) Health(c *gin.Context) {
	start := time.Now()

	// Get high-level status first
	status := h.accountManager.GetStatus()
	allAccounts := h.accountManager.GetAllAccounts()

	// Build account details
	type accountDetail struct {
		Email                     string                 `json:"email"`
		Status                    string                 `json:"status"`
		Error                     string                 `json:"error,omitempty"`
		LastUsed                  string                 `json:"lastUsed,omitempty"`
		ModelRateLimits           map[string]interface{} `json:"modelRateLimits,omitempty"`
		RateLimitCooldownRemaining int64                 `json:"rateLimitCooldownRemaining"`
		Models                    map[string]interface{} `json:"models,omitempty"`
	}

	detailedAccounts := make([]accountDetail, 0, len(allAccounts))

	for _, acc := range allAccounts {
		detail := accountDetail{
			Email:           acc.Email,
			ModelRateLimits: make(map[string]interface{}),
			Models:          make(map[string]interface{}),
		}

		// Format last used time
		if acc.LastUsed > 0 {
			detail.LastUsed = time.UnixMilli(acc.LastUsed).Format(time.RFC3339)
		}

		// Check model-specific rate limits
		now := time.Now().UnixMilli()
		var soonestReset int64 = 0
		isRateLimited := false

		for modelID, limit := range acc.ModelRateLimits {
			if limit.IsRateLimited && limit.ResetTime > now {
				isRateLimited = true
				if soonestReset == 0 || limit.ResetTime < soonestReset {
					soonestReset = limit.ResetTime
				}
			}
			detail.ModelRateLimits[modelID] = map[string]interface{}{
				"isRateLimited": limit.IsRateLimited,
				"resetTime":     limit.ResetTime,
			}
		}

		if soonestReset > 0 {
			detail.RateLimitCooldownRemaining = soonestReset - now
		}

		// Skip invalid accounts for quota check
		if acc.IsInvalid {
			detail.Status = "invalid"
			detail.Error = acc.InvalidReason
			detailedAccounts = append(detailedAccounts, detail)
			continue
		}

		// Try to get quota info
		ctx := c.Request.Context()
		token, err := h.accountManager.GetTokenForAccount(ctx, acc)
		if err != nil {
			detail.Status = "error"
			detail.Error = err.Error()
			detailedAccounts = append(detailedAccounts, detail)
			continue
		}

		projectID := ""
		if acc.Subscription != nil {
			projectID = acc.Subscription.ProjectID
		}

		quotas, err := cloudcode.GetModelQuotas(ctx, token, projectID)
		if err != nil {
			detail.Status = "error"
			detail.Error = err.Error()
			detailedAccounts = append(detailedAccounts, detail)
			continue
		}

		// Format quotas for readability
		for modelID, info := range quotas {
			remaining := "N/A"
			var remainingFraction float64
			if info.RemainingFraction != nil && *info.RemainingFraction >= 0 {
				remainingFraction = *info.RemainingFraction
				remaining = utils.FormatPercent(remainingFraction)
			}

			resetTime := ""
			if info.ResetTime != nil {
				resetTime = *info.ResetTime
			}

			detail.Models[modelID] = map[string]interface{}{
				"remaining":         remaining,
				"remainingFraction": remainingFraction,
				"resetTime":         resetTime,
			}
		}

		if isRateLimited {
			detail.Status = "rate-limited"
		} else {
			detail.Status = "ok"
		}

		detailedAccounts = append(detailedAccounts, detail)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
		"latencyMs": time.Since(start).Milliseconds(),
		"summary":   status.Summary,
		"counts": gin.H{
			"total":       status.Total,
			"available":   status.Available,
			"rateLimited": status.RateLimited,
			"invalid":     status.Invalid,
		},
		"accounts": detailedAccounts,
	})
}

// Package handlers provides HTTP request handlers for the server.
// This file handles account limits endpoints.
package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/cloudcode"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// AccountsHandler handles account-related endpoints
type AccountsHandler struct {
	accountManager *account.Manager
	cfg            *config.Config
}

// NewAccountsHandler creates a new AccountsHandler
func NewAccountsHandler(accountManager *account.Manager, cfg *config.Config) *AccountsHandler {
	return &AccountsHandler{
		accountManager: accountManager,
		cfg:            cfg,
	}
}

// accountLimitResult holds the result for one account
type accountLimitResult struct {
	Email        string                          `json:"email"`
	Status       string                          `json:"status"`
	Error        string                          `json:"error,omitempty"`
	Subscription *cloudcode.SubscriptionInfo     `json:"subscription,omitempty"`
	Models       map[string]*cloudcode.ModelQuota `json:"models"`
}

// AccountLimits handles GET /account-limits
func (h *AccountsHandler) AccountLimits(c *gin.Context) {
	ctx := c.Request.Context()
	allAccounts := h.accountManager.GetAllAccounts()
	format := c.Query("format")
	includeHistory := c.Query("includeHistory") == "true"

	// Fetch quotas for each account
	results := make([]*accountLimitResult, 0, len(allAccounts))

	for _, acc := range allAccounts {
		result := &accountLimitResult{
			Email:  acc.Email,
			Models: make(map[string]*cloudcode.ModelQuota),
		}

		// Skip invalid accounts
		if acc.IsInvalid {
			result.Status = "invalid"
			result.Error = acc.InvalidReason
			results = append(results, result)
			continue
		}

		// Get token
		token, err := h.accountManager.GetTokenForAccount(ctx, acc)
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			results = append(results, result)
			continue
		}

		// Fetch subscription tier
		subscription, err := cloudcode.GetSubscriptionTier(ctx, token)
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			if acc.Subscription != nil {
				result.Subscription = &cloudcode.SubscriptionInfo{
					Tier:      acc.Subscription.Tier,
					ProjectID: acc.Subscription.ProjectID,
				}
			} else {
				result.Subscription = &cloudcode.SubscriptionInfo{
					Tier: "unknown",
				}
			}
			results = append(results, result)
			continue
		}

		result.Subscription = subscription

		// Fetch quotas with project ID
		quotas, err := cloudcode.GetModelQuotas(ctx, token, subscription.ProjectID)
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			results = append(results, result)
			continue
		}

		result.Status = "ok"
		result.Models = quotas

		// Update account object with fresh data
		h.accountManager.UpdateAccountSubscription(acc.Email, subscription.Tier, subscription.ProjectID)

		// Convert quotas to interface{} map for storage
		quotaMap := make(map[string]interface{})
		for modelID, quota := range quotas {
			qm := make(map[string]interface{})
			if quota.RemainingFraction != nil {
				qm["remainingFraction"] = *quota.RemainingFraction
			}
			if quota.ResetTime != nil {
				qm["resetTime"] = *quota.ResetTime
			}
			quotaMap[modelID] = qm
		}
		h.accountManager.UpdateAccountQuota(acc.Email, quotaMap)

		results = append(results, result)
	}

	// Collect all unique model IDs
	modelIDSet := make(map[string]bool)
	for _, result := range results {
		for modelID := range result.Models {
			modelIDSet[modelID] = true
		}
	}

	sortedModels := make([]string, 0, len(modelIDSet))
	for modelID := range modelIDSet {
		sortedModels = append(sortedModels, modelID)
	}
	sort.Strings(sortedModels)

	// Return ASCII table format
	if format == "table" {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		table := h.buildAccountLimitsTable(results, sortedModels)
		c.String(http.StatusOK, table)
		return
	}

	// Get account metadata from AccountManager
	accountStatus := h.accountManager.GetStatus()

	// Build response data
	accountsData := make([]map[string]interface{}, 0, len(results))

	for _, result := range results {
		// Find metadata from status
		var metadata *account.AccountStatus
		for _, s := range accountStatus.Accounts {
			if s.Email == result.Email {
				metadata = s
				break
			}
		}

		accData := map[string]interface{}{
			"email":        result.Email,
			"status":       result.Status,
			"subscription": result.Subscription,
		}

		if result.Error != "" {
			accData["error"] = result.Error
		}

		// Include metadata from AccountManager
		if metadata != nil {
			accData["source"] = metadata.Source
			accData["enabled"] = metadata.Enabled
			accData["projectId"] = metadata.ProjectID
			accData["isInvalid"] = metadata.IsInvalid
			accData["invalidReason"] = metadata.InvalidReason
			accData["lastUsed"] = metadata.LastUsed
			accData["modelRateLimits"] = metadata.ModelRateLimits
			// Only include quotaThreshold if it's set (matches Node.js behavior)
			if metadata.QuotaThreshold != nil {
				accData["quotaThreshold"] = metadata.QuotaThreshold
			}
			if len(metadata.ModelQuotaThresholds) > 0 {
				accData["modelQuotaThresholds"] = metadata.ModelQuotaThresholds
			}
		}

		// Build limits
		limits := make(map[string]interface{})
		for _, modelID := range sortedModels {
			quota := result.Models[modelID]
			if quota == nil {
				limits[modelID] = nil
				continue
			}

			remaining := "N/A"
			var remainingFraction float64
			if quota.RemainingFraction != nil {
				remainingFraction = *quota.RemainingFraction
				remaining = utils.FormatPercent(remainingFraction)
			}

			resetTime := ""
			if quota.ResetTime != nil {
				resetTime = *quota.ResetTime
			}

			limits[modelID] = map[string]interface{}{
				"remaining":         remaining,
				"remainingFraction": remainingFraction,
				"resetTime":         resetTime,
			}
		}
		accData["limits"] = limits

		accountsData = append(accountsData, accData)
	}

	responseData := gin.H{
		"timestamp":            time.Now().Format(time.RFC3339),
		"totalAccounts":        len(allAccounts),
		"models":               sortedModels,
		"modelConfig":          h.cfg.ModelMapping,
		"globalQuotaThreshold": h.cfg.GlobalQuotaThreshold,
		"accounts":             accountsData,
	}

	// Optionally include usage history
	if includeHistory {
		// TODO: Add usage stats module integration
		responseData["history"] = []interface{}{}
	}

	c.JSON(http.StatusOK, responseData)
}

// buildAccountLimitsTable builds an ASCII table of account limits
func (h *AccountsHandler) buildAccountLimitsTable(results []*accountLimitResult, sortedModels []string) string {
	var sb strings.Builder

	timestamp := time.Now().Format(time.RFC1123)
	sb.WriteString(fmt.Sprintf("Account Limits (%s)\n", timestamp))

	// Get account status info
	status := h.accountManager.GetStatus()
	sb.WriteString(fmt.Sprintf("Accounts: %d total, %d available, %d rate-limited, %d invalid\n\n",
		status.Total, status.Available, status.RateLimited, status.Invalid))

	// Table 1: Account status
	accColWidth := 25
	statusColWidth := 15
	lastUsedColWidth := 25
	resetColWidth := 25

	sb.WriteString(fmt.Sprintf("%-*s%-*s%-*s%s\n",
		accColWidth, "Account",
		statusColWidth, "Status",
		lastUsedColWidth, "Last Used",
		"Quota Reset"))
	sb.WriteString(strings.Repeat("─", accColWidth+statusColWidth+lastUsedColWidth+resetColWidth) + "\n")

	for _, acc := range status.Accounts {
		shortEmail := acc.Email
		if idx := strings.Index(shortEmail, "@"); idx > 0 {
			shortEmail = shortEmail[:idx]
		}
		if len(shortEmail) > 22 {
			shortEmail = shortEmail[:22]
		}

		lastUsed := "never"
		if acc.LastUsed > 0 {
			lastUsed = time.UnixMilli(acc.LastUsed).Format(time.RFC1123)
		}

		// Get status and error from results
		var accResult *accountLimitResult
		for _, r := range results {
			if r.Email == acc.Email {
				accResult = r
				break
			}
		}

		var accStatus string
		if acc.IsInvalid {
			accStatus = "invalid"
		} else if accResult != nil && accResult.Status == "error" {
			accStatus = "error"
		} else if accResult != nil {
			// Count exhausted models
			models := accResult.Models
			modelCount := len(models)
			exhaustedCount := 0
			for _, q := range models {
				if q.RemainingFraction == nil || *q.RemainingFraction == 0 || *q.RemainingFraction < 0 {
					exhaustedCount++
				}
			}

			if exhaustedCount == 0 {
				accStatus = "ok"
			} else {
				accStatus = fmt.Sprintf("(%d/%d) limited", exhaustedCount, modelCount)
			}
		} else {
			accStatus = "unknown"
		}

		// Get reset time from quota API
		resetTime := "-"
		for _, modelID := range sortedModels {
			if strings.Contains(modelID, "claude") && accResult != nil {
				if quota := accResult.Models[modelID]; quota != nil && quota.ResetTime != nil && *quota.ResetTime != "" {
					resetTime = *quota.ResetTime
					break
				}
			}
		}

		sb.WriteString(fmt.Sprintf("%-*s%-*s%-*s%s\n",
			accColWidth, shortEmail,
			statusColWidth, accStatus,
			lastUsedColWidth, lastUsed,
			resetTime))

		// Add error on next line if present
		if accResult != nil && accResult.Error != "" {
			sb.WriteString(fmt.Sprintf("  └─ %s\n", accResult.Error))
		}
	}
	sb.WriteString("\n")

	// Table 2: Model quotas
	modelColWidth := 28
	for _, m := range sortedModels {
		if len(m)+2 > modelColWidth {
			modelColWidth = len(m) + 2
		}
	}
	accountColWidth := 30

	// Header row
	sb.WriteString(fmt.Sprintf("%-*s", modelColWidth, "Model"))
	for _, acc := range results {
		shortEmail := acc.Email
		if idx := strings.Index(shortEmail, "@"); idx > 0 {
			shortEmail = shortEmail[:idx]
		}
		if len(shortEmail) > 26 {
			shortEmail = shortEmail[:26]
		}
		sb.WriteString(fmt.Sprintf("%-*s", accountColWidth, shortEmail))
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("─", modelColWidth+len(results)*accountColWidth) + "\n")

	// Data rows
	for _, modelID := range sortedModels {
		sb.WriteString(fmt.Sprintf("%-*s", modelColWidth, modelID))
		for _, acc := range results {
			var cell string
			if acc.Status != "ok" && acc.Status != "rate-limited" {
				cell = fmt.Sprintf("[%s]", acc.Status)
			} else if quota := acc.Models[modelID]; quota == nil {
				cell = "-"
			} else if quota.RemainingFraction == nil || *quota.RemainingFraction == 0 || *quota.RemainingFraction < 0 {
				// Show reset time for exhausted models
				if quota.ResetTime != nil && *quota.ResetTime != "" {
					resetMs := parseResetTimeMs(*quota.ResetTime)
					if resetMs > 0 {
						cell = fmt.Sprintf("0%% (wait %s)", utils.FormatDuration(resetMs))
					} else {
						cell = "0% (resetting...)"
					}
				} else {
					cell = "0% (exhausted)"
				}
			} else {
				pct := int(*quota.RemainingFraction * 100)
				cell = fmt.Sprintf("%d%%", pct)
			}
			sb.WriteString(fmt.Sprintf("%-*s", accountColWidth, cell))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// parseResetTimeMs parses a reset time string and returns milliseconds until reset
func parseResetTimeMs(resetTime string) int64 {
	t, err := time.Parse(time.RFC3339, resetTime)
	if err != nil {
		return 0
	}
	return t.UnixMilli() - time.Now().UnixMilli()
}

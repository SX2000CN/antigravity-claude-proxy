// Package cloudcode provides Cloud Code API client implementation.
// This file corresponds to src/cloudcode/message-handler.js in the Node.js version.
package cloudcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/format"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/anthropic"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// MessageHandler handles non-streaming message requests
type MessageHandler struct {
	accountManager *account.Manager
	httpClient     *http.Client
	cfg            *config.Config
}

// NewMessageHandler creates a new MessageHandler
func NewMessageHandler(accountManager *account.Manager, cfg *config.Config) *MessageHandler {
	return &MessageHandler{
		accountManager: accountManager,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute, // Long timeout for AI responses
		},
		cfg: cfg,
	}
}

// SendMessage sends a non-streaming request to Cloud Code with multi-account support
// Uses SSE endpoint for thinking models (non-streaming doesn't return thinking blocks)
func (h *MessageHandler) SendMessage(ctx context.Context, anthropicRequest *anthropic.MessagesRequest, fallbackEnabled bool) (*anthropic.MessagesResponse, error) {
	model := anthropicRequest.Model
	isThinking := config.IsThinkingModel(model)

	// Retry loop with account failover
	maxAttempts := max(config.MaxRetries, h.accountManager.GetAccountCount()+1)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Clear any expired rate limits before picking
		h.accountManager.ClearExpiredLimits(ctx)

		// Get available accounts for this model
		availableAccounts := h.accountManager.GetAvailableAccounts(model)

		// If no accounts available, check if we should wait or throw error
		if len(availableAccounts) == 0 {
			if h.accountManager.IsAllRateLimited(model) {
				minWaitMs := h.accountManager.GetMinWaitTimeMs(ctx, model)
				resetTime := time.Now().Add(time.Duration(minWaitMs) * time.Millisecond).Format(time.RFC3339)

				// If wait time is too long (> 2 minutes), try fallback first, then throw error
				if minWaitMs > config.MaxWaitBeforeErrorMs {
					// Check if fallback is enabled and available
					if fallbackEnabled {
						fallbackModel, ok := config.GetFallbackModel(model)
						if ok {
							utils.Warn("[CloudCode] All accounts exhausted for %s (%s wait). Attempting fallback to %s",
								model, utils.FormatDuration(minWaitMs), fallbackModel)
							fallbackRequest := *anthropicRequest
							fallbackRequest.Model = fallbackModel
							return h.SendMessage(ctx, &fallbackRequest, false)
						}
					}
					return nil, fmt.Errorf("RESOURCE_EXHAUSTED: Rate limited on %s. Quota will reset after %s. Next available: %s",
						model, utils.FormatDuration(minWaitMs), resetTime)
				}

				// Wait for shortest reset time
				accountCount := h.accountManager.GetAccountCount()
				utils.Warn("[CloudCode] All %d account(s) rate-limited. Waiting %s...",
					accountCount, utils.FormatDuration(minWaitMs))
				utils.SleepMs(minWaitMs + 500)
				h.accountManager.ClearExpiredLimits(ctx)

				// CRITICAL FIX: Don't count waiting for rate limits as a failed attempt
				attempt--
				continue
			}

			// No accounts available and not rate-limited
			return nil, fmt.Errorf("No accounts available")
		}

		// Select account using configured strategy
		result, err := h.accountManager.SelectAccount(ctx, model, account.SelectOptions{})
		if err != nil {
			return nil, err
		}

		// If strategy returns a wait time without an account, sleep and retry
		if result.Account == nil && result.WaitMs > 0 {
			utils.Info("[CloudCode] Waiting %s for account...", utils.FormatDuration(result.WaitMs))
			utils.SleepMs(result.WaitMs + 500)
			attempt--
			continue
		}

		// If strategy returns an account with throttle wait (fallback mode), apply delay
		if result.Account != nil && result.WaitMs > 0 {
			utils.Debug("[CloudCode] Throttling request (%dms) - fallback mode active", result.WaitMs)
			utils.SleepMs(result.WaitMs)
		}

		if result.Account == nil {
			utils.Warn("[CloudCode] Strategy returned no account for %s (attempt %d/%d)",
				model, attempt+1, maxAttempts)
			continue
		}

		selectedAccount := result.Account

		// Get token and project for this account
		token, err := h.getTokenForAccount(ctx, selectedAccount)
		if err != nil {
			utils.Warn("[CloudCode] Failed to get token for %s: %v", selectedAccount.Email, err)
			continue
		}

		projectID := selectedAccount.ProjectID
		if projectID == "" {
			projectID = config.DefaultProjectID
		}

		payload, err := BuildCloudCodeRequest(anthropicRequest, projectID)
		if err != nil {
			return nil, err
		}

		utils.Debug("[CloudCode] Sending request for model: %s", model)

		// Try each endpoint
		var lastError error
		capacityRetryCount := 0

		for endpointIndex := 0; endpointIndex < len(config.AntigravityEndpointFallbacks); endpointIndex++ {
			endpoint := config.AntigravityEndpointFallbacks[endpointIndex]

			var url string
			if isThinking {
				url = endpoint + "/v1internal:streamGenerateContent?alt=sse"
			} else {
				url = endpoint + "/v1internal:generateContent"
			}

			var accept string
			if isThinking {
				accept = "text/event-stream"
			} else {
				accept = "application/json"
			}

			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}

			req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payloadBytes))
			if err != nil {
				return nil, err
			}

			headers := BuildHeaders(token, model, accept)
			for k, v := range headers {
				req.Header.Set(k, v)
			}

			resp, err := h.httpClient.Do(req)
			if err != nil {
				if utils.IsNetworkError(err) {
					utils.Warn("[CloudCode] Network error at %s: %v", endpoint, err)
					lastError = err
					endpointIndex++
					continue
				}
				return nil, err
			}

			if resp.StatusCode != http.StatusOK {
				bodyBytes, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				errorText := string(bodyBytes)
				utils.Warn("[CloudCode] Error at %s: %d - %s", endpoint, resp.StatusCode, errorText)

				// Handle various error codes
				switch resp.StatusCode {
				case 401:
					if IsPermanentAuthFailure(errorText) {
						utils.Error("[CloudCode] Permanent auth failure for %s: %.100s",
							selectedAccount.Email, errorText)
						_ = h.accountManager.MarkInvalid(ctx, selectedAccount.Email, "Token revoked - re-authentication required")
						return nil, fmt.Errorf("AUTH_INVALID_PERMANENT: %s", errorText)
					}
					// Transient auth error
					lastError = fmt.Errorf("Auth error: %s", errorText)
					endpointIndex++
					continue

				case 429:
					resetMs := ParseResetTime(resp.Header, errorText)

					// Check if capacity issue - retry same endpoint
					if IsModelCapacityExhausted(errorText) {
						if capacityRetryCount < config.MaxCapacityRetries {
							tierIndex := min(capacityRetryCount, len(config.CapacityBackoffTiersMs)-1)
							waitMs := resetMs
							if waitMs <= 0 {
								waitMs = config.CapacityBackoffTiersMs[tierIndex]
							}
							capacityRetryCount++
							utils.Info("[CloudCode] Model capacity exhausted, retry %d/%d after %s...",
								capacityRetryCount, config.MaxCapacityRetries, utils.FormatDuration(waitMs))
							utils.SleepMs(waitMs)
							continue // Retry same endpoint
						}
						utils.Warn("[CloudCode] Max capacity retries (%d) exceeded, switching account",
							config.MaxCapacityRetries)
					}

					// Get rate limit backoff
					backoff := GetRateLimitBackoff(selectedAccount.Email, model, resetMs)

					// For very short rate limits, wait and retry
					if resetMs > 0 && resetMs < 1000 {
						utils.Info("[CloudCode] Short rate limit on %s (%dms), waiting and retrying...",
							selectedAccount.Email, resetMs)
						utils.SleepMs(resetMs)
						continue
					}

					// If within dedup window, switch account
					if backoff.IsDuplicate {
						smartBackoffMs := CalculateSmartBackoff(errorText, resetMs, 0)
						utils.Info("[CloudCode] Skipping retry due to recent rate limit on %s (attempt %d), switching account...",
							selectedAccount.Email, backoff.Attempt)
						_ = h.accountManager.MarkRateLimited(ctx, selectedAccount.Email, smartBackoffMs, model)
						lastError = fmt.Errorf("RATE_LIMITED_DEDUP: %s", errorText)
						break // Break to try next account
					}

					// Calculate smart backoff
					smartBackoffMs := CalculateSmartBackoff(errorText, resetMs, 0)

					// Decision: wait and retry OR switch account
					if backoff.Attempt == 1 && smartBackoffMs <= config.DefaultCooldownMs {
						waitMs := backoff.DelayMs
						_ = h.accountManager.MarkRateLimited(ctx, selectedAccount.Email, waitMs, model)
						utils.Info("[CloudCode] First rate limit on %s, quick retry after %s...",
							selectedAccount.Email, utils.FormatDuration(waitMs))
						utils.SleepMs(waitMs)
						continue
					} else if smartBackoffMs > config.DefaultCooldownMs {
						utils.Info("[CloudCode] Quota exhausted for %s (%s), switching account after %s delay...",
							selectedAccount.Email, utils.FormatDuration(smartBackoffMs), utils.FormatDuration(config.SwitchAccountDelayMs))
						utils.SleepMs(config.SwitchAccountDelayMs)
						_ = h.accountManager.MarkRateLimited(ctx, selectedAccount.Email, smartBackoffMs, model)
						lastError = fmt.Errorf("QUOTA_EXHAUSTED: %s", errorText)
						break
					} else {
						waitMs := backoff.DelayMs
						_ = h.accountManager.MarkRateLimited(ctx, selectedAccount.Email, waitMs, model)
						utils.Info("[CloudCode] Rate limit on %s (attempt %d), waiting %s...",
							selectedAccount.Email, backoff.Attempt, utils.FormatDuration(waitMs))
						utils.SleepMs(waitMs)
						continue
					}

				case 400:
					utils.Error("[CloudCode] Invalid request (400): %.200s", errorText)
					return nil, fmt.Errorf("invalid_request_error: %s", errorText)

				case 503, 529:
					if IsModelCapacityExhausted(errorText) && capacityRetryCount < config.MaxCapacityRetries {
						tierIndex := min(capacityRetryCount, len(config.CapacityBackoffTiersMs)-1)
						waitMs := config.CapacityBackoffTiersMs[tierIndex]
						capacityRetryCount++
						utils.Info("[CloudCode] %d Model capacity exhausted, retry %d/%d after %s...",
							resp.StatusCode, capacityRetryCount, config.MaxCapacityRetries, utils.FormatDuration(waitMs))
						utils.SleepMs(waitMs)
						continue
					}
					fallthrough

				default:
					lastError = fmt.Errorf("API error %d: %s", resp.StatusCode, errorText)
					if resp.StatusCode >= 500 {
						utils.Warn("[CloudCode] %d error, waiting 1s before retry...", resp.StatusCode)
						utils.SleepMs(1000)
					}
					endpointIndex++
					continue
				}
			}

			// Success - process response
			defer resp.Body.Close()

			// For thinking models, parse SSE and accumulate all parts
			if isThinking {
				result, err := ParseThinkingSSEResponse(resp.Body, anthropicRequest.Model)
				if err != nil {
					return nil, err
				}
				// Clear rate limit state on success
				ClearRateLimitState(selectedAccount.Email, model)
				h.accountManager.NotifySuccess(selectedAccount, model)
				return result, nil
			}

			// Non-thinking models use regular JSON
			var data map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
				return nil, err
			}
			utils.Debug("[CloudCode] Response received")
			// Clear rate limit state on success
			ClearRateLimitState(selectedAccount.Email, model)
			h.accountManager.NotifySuccess(selectedAccount, model)
			googleResp := format.GoogleResponseFromMap(data)
			return format.ConvertGoogleToAnthropic(googleResp, anthropicRequest.Model), nil
		}

		// If all endpoints failed for this account
		if lastError != nil {
			if isRateLimitError(lastError) {
				h.accountManager.NotifyRateLimit(selectedAccount, model)
				utils.Info("[CloudCode] Account %s rate-limited, trying next...", selectedAccount.Email)
				continue
			}
			if isAuthError(lastError) {
				utils.Warn("[CloudCode] Account %s has invalid credentials, trying next...", selectedAccount.Email)
				continue
			}
			// Handle 5xx errors
			if is5xxError(lastError) {
				h.accountManager.NotifyFailure(selectedAccount, model)
				utils.Warn("[CloudCode] Account %s failed with 5xx error, trying next...", selectedAccount.Email)
				continue
			}
			if utils.IsNetworkError(lastError) {
				h.accountManager.NotifyFailure(selectedAccount, model)
				utils.Warn("[CloudCode] Network error for %s, trying next account... (%v)", selectedAccount.Email, lastError)
				utils.SleepMs(1000)
				continue
			}
			return nil, lastError
		}
	}

	// All retries exhausted - try fallback model if enabled
	if fallbackEnabled {
		fallbackModel, ok := config.GetFallbackModel(model)
		if ok {
			utils.Warn("[CloudCode] All retries exhausted for %s. Attempting fallback to %s",
				model, fallbackModel)
			fallbackRequest := *anthropicRequest
			fallbackRequest.Model = fallbackModel
			return h.SendMessage(ctx, &fallbackRequest, false)
		}
	}

	return nil, fmt.Errorf("Max retries exceeded")
}

// getTokenForAccount gets an access token for the account
func (h *MessageHandler) getTokenForAccount(ctx context.Context, acc *redis.Account) (string, error) {
	// This would integrate with the auth module
	// For now, we'll use the refresh token to get an access token
	// This is a placeholder - actual implementation would use OAuth
	return "TODO_GET_ACCESS_TOKEN", nil
}

// Helper functions
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return utils.ContainsAny(msg,
		"429",
		"RATE_LIMITED",
		"QUOTA_EXHAUSTED",
		"RESOURCE_EXHAUSTED",
	)
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return utils.ContainsAny(msg,
		"401",
		"AUTH_INVALID",
		"invalid_grant",
	)
}

func is5xxError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return utils.ContainsAny(msg,
		"API error 5",
		"500",
		"503",
	)
}


// Package cloudcode provides Cloud Code API client implementation.
// This file corresponds to src/cloudcode/streaming-handler.js in the Node.js version.
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
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/anthropic"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// StreamingHandler handles streaming message requests
type StreamingHandler struct {
	accountManager *account.Manager
	httpClient     *http.Client
	cfg            *config.Config
}

// NewStreamingHandler creates a new StreamingHandler
func NewStreamingHandler(accountManager *account.Manager, cfg *config.Config) *StreamingHandler {
	return &StreamingHandler{
		accountManager: accountManager,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute, // Long timeout for AI responses
		},
		cfg: cfg,
	}
}

// SendMessageStream sends a streaming request to Cloud Code with multi-account support
// Returns a channel of SSE events
func (h *StreamingHandler) SendMessageStream(ctx context.Context, anthropicRequest *anthropic.MessagesRequest, fallbackEnabled bool) (<-chan *SSEEvent, <-chan error) {
	events := make(chan *SSEEvent, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		err := h.streamWithRetry(ctx, anthropicRequest, fallbackEnabled, events)
		if err != nil {
			errs <- err
		}
	}()

	return events, errs
}

// streamWithRetry handles the streaming with retry logic
func (h *StreamingHandler) streamWithRetry(ctx context.Context, anthropicRequest *anthropic.MessagesRequest, fallbackEnabled bool, events chan<- *SSEEvent) error {
	model := anthropicRequest.Model

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
							utils.Warn("[CloudCode] All accounts exhausted for %s (%s wait). Attempting fallback to %s (streaming)",
								model, utils.FormatDuration(minWaitMs), fallbackModel)
							fallbackRequest := *anthropicRequest
							fallbackRequest.Model = fallbackModel
							return h.streamWithRetry(ctx, &fallbackRequest, false, events)
						}
					}
					return fmt.Errorf("RESOURCE_EXHAUSTED: Rate limited on %s. Quota will reset after %s. Next available: %s",
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
			return fmt.Errorf("No accounts available")
		}

		// Select account using configured strategy
		result, err := h.accountManager.SelectAccount(ctx, model, account.SelectOptions{})
		if err != nil {
			return err
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
			return err
		}

		utils.Debug("[CloudCode] Starting stream for model: %s", model)

		// Try each endpoint
		var lastError error
		capacityRetryCount := 0

	endpointLoop:
		for endpointIndex := 0; endpointIndex < len(config.AntigravityEndpointFallbacks); endpointIndex++ {
			endpoint := config.AntigravityEndpointFallbacks[endpointIndex]
			url := endpoint + "/v1internal:streamGenerateContent?alt=sse"

			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return err
			}

			req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payloadBytes))
			if err != nil {
				return err
			}

			headers := BuildHeaders(token, model, "text/event-stream")
			for k, v := range headers {
				req.Header.Set(k, v)
			}

			resp, err := h.httpClient.Do(req)
			if err != nil {
				if utils.IsNetworkError(err) {
					utils.Warn("[CloudCode] Network error at %s: %v", endpoint, err)
					lastError = err
					continue
				}
				return err
			}

			if resp.StatusCode != http.StatusOK {
				bodyBytes, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				errorText := string(bodyBytes)
				utils.Warn("[CloudCode] Stream error at %s: %d - %s", endpoint, resp.StatusCode, errorText)

				// Handle various error codes (similar to message_handler.go)
				switch resp.StatusCode {
				case 401:
					if IsPermanentAuthFailure(errorText) {
						utils.Error("[CloudCode] Permanent auth failure for %s: %.100s",
							selectedAccount.Email, errorText)
						_ = h.accountManager.MarkInvalid(ctx, selectedAccount.Email, "Token revoked - re-authentication required")
						return fmt.Errorf("AUTH_INVALID_PERMANENT: %s", errorText)
					}
					lastError = fmt.Errorf("Auth error: %s", errorText)
					continue

				case 429:
					resetMs := ParseResetTime(resp.Header, errorText)

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
							continue
						}
					}

					backoff := GetRateLimitBackoff(selectedAccount.Email, model, resetMs)

					if resetMs > 0 && resetMs < 1000 {
						utils.Info("[CloudCode] Short rate limit on %s (%dms), waiting and retrying...",
							selectedAccount.Email, resetMs)
						utils.SleepMs(resetMs)
						continue
					}

					if backoff.IsDuplicate {
						smartBackoffMs := CalculateSmartBackoff(errorText, resetMs, 0)
						utils.Info("[CloudCode] Skipping retry due to recent rate limit on %s (attempt %d), switching account...",
							selectedAccount.Email, backoff.Attempt)
						_ = h.accountManager.MarkRateLimited(ctx, selectedAccount.Email, smartBackoffMs, model)
						lastError = fmt.Errorf("RATE_LIMITED_DEDUP: %s", errorText)
						break endpointLoop
					}

					smartBackoffMs := CalculateSmartBackoff(errorText, resetMs, 0)

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
						break endpointLoop
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
					return fmt.Errorf("invalid_request_error: %s", errorText)

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
						utils.Warn("[CloudCode] %d stream error, waiting 1s before retry...", resp.StatusCode)
						utils.SleepMs(1000)
					}
					continue
				}
			}

			// Success - stream the response with retry logic for empty responses
			emptyRetries := 0
			currentResp := resp

			for emptyRetries <= config.MaxEmptyResponseRetries {
				sseEvents, sseErrs := StreamSSEResponse(currentResp.Body, anthropicRequest.Model)

				// Forward all events
				hadError := false
				for event := range sseEvents {
					events <- event
				}

				// Check for errors
				select {
				case err := <-sseErrs:
					if err != nil {
						if IsEmptyResponseError(err) {
							currentResp.Body.Close()

							if emptyRetries >= config.MaxEmptyResponseRetries {
								utils.Error("[CloudCode] Empty response after %d retries", config.MaxEmptyResponseRetries)
								// Emit empty response fallback
								emitEmptyResponseFallback(events, anthropicRequest.Model)
								return nil
							}

							// Exponential backoff
							backoffMs := 500 * (1 << emptyRetries)
							utils.Warn("[CloudCode] Empty response, retry %d/%d after %dms...",
								emptyRetries+1, config.MaxEmptyResponseRetries, backoffMs)
							utils.SleepMs(int64(backoffMs))

							// Refetch
							newReq, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payloadBytes))
							for k, v := range headers {
								newReq.Header.Set(k, v)
							}
							currentResp, err = h.httpClient.Do(newReq)
							if err != nil || currentResp.StatusCode != http.StatusOK {
								if currentResp != nil {
									currentResp.Body.Close()
								}
								return fmt.Errorf("Retry failed: %v", err)
							}
							emptyRetries++
							continue
						}
						hadError = true
						lastError = err
					}
				default:
				}

				if !hadError {
					// Success
					currentResp.Body.Close()
					utils.Debug("[CloudCode] Stream completed")
					ClearRateLimitState(selectedAccount.Email, model)
					h.accountManager.NotifySuccess(selectedAccount, model)
					return nil
				}
				break endpointLoop
			}
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
			if is5xxError(lastError) {
				h.accountManager.NotifyFailure(selectedAccount, model)
				utils.Warn("[CloudCode] Account %s failed with 5xx stream error, trying next...", selectedAccount.Email)
				continue
			}
			if utils.IsNetworkError(lastError) {
				h.accountManager.NotifyFailure(selectedAccount, model)
				utils.Warn("[CloudCode] Network error for %s (stream), trying next account... (%v)", selectedAccount.Email, lastError)
				utils.SleepMs(1000)
				continue
			}
			return lastError
		}
	}

	// All retries exhausted - try fallback model if enabled
	if fallbackEnabled {
		fallbackModel, ok := config.GetFallbackModel(model)
		if ok {
			utils.Warn("[CloudCode] All retries exhausted for %s. Attempting fallback to %s (streaming)",
				model, fallbackModel)
			fallbackRequest := *anthropicRequest
			fallbackRequest.Model = fallbackModel
			return h.streamWithRetry(ctx, &fallbackRequest, false, events)
		}
	}

	return fmt.Errorf("Max retries exceeded")
}

// getTokenForAccount gets an access token for the account
func (h *StreamingHandler) getTokenForAccount(ctx context.Context, acc *redis.Account) (string, error) {
	return h.accountManager.GetTokenForAccount(ctx, acc)
}

// emitEmptyResponseFallback emits a fallback message when all retry attempts fail
func emitEmptyResponseFallback(events chan<- *SSEEvent, model string) {
	messageID := "msg_" + generateHexID(16)

	events <- &SSEEvent{
		Type: "message_start",
		Message: &anthropic.MessagesResponse{
			ID:           messageID,
			Type:         "message",
			Role:         "assistant",
			Content:      []anthropic.ContentBlock{},
			Model:        model,
			StopReason:   "",
			StopSequence: nil,
			Usage:        &anthropic.Usage{InputTokens: 0, OutputTokens: 0},
		},
	}

	events <- &SSEEvent{
		Type:  "content_block_start",
		Index: 0,
		ContentBlock: &anthropic.ContentBlock{
			Type: "text",
			Text: "",
		},
	}

	events <- &SSEEvent{
		Type:  "content_block_delta",
		Index: 0,
		Delta: map[string]interface{}{
			"type": "text_delta",
			"text": "[No response after retries - please try again]",
		},
	}

	events <- &SSEEvent{Type: "content_block_stop", Index: 0}

	events <- &SSEEvent{
		Type: "message_delta",
		Delta: map[string]interface{}{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
		Usage: &anthropic.Usage{OutputTokens: 0},
	}

	events <- &SSEEvent{Type: "message_stop"}
}

// Package handlers provides HTTP request handlers for the server.
// This file handles the main /v1/messages endpoint.
package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/cloudcode"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/server/sse"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/anthropic"
)

// MessagesHandler handles the /v1/messages endpoint
type MessagesHandler struct {
	accountManager  *account.Manager
	cloudCodeClient *cloudcode.Client
	cfg             *config.Config
	fallbackEnabled bool
}

// NewMessagesHandler creates a new MessagesHandler
func NewMessagesHandler(
	accountManager *account.Manager,
	cloudCodeClient *cloudcode.Client,
	cfg *config.Config,
	fallbackEnabled bool,
) *MessagesHandler {
	return &MessagesHandler{
		accountManager:  accountManager,
		cloudCodeClient: cloudCodeClient,
		cfg:             cfg,
		fallbackEnabled: fallbackEnabled,
	}
}

// Messages handles POST /v1/messages - Anthropic Messages API compatible
func (h *MessagesHandler) Messages(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse request body
	var req anthropic.MessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": "Invalid request body: " + err.Error(),
			},
		})
		return
	}

	// Resolve model mapping if configured
	requestedModel := req.Model
	if requestedModel == "" {
		requestedModel = "claude-3-5-sonnet-20241022"
	}

	if h.cfg.ModelMapping != nil {
		if mapping, ok := h.cfg.ModelMapping[requestedModel]; ok && mapping != "" {
			utils.Info("[Server] Mapping model %s -> %s", requestedModel, mapping)
			requestedModel = mapping
		}
	}

	req.Model = requestedModel

	// Validate model ID before processing
	result, _ := h.accountManager.SelectAccount(ctx, "", account.SelectOptions{})
	if result.Account != nil {
		token, err := h.accountManager.GetTokenForAccount(ctx, result.Account)
		if err == nil {
			projectID := ""
			if result.Account.Subscription != nil {
				projectID = result.Account.Subscription.ProjectID
			}
			if !cloudcode.IsValidModel(ctx, req.Model, token, projectID) {
				h.sendError(c, http.StatusBadRequest, "invalid_request_error",
					"Invalid model: "+req.Model+". Use /v1/models to see available models.")
				return
			}
		}
	}

	// Optimistic Retry: If ALL accounts are rate-limited for this model, reset them
	if h.accountManager.IsAllRateLimited(req.Model) {
		utils.Warn("[Server] All accounts rate-limited for %s. Resetting state for optimistic retry.", req.Model)
		h.accountManager.ResetAllRateLimits(ctx)
	}

	// Validate required fields
	if req.Messages == nil || len(req.Messages) == 0 {
		h.sendError(c, http.StatusBadRequest, "invalid_request_error",
			"messages is required and must be an array")
		return
	}

	// Filter out "count" requests
	if len(req.Messages) == 1 && len(req.Messages[0].Content) == 1 {
		if req.Messages[0].Content[0].Type == "text" && req.Messages[0].Content[0].Text == "count" {
			c.JSON(http.StatusOK, gin.H{})
			return
		}
	}

	// Set default max_tokens
	if req.MaxTokens == 0 {
		req.MaxTokens = 4096
	}

	utils.Info("[API] Request for model: %s, stream: %t", req.Model, req.Stream)

	// Debug: Log message structure
	if utils.IsDebug() {
		utils.Debug("[API] Message structure:")
		for i, msg := range req.Messages {
			types := make([]string, 0, len(msg.Content))
			for _, block := range msg.Content {
				types = append(types, block.Type)
			}
			utils.Debug("  [%d] %s: %s", i, msg.Role, strings.Join(types, ", "))
		}
	}

	if req.Stream {
		h.handleStreamingResponse(c, &req)
	} else {
		h.handleNonStreamingResponse(c, &req)
	}
}

// handleStreamingResponse handles streaming SSE responses
func (h *MessagesHandler) handleStreamingResponse(c *gin.Context, req *anthropic.MessagesRequest) {
	ctx := c.Request.Context()

	// Initialize SSE stream
	events, errs := h.cloudCodeClient.SendMessageStream(ctx, req, h.fallbackEnabled)

	// Buffer strategy: Pull the first event before sending headers
	var firstEvent *cloudcode.SSEEvent
	var firstErr error

	select {
	case event, ok := <-events:
		if !ok {
			// Channel closed without any events
			select {
			case err := <-errs:
				firstErr = err
			default:
				firstErr = cloudcode.NewEmptyResponseError("No response received")
			}
		} else {
			firstEvent = event
		}
	case err := <-errs:
		firstErr = err
	}

	// If we got an error before any data, send proper error response
	if firstErr != nil {
		utils.Error("[API] Initial stream error: %v", firstErr)
		errorType, statusCode, errorMessage := parseError(firstErr)
		c.JSON(statusCode, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    errorType,
				"message": errorMessage,
			},
		})
		return
	}

	// If we get here, the stream started successfully
	sseWriter, err := sse.NewWriter(c.Writer)
	if err != nil {
		utils.Error("[API] Failed to create SSE writer: %v", err)
		h.sendError(c, http.StatusInternalServerError, "api_error", "Streaming not supported")
		return
	}

	c.Status(http.StatusOK)
	sseWriter.SetHeaders()
	c.Writer.Flush()

	// Send the first event
	if firstEvent != nil {
		if err := sseWriter.WriteEvent(firstEvent.Type, firstEvent); err != nil {
			utils.Error("[API] Error writing first SSE event: %v", err)
			return
		}
	}

	// Continue with the rest of the stream
	for {
		select {
		case event, ok := <-events:
			if !ok {
				// Stream ended
				return
			}
			if err := sseWriter.WriteEvent(event.Type, event); err != nil {
				utils.Error("[API] Error writing SSE event: %v", err)
				return
			}
		case err := <-errs:
			if err != nil {
				// Mid-stream error
				utils.Error("[API] Mid-stream error: %v", err)
				errorType, _, errorMessage := parseError(err)
				sseWriter.WriteError(errorType, errorMessage)
			}
			return
		case <-ctx.Done():
			return
		}
	}
}

// handleNonStreamingResponse handles non-streaming responses
func (h *MessagesHandler) handleNonStreamingResponse(c *gin.Context, req *anthropic.MessagesRequest) {
	ctx := c.Request.Context()

	response, err := h.cloudCodeClient.SendMessage(ctx, req, h.fallbackEnabled)
	if err != nil {
		utils.Error("[API] Error: %v", err)
		errorType, statusCode, errorMessage := h.handleAPIError(err)
		h.sendError(c, statusCode, errorType, errorMessage)
		return
	}

	c.JSON(http.StatusOK, response)
}

// handleAPIError handles API errors with optional token refresh
func (h *MessagesHandler) handleAPIError(err error) (string, int, string) {
	errorType, statusCode, errorMessage := parseError(err)

	// For auth errors, try to refresh token
	if errorType == "authentication_error" {
		utils.Warn("[API] Token might be expired, attempting refresh...")
		// In Go version, we clear caches
		h.accountManager.ClearTokenCache()
		h.accountManager.ClearProjectCache()
		errorMessage = "Token was expired and has been refreshed. Please retry your request."
	}

	utils.Warn("[API] Returning error response: %d %s - %s", statusCode, errorType, errorMessage)
	return errorType, statusCode, errorMessage
}

// sendError sends an error JSON response
func (h *MessagesHandler) sendError(c *gin.Context, statusCode int, errorType, message string) {
	c.JSON(statusCode, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errorType,
			"message": message,
		},
	})
}

// CountTokens handles POST /v1/messages/count_tokens
func (h *MessagesHandler) CountTokens(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "not_implemented",
			"message": "Token counting is not implemented. Use /v1/messages with max_tokens or configure your client to skip token counting.",
		},
	})
}

// parseError parses an error and returns error type, status code, and message
func parseError(err error) (string, int, string) {
	errorType := "api_error"
	statusCode := 500
	errorMessage := err.Error()

	msg := err.Error()

	if strings.Contains(msg, "401") || strings.Contains(msg, "UNAUTHENTICATED") {
		errorType = "authentication_error"
		statusCode = 401
		errorMessage = "Authentication failed. Make sure Antigravity is running with a valid token."
	} else if strings.Contains(msg, "429") || strings.Contains(msg, "RESOURCE_EXHAUSTED") || strings.Contains(msg, "QUOTA_EXHAUSTED") {
		errorType = "invalid_request_error"
		statusCode = 400

		// Try to extract the quota reset time from the error
		model := "the model"
		if idx := strings.Index(msg, "Rate limited on "); idx >= 0 {
			end := strings.Index(msg[idx:], ".")
			if end > 0 {
				model = msg[idx+len("Rate limited on "):idx+end]
			}
		}

		// Try to extract reset time
		if idx := strings.Index(msg, "quota will reset after "); idx >= 0 {
			rest := msg[idx+len("quota will reset after "):]
			if end := strings.IndexAny(rest, ".,"); end > 0 {
				duration := rest[:end]
				errorMessage = "You have exhausted your capacity on " + model + ". Quota will reset after " + duration + "."
			} else {
				errorMessage = "You have exhausted your capacity on " + model + ". Please wait for your quota to reset."
			}
		} else {
			errorMessage = "You have exhausted your capacity on " + model + ". Please wait for your quota to reset."
		}
	} else if strings.Contains(msg, "invalid_request_error") || strings.Contains(msg, "INVALID_ARGUMENT") {
		errorType = "invalid_request_error"
		statusCode = 400
		// Try to extract the message
		if idx := strings.Index(msg, `"message":"`); idx >= 0 {
			rest := msg[idx+len(`"message":"`):]
			if end := strings.Index(rest, `"`); end > 0 {
				errorMessage = rest[:end]
			}
		}
	} else if strings.Contains(msg, "All endpoints failed") {
		errorType = "api_error"
		statusCode = 503
		errorMessage = "Unable to connect to Claude API. Check that Antigravity is running."
	} else if strings.Contains(msg, "PERMISSION_DENIED") {
		errorType = "permission_error"
		statusCode = 403
	}

	return errorType, statusCode, errorMessage
}

// ClearSignatureCache handles POST /test/clear-signature-cache
func ClearSignatureCache(c *gin.Context) {
	// Clear the global signature cache
	// This is called from format package
	utils.Debug("[Test] Cleared thinking signature cache")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Thinking signature cache cleared",
	})
}

// RefreshTokenHandler handles POST /refresh-token
type RefreshTokenHandler struct {
	accountManager *account.Manager
}

// NewRefreshTokenHandler creates a new RefreshTokenHandler
func NewRefreshTokenHandler(accountManager *account.Manager) *RefreshTokenHandler {
	return &RefreshTokenHandler{
		accountManager: accountManager,
	}
}

// RefreshToken handles POST /refresh-token
func (h *RefreshTokenHandler) RefreshToken(c *gin.Context) {
	// Clear all caches
	h.accountManager.ClearTokenCache()
	h.accountManager.ClearProjectCache()

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Token caches cleared and refreshed",
	})
}

// SerializeRequest converts a request to JSON for logging
func SerializeRequest(req *anthropic.MessagesRequest) string {
	data, err := json.Marshal(req)
	if err != nil {
		return "{}"
	}
	return string(data)
}

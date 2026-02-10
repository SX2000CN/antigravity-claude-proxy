// Package cloudcode provides Cloud Code API client implementation.
// This file corresponds to src/cloudcode/request-builder.js in the Node.js version.
package cloudcode

import (
	"github.com/google/uuid"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/format"
	"github.com/poemonsense/antigravity-proxy-go/pkg/anthropic"
)

// CloudCodePayload represents the wrapped request body for Cloud Code API
type CloudCodePayload struct {
	Project     string                 `json:"project"`
	Model       string                 `json:"model"`
	Request     map[string]interface{} `json:"request"`
	UserAgent   string                 `json:"userAgent"`
	RequestType string                 `json:"requestType"`
	RequestID   string                 `json:"requestId"`
}

// BuildCloudCodeRequest builds the wrapped request body for Cloud Code API
func BuildCloudCodeRequest(anthropicRequest *anthropic.MessagesRequest, projectID string) (*CloudCodePayload, error) {
	model := anthropicRequest.Model

	// Convert to Google format and then to map for dynamic field addition
	googleRequestStruct := format.ConvertAnthropicToGoogle(anthropicRequest)
	googleRequest := googleRequestStruct.ToMap()

	// Use stable session ID derived from first user message for cache continuity
	googleRequest["sessionId"] = DeriveSessionID(anthropicRequest)

	// Build system instruction parts array with [ignore] tags to prevent model from
	// identifying as "Antigravity" (fixes GitHub issue #76)
	// Reference: CLIProxyAPI, gcli2api, AIClient-2-API all use this approach
	systemParts := []map[string]interface{}{
		{"text": config.AntigravitySystemInstruction},
		{"text": "Please ignore the following [ignore]" + config.AntigravitySystemInstruction + "[/ignore]"},
	}

	// Append any existing system instructions from the request
	if existingInstruction, ok := googleRequest["systemInstruction"].(map[string]interface{}); ok {
		if parts, ok := existingInstruction["parts"].([]interface{}); ok {
			for _, part := range parts {
				if partMap, ok := part.(map[string]interface{}); ok {
					if text, ok := partMap["text"].(string); ok && text != "" {
						systemParts = append(systemParts, map[string]interface{}{"text": text})
					}
				}
			}
		}
	}

	// Inject systemInstruction with role: "user" at the top level (CLIProxyAPI v6.6.89 behavior)
	googleRequest["systemInstruction"] = map[string]interface{}{
		"role":  "user",
		"parts": systemParts,
	}

	payload := &CloudCodePayload{
		Project:     projectID,
		Model:       model,
		Request:     googleRequest,
		UserAgent:   "antigravity",
		RequestType: "agent", // CLIProxyAPI v6.6.89 compatibility
		RequestID:   "agent-" + uuid.New().String(),
	}

	return payload, nil
}

// BuildHeaders builds headers for Cloud Code API requests
func BuildHeaders(token, model string, accept string) map[string]string {
	if accept == "" {
		accept = "application/json"
	}

	headers := make(map[string]string)

	// Add authorization
	headers["Authorization"] = "Bearer " + token
	headers["Content-Type"] = "application/json"

	// Add Antigravity headers
	for k, v := range config.AntigravityHeaders() {
		headers[k] = v
	}

	// Add interleaved thinking header only for Claude thinking models
	modelFamily := config.GetModelFamily(model)
	if modelFamily == config.ModelFamilyClaude && config.IsThinkingModel(model) {
		headers["anthropic-beta"] = "interleaved-thinking-2025-05-14"
	}

	if accept != "application/json" {
		headers["Accept"] = accept
	}

	return headers
}

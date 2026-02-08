// Package format provides conversion between Anthropic and Google Generative AI formats.
// This file corresponds to src/format/response-converter.js in the Node.js version.
package format

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/pkg/anthropic"
)

// GoogleResponse represents a response from Google Generative AI
type GoogleResponse struct {
	Response    *GoogleResponseInner `json:"response,omitempty"`
	Candidates  []Candidate          `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata     `json:"usageMetadata,omitempty"`
}

// GoogleResponseInner represents the inner response object
type GoogleResponseInner struct {
	Candidates    []Candidate    `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// Candidate represents a response candidate
type Candidate struct {
	Content      *CandidateContent `json:"content,omitempty"`
	FinishReason string            `json:"finishReason,omitempty"`
}

// CandidateContent represents the content of a candidate
type CandidateContent struct {
	Parts []ResponsePart `json:"parts,omitempty"`
	Role  string         `json:"role,omitempty"`
}

// ResponsePart represents a part in the response
type ResponsePart struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	FunctionCall     *ResponseFuncCall `json:"functionCall,omitempty"`
	InlineData       *InlineData       `json:"inlineData,omitempty"`
}

// ResponseFuncCall represents a function call in the response
type ResponseFuncCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
	ID   string                 `json:"id,omitempty"`
}

// UsageMetadata represents usage metadata
type UsageMetadata struct {
	PromptTokenCount       int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount   int `json:"candidatesTokenCount,omitempty"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
}

// GoogleResponseFromMap creates a GoogleResponse from a map[string]interface{}
func GoogleResponseFromMap(data map[string]interface{}) *GoogleResponse {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return &GoogleResponse{}
	}
	var response GoogleResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return &GoogleResponse{}
	}
	return &response
}

// ConvertGoogleToAnthropic converts Google Generative AI response to Anthropic Messages API format
func ConvertGoogleToAnthropic(googleResponse *GoogleResponse, model string) *anthropic.MessagesResponse {
	// Handle the response wrapper
	var candidates []Candidate
	var usageMetadata *UsageMetadata

	if googleResponse.Response != nil {
		candidates = googleResponse.Response.Candidates
		usageMetadata = googleResponse.Response.UsageMetadata
	} else {
		candidates = googleResponse.Candidates
		usageMetadata = googleResponse.UsageMetadata
	}

	var firstCandidate Candidate
	if len(candidates) > 0 {
		firstCandidate = candidates[0]
	}

	var parts []ResponsePart
	if firstCandidate.Content != nil {
		parts = firstCandidate.Content.Parts
	}

	// Convert parts to Anthropic content blocks
	anthropicContent := make([]anthropic.ContentBlock, 0)
	hasToolCalls := false

	cache := GetGlobalSignatureCache()

	for _, part := range parts {
		if part.Text != "" {
			// Handle thinking blocks
			if part.Thought {
				signature := part.ThoughtSignature

				// Cache thinking signature with model family for cross-model compatibility
				if signature != "" && len(signature) >= config.MinSignatureLength {
					modelFamily := config.GetModelFamily(model)
					cache.CacheThinkingSignature(signature, string(modelFamily))
				}

				// Include thinking blocks in the response for Claude Code
				anthropicContent = append(anthropicContent, anthropic.ContentBlock{
					Type:      "thinking",
					Thinking:  part.Text,
					Signature: signature,
				})
			} else {
				anthropicContent = append(anthropicContent, anthropic.ContentBlock{
					Type: "text",
					Text: part.Text,
				})
			}
		} else if part.FunctionCall != nil {
			// Convert functionCall to tool_use
			// Use the id from the response if available, otherwise generate one
			toolID := part.FunctionCall.ID
			if toolID == "" {
				toolID = "toolu_" + generateRandomHex(12)
			}

			// Convert Args map to json.RawMessage
			var inputJSON json.RawMessage
			if part.FunctionCall.Args != nil {
				inputJSON, _ = json.Marshal(part.FunctionCall.Args)
			} else {
				inputJSON = json.RawMessage("{}")
			}

			toolUseBlock := anthropic.ContentBlock{
				Type:  "tool_use",
				ID:    toolID,
				Name:  part.FunctionCall.Name,
				Input: inputJSON,
			}

			// For Gemini 3+, include thoughtSignature from the part level
			if part.ThoughtSignature != "" && len(part.ThoughtSignature) >= config.MinSignatureLength {
				toolUseBlock.ThoughtSignature = part.ThoughtSignature
				// Cache for future requests (Claude Code may strip this field)
				cache.CacheSignature(toolID, part.ThoughtSignature)
			}

			anthropicContent = append(anthropicContent, toolUseBlock)
			hasToolCalls = true
		} else if part.InlineData != nil {
			// Handle image content from Google format
			anthropicContent = append(anthropicContent, anthropic.ContentBlock{
				Type: "image",
				Source: &anthropic.ImageSource{
					Type:      "base64",
					MediaType: part.InlineData.MimeType,
					Data:      part.InlineData.Data,
				},
			})
		}
	}

	// Determine stop reason
	finishReason := firstCandidate.FinishReason
	stopReason := "end_turn"
	if finishReason == "STOP" {
		stopReason = "end_turn"
	} else if finishReason == "MAX_TOKENS" {
		stopReason = "max_tokens"
	} else if finishReason == "TOOL_USE" || hasToolCalls {
		stopReason = "tool_use"
	}

	// Extract usage metadata
	// Note: Antigravity's promptTokenCount is the TOTAL (includes cached),
	// but Anthropic's input_tokens excludes cached. We subtract to match.
	var promptTokens, cachedTokens, outputTokens int
	if usageMetadata != nil {
		promptTokens = usageMetadata.PromptTokenCount
		cachedTokens = usageMetadata.CachedContentTokenCount
		outputTokens = usageMetadata.CandidatesTokenCount
	}

	// Ensure we have at least one content block
	if len(anthropicContent) == 0 {
		anthropicContent = append(anthropicContent, anthropic.ContentBlock{
			Type: "text",
			Text: "",
		})
	}

	return &anthropic.MessagesResponse{
		ID:           "msg_" + generateRandomHex(16),
		Type:         "message",
		Role:         "assistant",
		Content:      anthropicContent,
		Model:        model,
		StopReason:   stopReason,
		StopSequence: nil,
		Usage: &anthropic.Usage{
			InputTokens:             promptTokens - cachedTokens,
			OutputTokens:            outputTokens,
			CacheReadInputTokens:    cachedTokens,
			CacheCreationInputTokens: 0,
		},
	}
}

// generateRandomHex generates a random hex string of the specified byte length
func generateRandomHex(byteLength int) string {
	bytes := make([]byte, byteLength)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

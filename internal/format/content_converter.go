// Package format provides conversion between Anthropic and Google Generative AI formats.
// This file corresponds to src/format/content-converter.js in the Node.js version.
package format

import (
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// GooglePart represents a part in Google Generative AI format
type GooglePart struct {
	Text             string                   `json:"text,omitempty"`
	Thought          bool                     `json:"thought,omitempty"`
	ThoughtSignature string                   `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall            `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse        `json:"functionResponse,omitempty"`
	InlineData       *InlineData              `json:"inlineData,omitempty"`
	FileData         *FileData                `json:"fileData,omitempty"`
}

// FunctionCall represents a function call in Google format
type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
	ID   string                 `json:"id,omitempty"`
}

// FunctionResponse represents a function response in Google format
type FunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response,omitempty"`
	ID       string                 `json:"id,omitempty"`
}

// InlineData represents inline data (e.g., base64 images)
type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// FileData represents file data (e.g., URL-referenced files)
type FileData struct {
	MimeType string `json:"mimeType"`
	FileURI  string `json:"fileUri"`
}

// ConvertRole converts Anthropic role to Google role
func ConvertRole(role string) string {
	if role == "assistant" {
		return "model"
	}
	if role == "user" {
		return "user"
	}
	return "user" // Default to user
}

// ConvertContentToParts converts Anthropic message content to Google Generative AI parts
func ConvertContentToParts(content []ContentBlock, isClaudeModel, isGeminiModel bool) []GooglePart {
	parts := make([]GooglePart, 0)
	deferredInlineData := make([]GooglePart, 0) // Collect inlineData to add at the end (Issue #91)

	cache := GetGlobalSignatureCache()

	for _, block := range content {
		switch block.Type {
		case "text":
			// Skip empty text blocks - they cause API errors
			if block.Text != "" && len(block.Text) > 0 {
				parts = append(parts, GooglePart{Text: block.Text})
			}

		case "image":
			// Handle image content
			if block.Source != nil {
				if block.Source.Type == "base64" {
					parts = append(parts, GooglePart{
						InlineData: &InlineData{
							MimeType: block.Source.MediaType,
							Data:     block.Source.Data,
						},
					})
				} else if block.Source.Type == "url" {
					mimeType := block.Source.MediaType
					if mimeType == "" {
						mimeType = "image/jpeg"
					}
					parts = append(parts, GooglePart{
						FileData: &FileData{
							MimeType: mimeType,
							FileURI:  block.Source.URL,
						},
					})
				}
			}

		case "document":
			// Handle document content (e.g. PDF)
			if block.Source != nil {
				if block.Source.Type == "base64" {
					parts = append(parts, GooglePart{
						InlineData: &InlineData{
							MimeType: block.Source.MediaType,
							Data:     block.Source.Data,
						},
					})
				} else if block.Source.Type == "url" {
					mimeType := block.Source.MediaType
					if mimeType == "" {
						mimeType = "application/pdf"
					}
					parts = append(parts, GooglePart{
						FileData: &FileData{
							MimeType: mimeType,
							FileURI:  block.Source.URL,
						},
					})
				}
			}

		case "tool_use":
			// Convert tool_use to functionCall (Google format)
			functionCall := &FunctionCall{
				Name: block.Name,
				Args: block.Input,
			}

			if isClaudeModel && block.ID != "" {
				functionCall.ID = block.ID
			}

			part := GooglePart{FunctionCall: functionCall}

			// For Gemini models, include thoughtSignature at the part level
			if isGeminiModel {
				// Priority: block.thoughtSignature > cache > GEMINI_SKIP_SIGNATURE
				signature := block.ThoughtSignature

				if signature == "" && block.ID != "" {
					signature = cache.GetCachedSignature(block.ID)
					if signature != "" {
						utils.Debug("[ContentConverter] Restored signature from cache for: %s", block.ID)
					}
				}

				if signature == "" {
					signature = config.GeminiSkipSignature
				}
				part.ThoughtSignature = signature
			}

			parts = append(parts, part)

		case "tool_result":
			// Convert tool_result to functionResponse (Google format)
			responseContent := make(map[string]interface{})
			var imageParts []GooglePart

			if block.Content != nil {
				switch c := block.Content.(type) {
				case string:
					responseContent["result"] = c
				case []interface{}:
					// Extract images and text from tool results
					var texts []string
					for _, item := range c {
						if itemMap, ok := item.(map[string]interface{}); ok {
							itemType, _ := itemMap["type"].(string)
							if itemType == "image" {
								if source, ok := itemMap["source"].(map[string]interface{}); ok {
									if source["type"] == "base64" {
										mimeType, _ := source["media_type"].(string)
										data, _ := source["data"].(string)
										imageParts = append(imageParts, GooglePart{
											InlineData: &InlineData{
												MimeType: mimeType,
												Data:     data,
											},
										})
									}
								}
							} else if itemType == "text" {
								if text, ok := itemMap["text"].(string); ok {
									texts = append(texts, text)
								}
							}
						}
					}
					if len(texts) > 0 {
						responseContent["result"] = joinStrings(texts, "\n")
					} else if len(imageParts) > 0 {
						responseContent["result"] = "Image attached"
					} else {
						responseContent["result"] = ""
					}
				case []ContentBlock:
					// Handle typed content blocks
					var texts []string
					for _, item := range c {
						if item.Type == "image" && item.Source != nil && item.Source.Type == "base64" {
							imageParts = append(imageParts, GooglePart{
								InlineData: &InlineData{
									MimeType: item.Source.MediaType,
									Data:     item.Source.Data,
								},
							})
						} else if item.Type == "text" {
							texts = append(texts, item.Text)
						}
					}
					if len(texts) > 0 {
						responseContent["result"] = joinStrings(texts, "\n")
					} else if len(imageParts) > 0 {
						responseContent["result"] = "Image attached"
					} else {
						responseContent["result"] = ""
					}
				}
			}

			funcName := block.ToolUseID
			if funcName == "" {
				funcName = "unknown"
			}

			functionResponse := &FunctionResponse{
				Name:     funcName,
				Response: responseContent,
			}

			// For Claude models, the id field must match the tool_use_id
			if isClaudeModel && block.ToolUseID != "" {
				functionResponse.ID = block.ToolUseID
			}

			parts = append(parts, GooglePart{FunctionResponse: functionResponse})

			// Defer images from the tool result to end of parts array (Issue #91)
			deferredInlineData = append(deferredInlineData, imageParts...)

		case "thinking":
			// Handle thinking blocks with signature compatibility check
			if block.Signature != "" && len(block.Signature) >= config.MinSignatureLength {
				signatureFamily := cache.GetCachedSignatureFamily(block.Signature)
				var targetFamily string
				if isClaudeModel {
					targetFamily = "claude"
				} else if isGeminiModel {
					targetFamily = "gemini"
				}

				// Drop blocks with incompatible signatures for Gemini (cross-model switch)
				if isGeminiModel && signatureFamily != "" && targetFamily != "" && signatureFamily != targetFamily {
					utils.Debug("[ContentConverter] Dropping incompatible %s thinking for %s model", signatureFamily, targetFamily)
					continue
				}

				// Drop blocks with unknown signature origin for Gemini (cold cache - safe default)
				if isGeminiModel && signatureFamily == "" && targetFamily != "" {
					utils.Debug("[ContentConverter] Dropping thinking with unknown signature origin")
					continue
				}

				// Compatible - convert to Gemini format with signature
				parts = append(parts, GooglePart{
					Text:             block.Thinking,
					Thought:          true,
					ThoughtSignature: block.Signature,
				})
			}
			// Unsigned thinking blocks are dropped (existing behavior)
		}
	}

	// Add deferred inlineData at the end (Issue #91)
	parts = append(parts, deferredInlineData...)

	return parts
}

// ConvertStringContentToParts converts string content to Google parts
func ConvertStringContentToParts(content string) []GooglePart {
	return []GooglePart{{Text: content}}
}

// Helper function to join strings
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

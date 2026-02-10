// Package format provides conversion between Anthropic and Google Generative AI formats.
// This file corresponds to src/format/request-converter.js in the Node.js version.
package format

import (
	"encoding/json"
	"strings"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/anthropic"
)

// GoogleRequest represents a request in Google Generative AI format
type GoogleRequest struct {
	Contents          []GoogleContent         `json:"contents"`
	GenerationConfig  *GenerationConfig       `json:"generationConfig,omitempty"`
	SystemInstruction *GoogleContent          `json:"systemInstruction,omitempty"`
	Tools             []GoogleTool            `json:"tools,omitempty"`
	ToolConfig        *ToolConfig             `json:"toolConfig,omitempty"`
}

// ToMap converts GoogleRequest to a map[string]interface{} for dynamic field addition
func (r *GoogleRequest) ToMap() map[string]interface{} {
	// Use JSON marshaling/unmarshaling for reliable conversion
	data, err := json.Marshal(r)
	if err != nil {
		return make(map[string]interface{})
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return make(map[string]interface{})
	}
	return result
}

// GoogleContent represents content in Google format
type GoogleContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GooglePart `json:"parts"`
}

// GenerationConfig holds generation configuration
type GenerationConfig struct {
	MaxOutputTokens int            `json:"maxOutputTokens,omitempty"`
	Temperature     *float64       `json:"temperature,omitempty"`
	TopP            *float64       `json:"topP,omitempty"`
	TopK            *int           `json:"topK,omitempty"`
	StopSequences   []string       `json:"stopSequences,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

// ThinkingConfig holds thinking configuration
type ThinkingConfig struct {
	// Claude style (snake_case)
	IncludeThoughts bool `json:"include_thoughts,omitempty"`
	ThinkingBudget  int  `json:"thinking_budget,omitempty"`

	// Gemini style (camelCase)
	IncludeThoughtsGemini bool `json:"includeThoughts,omitempty"`
	ThinkingBudgetGemini  int  `json:"thinkingBudget,omitempty"`
}

// GoogleTool represents a tool in Google format
type GoogleTool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// FunctionDeclaration represents a function declaration
type FunctionDeclaration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// ToolConfig represents tool configuration
type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

// FunctionCallingConfig represents function calling configuration
type FunctionCallingConfig struct {
	Mode string `json:"mode,omitempty"`
}

// ConvertAnthropicToGoogle converts Anthropic Messages API request to Google format
func ConvertAnthropicToGoogle(anthropicRequest *anthropic.MessagesRequest) *GoogleRequest {
	// [CRITICAL FIX] Pre-clean all cache_control fields from messages (Issue #189)
	messages := CleanCacheControl(convertAnthropicMessages(anthropicRequest.Messages))

	modelName := anthropicRequest.Model
	modelFamily := config.GetModelFamily(modelName)
	isClaudeModel := modelFamily == config.ModelFamilyClaude
	isGeminiModel := modelFamily == config.ModelFamilyGemini
	isThinking := config.IsThinkingModel(modelName)

	googleRequest := &GoogleRequest{
		Contents:         make([]GoogleContent, 0),
		GenerationConfig: &GenerationConfig{},
	}

	// Handle system instruction
	if anthropicRequest.System != nil {
		systemParts := make([]GooglePart, 0)

		switch s := anthropicRequest.System.(type) {
		case string:
			if s != "" {
				systemParts = append(systemParts, GooglePart{Text: s})
			}
		case []interface{}:
			for _, block := range s {
				if blockMap, ok := block.(map[string]interface{}); ok {
					if blockMap["type"] == "text" {
						if text, ok := blockMap["text"].(string); ok {
							systemParts = append(systemParts, GooglePart{Text: text})
						}
					}
				}
			}
		}

		if len(systemParts) > 0 {
			googleRequest.SystemInstruction = &GoogleContent{
				Parts: systemParts,
			}
		}
	}

	// Add interleaved thinking hint for Claude thinking models with tools
	if isClaudeModel && isThinking && len(anthropicRequest.Tools) > 0 {
		hint := "Interleaved thinking is enabled. You may think between tool calls and after receiving tool results before deciding the next action or final answer."
		if googleRequest.SystemInstruction == nil {
			googleRequest.SystemInstruction = &GoogleContent{
				Parts: []GooglePart{{Text: hint}},
			}
		} else if len(googleRequest.SystemInstruction.Parts) > 0 {
			lastPart := &googleRequest.SystemInstruction.Parts[len(googleRequest.SystemInstruction.Parts)-1]
			if lastPart.Text != "" {
				lastPart.Text = lastPart.Text + "\n\n" + hint
			} else {
				googleRequest.SystemInstruction.Parts = append(googleRequest.SystemInstruction.Parts, GooglePart{Text: hint})
			}
		}
	}

	// Apply thinking recovery for Gemini thinking models when needed
	processedMessages := messages

	if isGeminiModel && isThinking && NeedsThinkingRecovery(messages) {
		utils.Debug("[RequestConverter] Applying thinking recovery for Gemini")
		processedMessages = CloseToolLoopForThinking(messages, "gemini")
	}

	// For Claude: apply recovery for cross-model (Gemini→Claude) or unsigned thinking blocks
	needsClaudeRecovery := HasGeminiHistory(messages) || HasUnsignedThinkingBlocks(messages)
	if isClaudeModel && isThinking && needsClaudeRecovery && NeedsThinkingRecovery(messages) {
		utils.Debug("[RequestConverter] Applying thinking recovery for Claude")
		processedMessages = CloseToolLoopForThinking(messages, "claude")
	}

	// Convert messages to contents
	for _, msg := range processedMessages {
		msgContent := msg.Content

		// For assistant messages, process thinking blocks and reorder content
		if (msg.Role == "assistant" || msg.Role == "model") && len(msgContent) > 0 {
			// First, try to restore signatures for unsigned thinking blocks from cache
			msgContent = RestoreThinkingSignatures(msgContent)
			// Remove trailing unsigned thinking blocks
			msgContent = RemoveTrailingThinkingBlocks(msgContent)
			// Reorder: thinking first, then text, then tool_use
			msgContent = ReorderAssistantContent(msgContent)
		}

		parts := ConvertContentToParts(msgContent, isClaudeModel, isGeminiModel)

		// SAFETY: Google API requires at least one part per content message
		if len(parts) == 0 {
			utils.Warn("[RequestConverter] WARNING: Empty parts array after filtering, adding placeholder")
			parts = append(parts, GooglePart{Text: "."})
		}

		content := GoogleContent{
			Role:  ConvertRole(msg.Role),
			Parts: parts,
		}
		googleRequest.Contents = append(googleRequest.Contents, content)
	}

	// Filter unsigned thinking blocks for Claude models
	if isClaudeModel {
		googleRequest.Contents = filterUnsignedThinkingBlocksFromContents(googleRequest.Contents)
	}

	// Generation config
	if anthropicRequest.MaxTokens > 0 {
		googleRequest.GenerationConfig.MaxOutputTokens = anthropicRequest.MaxTokens
	}
	if anthropicRequest.Temperature != nil {
		googleRequest.GenerationConfig.Temperature = anthropicRequest.Temperature
	}
	if anthropicRequest.TopP != nil {
		googleRequest.GenerationConfig.TopP = anthropicRequest.TopP
	}
	if anthropicRequest.TopK != nil {
		googleRequest.GenerationConfig.TopK = anthropicRequest.TopK
	}
	if len(anthropicRequest.StopSequences) > 0 {
		googleRequest.GenerationConfig.StopSequences = anthropicRequest.StopSequences
	}

	// Enable thinking for thinking models
	if isThinking {
		if isClaudeModel {
			// Claude thinking config
			thinkingConfig := &ThinkingConfig{
				IncludeThoughts: true,
			}

			// Only set thinking_budget if explicitly provided
			var thinkingBudget int
			if anthropicRequest.Thinking != nil {
				thinkingBudget = anthropicRequest.Thinking.BudgetTokens
			}

			if thinkingBudget > 0 {
				thinkingConfig.ThinkingBudget = thinkingBudget
				utils.Debug("[RequestConverter] Claude thinking enabled with budget: %d", thinkingBudget)

				// Validate max_tokens > thinking_budget as required by the API
				if googleRequest.GenerationConfig.MaxOutputTokens > 0 &&
					googleRequest.GenerationConfig.MaxOutputTokens <= thinkingBudget {
					// Bump max_tokens to allow for some response content
					adjustedMaxTokens := thinkingBudget + 8192
					utils.Warn("[RequestConverter] max_tokens (%d) <= thinking_budget (%d). Adjusting to %d",
						googleRequest.GenerationConfig.MaxOutputTokens, thinkingBudget, adjustedMaxTokens)
					googleRequest.GenerationConfig.MaxOutputTokens = adjustedMaxTokens
				}
			} else {
				utils.Debug("[RequestConverter] Claude thinking enabled (no budget specified)")
			}

			googleRequest.GenerationConfig.ThinkingConfig = thinkingConfig

		} else if isGeminiModel {
			// Gemini thinking config (uses camelCase)
			budget := 16000
			if anthropicRequest.Thinking != nil && anthropicRequest.Thinking.BudgetTokens > 0 {
				budget = anthropicRequest.Thinking.BudgetTokens
			}

			thinkingConfig := &ThinkingConfig{
				IncludeThoughtsGemini: true,
				ThinkingBudgetGemini:  budget,
			}
			utils.Debug("[RequestConverter] Gemini thinking enabled with budget: %d", budget)

			googleRequest.GenerationConfig.ThinkingConfig = thinkingConfig
		}
	}

	// Convert tools to Google format
	if len(anthropicRequest.Tools) > 0 {
		functionDeclarations := make([]FunctionDeclaration, 0, len(anthropicRequest.Tools))

		for idx, tool := range anthropicRequest.Tools {
			// Extract name (required field)
			name := tool.Name
			if name == "" {
				name = "tool-" + string(rune('0'+idx))
			}

			// Extract description
			description := tool.Description

			// Extract schema from InputSchema (json.RawMessage)
			var schema map[string]interface{}
			if len(tool.InputSchema) > 0 {
				if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
					utils.Warn("[RequestConverter] Failed to unmarshal tool schema for %s: %v", name, err)
					schema = map[string]interface{}{"type": "object"}
				}
			} else {
				schema = map[string]interface{}{"type": "object"}
			}

			// Sanitize and clean schema
			parameters := SanitizeSchema(schema)
			parameters = CleanSchema(parameters)

			// Clean name (alphanumeric, underscore, hyphen only, max 64 chars)
			cleanName := cleanToolName(name)

			functionDeclarations = append(functionDeclarations, FunctionDeclaration{
				Name:        cleanName,
				Description: description,
				Parameters:  parameters,
			})
		}

		googleRequest.Tools = []GoogleTool{{FunctionDeclarations: functionDeclarations}}

		// For Claude models, set functionCallingConfig.mode = "VALIDATED"
		if isClaudeModel {
			googleRequest.ToolConfig = &ToolConfig{
				FunctionCallingConfig: &FunctionCallingConfig{
					Mode: "VALIDATED",
				},
			}
		}
	}

	// Cap max tokens for Gemini models
	if isGeminiModel && googleRequest.GenerationConfig.MaxOutputTokens > config.GeminiMaxOutputTokens {
		utils.Debug("[RequestConverter] Capping Gemini max_tokens from %d to %d",
			googleRequest.GenerationConfig.MaxOutputTokens, config.GeminiMaxOutputTokens)
		googleRequest.GenerationConfig.MaxOutputTokens = config.GeminiMaxOutputTokens
	}

	return googleRequest
}

// convertAnthropicMessages converts Anthropic messages to internal Message format
func convertAnthropicMessages(messages []anthropic.Message) []Message {
	result := make([]Message, 0, len(messages))

	for _, msg := range messages {
		converted := Message{
			Role:    msg.Role,
			Content: convertAnthropicContent(msg.Content),
		}
		result = append(result, converted)
	}

	return result
}

// convertAnthropicContent converts Anthropic content to internal ContentBlock format
func convertAnthropicContent(content interface{}) []ContentBlock {
	switch c := content.(type) {
	case string:
		return []ContentBlock{{Type: "text", Text: c}}
	case []interface{}:
		result := make([]ContentBlock, 0, len(c))
		for _, item := range c {
			if blockMap, ok := item.(map[string]interface{}); ok {
				block := convertContentBlockMap(blockMap)
				result = append(result, block)
			}
		}
		return result
	case []anthropic.ContentBlock:
		result := make([]ContentBlock, 0, len(c))
		for _, item := range c {
			block := ContentBlock{
				Type:             item.Type,
				Text:             item.Text,
				Thinking:         item.Thinking,
				Signature:        item.Signature,
				ThoughtSignature: item.ThoughtSignature,
				ID:               item.ID,
				Name:             item.Name,
				ToolUseID:        item.ToolUseID,
				Content:          item.Content,
			}
			// Convert Input from json.RawMessage to map[string]interface{}
			if len(item.Input) > 0 {
				var inputMap map[string]interface{}
				if err := json.Unmarshal(item.Input, &inputMap); err == nil {
					block.Input = inputMap
				}
			}
			if item.Source != nil {
				block.Source = &ImageSource{
					Type:      item.Source.Type,
					MediaType: item.Source.MediaType,
					Data:      item.Source.Data,
					URL:       item.Source.URL,
				}
			}
			if item.CacheControl != nil {
				block.CacheControl = item.CacheControl
			}
			result = append(result, block)
		}
		return result
	default:
		return []ContentBlock{}
	}
}

// convertContentBlockMap converts a map to ContentBlock
func convertContentBlockMap(blockMap map[string]interface{}) ContentBlock {
	block := ContentBlock{}

	if t, ok := blockMap["type"].(string); ok {
		block.Type = t
	}
	if text, ok := blockMap["text"].(string); ok {
		block.Text = text
	}
	if thinking, ok := blockMap["thinking"].(string); ok {
		block.Thinking = thinking
	}
	if sig, ok := blockMap["signature"].(string); ok {
		block.Signature = sig
	}
	if tSig, ok := blockMap["thoughtSignature"].(string); ok {
		block.ThoughtSignature = tSig
	}
	if thought, ok := blockMap["thought"].(bool); ok {
		block.Thought = thought
	}
	if id, ok := blockMap["id"].(string); ok {
		block.ID = id
	}
	if name, ok := blockMap["name"].(string); ok {
		block.Name = name
	}
	if input, ok := blockMap["input"].(map[string]interface{}); ok {
		block.Input = input
	}
	if toolUseID, ok := blockMap["tool_use_id"].(string); ok {
		block.ToolUseID = toolUseID
	}
	if content := blockMap["content"]; content != nil {
		block.Content = content
	}
	if data, ok := blockMap["data"].(string); ok {
		block.Data = data
	}
	if cc := blockMap["cache_control"]; cc != nil {
		block.CacheControl = cc
	}

	// Handle source for images
	if sourceMap, ok := blockMap["source"].(map[string]interface{}); ok {
		block.Source = &ImageSource{}
		if t, ok := sourceMap["type"].(string); ok {
			block.Source.Type = t
		}
		if mt, ok := sourceMap["media_type"].(string); ok {
			block.Source.MediaType = mt
		}
		if d, ok := sourceMap["data"].(string); ok {
			block.Source.Data = d
		}
		if u, ok := sourceMap["url"].(string); ok {
			block.Source.URL = u
		}
	}

	return block
}

// filterUnsignedThinkingBlocksFromContents filters unsigned thinking blocks from Google contents
func filterUnsignedThinkingBlocksFromContents(contents []GoogleContent) []GoogleContent {
	result := make([]GoogleContent, 0, len(contents))

	for _, content := range contents {
		filteredParts := make([]GooglePart, 0, len(content.Parts))

		for _, part := range content.Parts {
			// Check if it's a thinking part
			if part.Thought {
				// Keep only if it has a valid signature
				if part.ThoughtSignature != "" && len(part.ThoughtSignature) >= config.MinSignatureLength {
					filteredParts = append(filteredParts, part)
				} else {
					utils.Debug("[RequestConverter] Dropping unsigned thinking block")
				}
			} else {
				filteredParts = append(filteredParts, part)
			}
		}

		result = append(result, GoogleContent{
			Role:  content.Role,
			Parts: filteredParts,
		})
	}

	return result
}

// cleanToolName cleans a tool name to be valid for the API
func cleanToolName(name string) string {
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	cleaned := result.String()
	if len(cleaned) > 64 {
		cleaned = cleaned[:64]
	}
	return cleaned
}

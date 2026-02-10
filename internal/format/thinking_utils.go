// Package format provides conversion between Anthropic and Google Generative AI formats.
// This file corresponds to src/format/thinking-utils.js in the Node.js version.
package format

import (
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// ContentBlock represents a content block in Anthropic format
type ContentBlock struct {
	Type             string                 `json:"type,omitempty"`
	Text             string                 `json:"text,omitempty"`
	Thinking         string                 `json:"thinking,omitempty"`
	Signature        string                 `json:"signature,omitempty"`
	ThoughtSignature string                 `json:"thoughtSignature,omitempty"`
	Thought          bool                   `json:"thought,omitempty"`
	ID               string                 `json:"id,omitempty"`
	Name             string                 `json:"name,omitempty"`
	Input            map[string]interface{} `json:"input,omitempty"`
	ToolUseID        string                 `json:"tool_use_id,omitempty"`
	Content          interface{}            `json:"content,omitempty"`
	CacheControl     interface{}            `json:"cache_control,omitempty"`
	Data             string                 `json:"data,omitempty"`

	// Image source
	Source *ImageSource `json:"source,omitempty"`
}

// ImageSource represents the source of an image
type ImageSource struct {
	Type      string `json:"type,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// Message represents a message in Anthropic format
type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content,omitempty"`
	// For Gemini format
	Parts []map[string]interface{} `json:"parts,omitempty"`
}

// CleanCacheControl removes cache_control fields from all content blocks in messages.
// This is a critical fix for Issue #189 where Claude Code CLI sends cache_control
// fields that the Cloud Code API rejects with "Extra inputs are not permitted".
func CleanCacheControl(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}

	removedCount := 0
	cleaned := make([]Message, 0, len(messages))

	for _, message := range messages {
		if len(message.Content) == 0 {
			cleaned = append(cleaned, message)
			continue
		}

		cleanedContent := make([]ContentBlock, 0, len(message.Content))
		for _, block := range message.Content {
			// Check if cache_control exists
			if block.CacheControl != nil {
				// Create a copy without cache_control
				newBlock := block
				newBlock.CacheControl = nil
				cleanedContent = append(cleanedContent, newBlock)
				removedCount++
			} else {
				cleanedContent = append(cleanedContent, block)
			}
		}

		cleaned = append(cleaned, Message{
			Role:    message.Role,
			Content: cleanedContent,
		})
	}

	if removedCount > 0 {
		utils.Debug("[ThinkingUtils] Removed cache_control from %d block(s)", removedCount)
	}

	return cleaned
}

// isThinkingPart checks if a block is a thinking block
func isThinkingPart(block ContentBlock) bool {
	return block.Type == "thinking" ||
		block.Type == "redacted_thinking" ||
		block.Thinking != "" ||
		block.Thought
}

// hasValidSignature checks if a thinking part has a valid signature
func hasValidSignature(block ContentBlock) bool {
	var signature string
	if block.Thought {
		signature = block.ThoughtSignature
	} else {
		signature = block.Signature
	}
	return signature != "" && len(signature) >= config.MinSignatureLength
}

// HasGeminiHistory checks if conversation history contains Gemini-style messages.
// Gemini puts thoughtSignature on tool_use blocks, Claude puts signature on thinking blocks.
func HasGeminiHistory(messages []Message) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.ThoughtSignature != "" {
				return true
			}
		}
	}
	return false
}

// HasUnsignedThinkingBlocks checks if conversation has unsigned thinking blocks that will be dropped.
func HasUnsignedThinkingBlocks(messages []Message) bool {
	for _, msg := range messages {
		if msg.Role != "assistant" && msg.Role != "model" {
			continue
		}
		for _, block := range msg.Content {
			if isThinkingPart(block) && !hasValidSignature(block) {
				return true
			}
		}
	}
	return false
}

// sanitizeAnthropicThinkingBlock sanitizes a thinking block by removing extra fields
func sanitizeAnthropicThinkingBlock(block ContentBlock) ContentBlock {
	if block.Type == "thinking" {
		return ContentBlock{
			Type:      "thinking",
			Thinking:  block.Thinking,
			Signature: block.Signature,
		}
	}

	if block.Type == "redacted_thinking" {
		return ContentBlock{
			Type: "redacted_thinking",
			Data: block.Data,
		}
	}

	return block
}

// sanitizeTextBlock sanitizes a text block by removing extra fields
func sanitizeTextBlock(block ContentBlock) ContentBlock {
	if block.Type != "text" {
		return block
	}
	return ContentBlock{
		Type: "text",
		Text: block.Text,
	}
}

// sanitizeToolUseBlock sanitizes a tool_use block by removing extra fields
func sanitizeToolUseBlock(block ContentBlock) ContentBlock {
	if block.Type != "tool_use" {
		return block
	}
	sanitized := ContentBlock{
		Type:  "tool_use",
		ID:    block.ID,
		Name:  block.Name,
		Input: block.Input,
	}
	// Preserve thoughtSignature for Gemini models
	if block.ThoughtSignature != "" {
		sanitized.ThoughtSignature = block.ThoughtSignature
	}
	return sanitized
}

// RestoreThinkingSignatures filters thinking blocks: keep only those with valid signatures.
func RestoreThinkingSignatures(content []ContentBlock) []ContentBlock {
	if len(content) == 0 {
		return content
	}

	originalLength := len(content)
	filtered := make([]ContentBlock, 0, originalLength)

	for _, block := range content {
		if block.Type != "thinking" {
			filtered = append(filtered, block)
			continue
		}

		// Keep blocks with valid signatures, sanitized
		if block.Signature != "" && len(block.Signature) >= config.MinSignatureLength {
			filtered = append(filtered, sanitizeAnthropicThinkingBlock(block))
		}
		// Unsigned thinking blocks are dropped
	}

	if len(filtered) < originalLength {
		utils.Debug("[ThinkingUtils] Dropped %d unsigned thinking block(s)", originalLength-len(filtered))
	}

	return filtered
}

// RemoveTrailingThinkingBlocks removes trailing unsigned thinking blocks from assistant messages.
func RemoveTrailingThinkingBlocks(content []ContentBlock) []ContentBlock {
	if len(content) == 0 {
		return content
	}

	// Work backwards from the end, removing thinking blocks
	endIndex := len(content)
	for i := len(content) - 1; i >= 0; i-- {
		block := content[i]

		// Check if it's a thinking block (any format)
		isThinking := isThinkingPart(block)

		if isThinking {
			// Check if it has a valid signature
			if !hasValidSignature(block) {
				endIndex = i
			} else {
				break // Stop at signed thinking block
			}
		} else {
			break // Stop at first non-thinking block
		}
	}

	if endIndex < len(content) {
		utils.Debug("[ThinkingUtils] Removed %d trailing unsigned thinking blocks", len(content)-endIndex)
		return content[:endIndex]
	}

	return content
}

// ReorderAssistantContent reorders content so that:
// 1. Thinking blocks come first (required when thinking is enabled)
// 2. Text blocks come in the middle (filtering out empty/useless ones)
// 3. Tool_use blocks come at the end (required before tool_result)
func ReorderAssistantContent(content []ContentBlock) []ContentBlock {
	if len(content) == 0 {
		return content
	}

	// Even for single-element arrays, we need to sanitize thinking blocks
	if len(content) == 1 {
		block := content[0]
		if block.Type == "thinking" || block.Type == "redacted_thinking" {
			return []ContentBlock{sanitizeAnthropicThinkingBlock(block)}
		}
		return content
	}

	var thinkingBlocks []ContentBlock
	var textBlocks []ContentBlock
	var toolUseBlocks []ContentBlock
	droppedEmptyBlocks := 0

	for _, block := range content {
		if block.Type == "thinking" || block.Type == "redacted_thinking" {
			thinkingBlocks = append(thinkingBlocks, sanitizeAnthropicThinkingBlock(block))
		} else if block.Type == "tool_use" {
			toolUseBlocks = append(toolUseBlocks, sanitizeToolUseBlock(block))
		} else if block.Type == "text" {
			// Only keep text blocks with meaningful content
			if block.Text != "" && len(block.Text) > 0 {
				textBlocks = append(textBlocks, sanitizeTextBlock(block))
			} else {
				droppedEmptyBlocks++
			}
		} else {
			// Other block types go in the text position
			textBlocks = append(textBlocks, block)
		}
	}

	if droppedEmptyBlocks > 0 {
		utils.Debug("[ThinkingUtils] Dropped %d empty text block(s)", droppedEmptyBlocks)
	}

	reordered := make([]ContentBlock, 0, len(thinkingBlocks)+len(textBlocks)+len(toolUseBlocks))
	reordered = append(reordered, thinkingBlocks...)
	reordered = append(reordered, textBlocks...)
	reordered = append(reordered, toolUseBlocks...)

	return reordered
}

// FilterUnsignedThinkingBlocks filters unsigned thinking blocks from contents (Gemini format)
func FilterUnsignedThinkingBlocks(contents []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(contents))

	for _, content := range contents {
		if parts, ok := content["parts"].([]interface{}); ok {
			filteredParts := filterPartsArray(parts)
			newContent := make(map[string]interface{})
			for k, v := range content {
				if k != "parts" {
					newContent[k] = v
				}
			}
			newContent["parts"] = filteredParts
			result = append(result, newContent)
		} else {
			result = append(result, content)
		}
	}

	return result
}

func filterPartsArray(parts []interface{}) []interface{} {
	filtered := make([]interface{}, 0, len(parts))

	for _, item := range parts {
		partMap, ok := item.(map[string]interface{})
		if !ok {
			filtered = append(filtered, item)
			continue
		}

		// Check if it's a thinking part
		if !isThinkingPartMap(partMap) {
			filtered = append(filtered, item)
			continue
		}

		// Keep items with valid signatures
		if hasValidSignatureMap(partMap) {
			filtered = append(filtered, sanitizeThinkingPartMap(partMap))
			continue
		}

		// Drop unsigned thinking blocks
		utils.Debug("[ThinkingUtils] Dropping unsigned thinking block")
	}

	return filtered
}

func isThinkingPartMap(part map[string]interface{}) bool {
	partType, _ := part["type"].(string)
	_, hasThinking := part["thinking"]
	thought, _ := part["thought"].(bool)

	return partType == "thinking" ||
		partType == "redacted_thinking" ||
		hasThinking ||
		thought
}

func hasValidSignatureMap(part map[string]interface{}) bool {
	thought, _ := part["thought"].(bool)
	var signature string
	if thought {
		signature, _ = part["thoughtSignature"].(string)
	} else {
		signature, _ = part["signature"].(string)
	}
	return signature != "" && len(signature) >= config.MinSignatureLength
}

func sanitizeThinkingPartMap(part map[string]interface{}) map[string]interface{} {
	thought, _ := part["thought"].(bool)

	// Gemini-style thought blocks
	if thought {
		sanitized := map[string]interface{}{"thought": true}
		if text, ok := part["text"]; ok {
			sanitized["text"] = text
		}
		if sig, ok := part["thoughtSignature"]; ok {
			sanitized["thoughtSignature"] = sig
		}
		return sanitized
	}

	// Anthropic-style thinking blocks
	partType, _ := part["type"].(string)
	if partType == "thinking" || part["thinking"] != nil {
		sanitized := map[string]interface{}{"type": "thinking"}
		if thinking, ok := part["thinking"]; ok {
			sanitized["thinking"] = thinking
		}
		if sig, ok := part["signature"]; ok {
			sanitized["signature"] = sig
		}
		return sanitized
	}

	return part
}

// ============================================================================
// Thinking Recovery Functions
// ============================================================================

// conversationState holds the analyzed state of a conversation
type conversationState struct {
	InToolLoop       bool
	InterruptedTool  bool
	TurnHasThinking  bool
	ToolResultCount  int
	LastAssistantIdx int
}

// analyzeConversationState analyzes conversation state to detect if we're in a corrupted state
func analyzeConversationState(messages []Message) conversationState {
	state := conversationState{LastAssistantIdx: -1}

	if len(messages) == 0 {
		return state
	}

	// Find the last assistant message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" || messages[i].Role == "model" {
			state.LastAssistantIdx = i
			break
		}
	}

	if state.LastAssistantIdx == -1 {
		return state
	}

	lastAssistant := messages[state.LastAssistantIdx]
	hasToolUse := messageHasToolUse(lastAssistant)
	hasThinking := messageHasValidThinking(lastAssistant)

	// Count trailing tool results after the assistant message
	hasPlainUserMessageAfter := false
	for i := state.LastAssistantIdx + 1; i < len(messages); i++ {
		if messageHasToolResult(messages[i]) {
			state.ToolResultCount++
		}
		if isPlainUserMessage(messages[i]) {
			hasPlainUserMessageAfter = true
		}
	}

	// We're in a tool loop if: assistant has tool_use AND there are tool_results after
	state.InToolLoop = hasToolUse && state.ToolResultCount > 0

	// We have an interrupted tool if: assistant has tool_use, NO tool_results,
	// but there IS a plain user message after (user interrupted and sent new message)
	state.InterruptedTool = hasToolUse && state.ToolResultCount == 0 && hasPlainUserMessageAfter

	state.TurnHasThinking = hasThinking

	return state
}

func messageHasValidThinking(message Message) bool {
	for _, block := range message.Content {
		if !isThinkingPart(block) {
			continue
		}
		// Check for valid signature (Anthropic style)
		if block.Signature != "" && len(block.Signature) >= config.MinSignatureLength {
			return true
		}
		// Check for thoughtSignature (Gemini style on functionCall)
		if block.ThoughtSignature != "" && len(block.ThoughtSignature) >= config.MinSignatureLength {
			return true
		}
	}
	return false
}

func messageHasToolUse(message Message) bool {
	for _, block := range message.Content {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}

func messageHasToolResult(message Message) bool {
	for _, block := range message.Content {
		if block.Type == "tool_result" {
			return true
		}
	}
	return false
}

func isPlainUserMessage(message Message) bool {
	if message.Role != "user" {
		return false
	}
	// Check if it has tool_result blocks
	for _, block := range message.Content {
		if block.Type == "tool_result" {
			return false
		}
	}
	return true
}

// NeedsThinkingRecovery checks if conversation needs thinking recovery.
func NeedsThinkingRecovery(messages []Message) bool {
	state := analyzeConversationState(messages)

	// Recovery is only needed in tool loops or interrupted tools
	if !state.InToolLoop && !state.InterruptedTool {
		return false
	}

	// Need recovery if no valid thinking blocks exist
	return !state.TurnHasThinking
}

// stripInvalidThinkingBlocks strips invalid or incompatible thinking blocks from messages
func stripInvalidThinkingBlocks(messages []Message, targetFamily string) []Message {
	strippedCount := 0
	cache := GetGlobalSignatureCache()

	result := make([]Message, 0, len(messages))

	for _, msg := range messages {
		if len(msg.Content) == 0 {
			result = append(result, msg)
			continue
		}

		filtered := make([]ContentBlock, 0, len(msg.Content))

		for _, block := range msg.Content {
			// Keep non-thinking blocks
			if !isThinkingPart(block) {
				filtered = append(filtered, block)
				continue
			}

			// Check generic validity (has signature of sufficient length)
			if !hasValidSignature(block) {
				strippedCount++
				continue
			}

			// Check family compatibility only for Gemini targets
			// Claude can validate its own signatures, so we don't drop for Claude
			if targetFamily == "gemini" {
				var signature string
				if block.Thought {
					signature = block.ThoughtSignature
				} else {
					signature = block.Signature
				}
				signatureFamily := cache.GetCachedSignatureFamily(signature)

				// For Gemini: drop unknown or mismatched signatures
				if signatureFamily == "" || signatureFamily != targetFamily {
					strippedCount++
					continue
				}
			}

			filtered = append(filtered, block)
		}

		// Use '.' instead of '' because claude models reject empty text parts
		if len(filtered) == 0 {
			filtered = []ContentBlock{{Type: "text", Text: "."}}
		}

		result = append(result, Message{
			Role:    msg.Role,
			Content: filtered,
		})
	}

	if strippedCount > 0 {
		utils.Debug("[ThinkingUtils] Stripped %d invalid/incompatible thinking block(s)", strippedCount)
	}

	return result
}

// CloseToolLoopForThinking closes tool loop by injecting synthetic messages.
// This allows the model to start a fresh turn when thinking is corrupted.
func CloseToolLoopForThinking(messages []Message, targetFamily string) []Message {
	state := analyzeConversationState(messages)

	// Handle neither tool loop nor interrupted tool
	if !state.InToolLoop && !state.InterruptedTool {
		return messages
	}

	// Strip only invalid/incompatible thinking blocks (keep valid ones)
	modified := stripInvalidThinkingBlocks(messages, targetFamily)

	if state.InterruptedTool {
		// For interrupted tools: just strip thinking and add a synthetic assistant message
		// to acknowledge the interruption before the user's new message

		// Find where to insert the synthetic message (before the plain user message)
		insertIdx := state.LastAssistantIdx + 1

		// Insert synthetic assistant message acknowledging interruption
		syntheticMsg := Message{
			Role:    "assistant",
			Content: []ContentBlock{{Type: "text", Text: "[Tool call was interrupted.]"}},
		}

		// Insert at position
		newModified := make([]Message, 0, len(modified)+1)
		newModified = append(newModified, modified[:insertIdx]...)
		newModified = append(newModified, syntheticMsg)
		newModified = append(newModified, modified[insertIdx:]...)
		modified = newModified

		utils.Debug("[ThinkingUtils] Applied thinking recovery for interrupted tool")
	} else if state.InToolLoop {
		// For tool loops: add synthetic messages to close the loop
		syntheticText := "[Tool execution completed.]"
		if state.ToolResultCount > 1 {
			syntheticText = "[" + string(rune('0'+state.ToolResultCount)) + " tool executions completed.]"
		}

		// Inject synthetic model message to complete the turn
		modified = append(modified, Message{
			Role:    "assistant",
			Content: []ContentBlock{{Type: "text", Text: syntheticText}},
		})

		// Inject synthetic user message to start fresh
		modified = append(modified, Message{
			Role:    "user",
			Content: []ContentBlock{{Type: "text", Text: "[Continue]"}},
		})

		utils.Debug("[ThinkingUtils] Applied thinking recovery for tool loop")
	}

	return modified
}

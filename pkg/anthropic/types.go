// Package anthropic provides type definitions for the Anthropic Messages API.
// This file defines all request and response types used by the proxy.
package anthropic

import (
	"encoding/json"
	"time"
)

// Message represents an Anthropic message
type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a content block in a message
type ContentBlock struct {
	Type string `json:"type"`

	// Text block fields
	Text string `json:"text,omitempty"`

	// Thinking block fields
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// Tool use fields
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// Tool result fields
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"` // Can be string or []ContentBlock

	// Gemini-specific (tool use)
	ThoughtSignature string `json:"thoughtSignature,omitempty"`

	// Image fields
	Source *ImageSource `json:"source,omitempty"`

	// Cache control (stripped before sending to API)
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ImageSource represents the source of an image
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	URL       string `json:"url,omitempty"`
}

// CacheControl for prompt caching
type CacheControl struct {
	Type string `json:"type"`
}

// Tool represents a tool definition
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolChoice represents tool selection preference
type ToolChoice struct {
	Type                string `json:"type"`
	Name                string `json:"name,omitempty"`
	DisableParallelToolUse bool `json:"disable_parallel_tool_use,omitempty"`
}

// ThinkingConfig for thinking models
type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// SystemContent represents system prompt content (can be string or array)
type SystemContent interface{}

// MessagesRequest represents a request to POST /v1/messages
type MessagesRequest struct {
	Model         string         `json:"model"`
	Messages      []Message      `json:"messages"`
	MaxTokens     int            `json:"max_tokens"`
	Stream        bool           `json:"stream,omitempty"`
	System        SystemContent  `json:"system,omitempty"`
	Tools         []Tool         `json:"tools,omitempty"`
	ToolChoice    *ToolChoice    `json:"tool_choice,omitempty"`
	Thinking      *ThinkingConfig `json:"thinking,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	TopK          *int           `json:"top_k,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	StopSequences []string       `json:"stop_sequences,omitempty"`
	Metadata      *Metadata      `json:"metadata,omitempty"`
}

// Metadata for request tracking
type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

// MessagesResponse represents a response from POST /v1/messages
type MessagesResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        *Usage         `json:"usage,omitempty"`
}

// Usage represents token usage
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// SSEEventType represents the type of SSE event
type SSEEventType string

const (
	SSEEventMessageStart      SSEEventType = "message_start"
	SSEEventContentBlockStart SSEEventType = "content_block_start"
	SSEEventContentBlockDelta SSEEventType = "content_block_delta"
	SSEEventContentBlockStop  SSEEventType = "content_block_stop"
	SSEEventMessageDelta      SSEEventType = "message_delta"
	SSEEventMessageStop       SSEEventType = "message_stop"
	SSEEventPing              SSEEventType = "ping"
	SSEEventError             SSEEventType = "error"
)

// SSEEvent represents a streaming SSE event
type SSEEvent struct {
	Type         SSEEventType     `json:"type"`
	Message      *MessagesResponse `json:"message,omitempty"`
	Index        int              `json:"index,omitempty"`
	Delta        *ContentDelta    `json:"delta,omitempty"`
	Usage        *Usage           `json:"usage,omitempty"`
	ContentBlock *ContentBlock    `json:"content_block,omitempty"`
	Error        *SSEError        `json:"error,omitempty"`
}

// ContentDelta for streaming deltas
type ContentDelta struct {
	Type             string `json:"type"`
	Text             string `json:"text,omitempty"`
	Thinking         string `json:"thinking,omitempty"`
	Signature        string `json:"signature,omitempty"`
	PartialJSON      string `json:"partial_json,omitempty"`
	StopReason       string `json:"stop_reason,omitempty"`
	ThoughtSignature string `json:"thoughtSignature,omitempty"`
}

// SSEError for error events
type SSEError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Model represents a model in the /v1/models response
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelsResponse represents a response from GET /v1/models
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Type  string     `json:"type"`
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error details
type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// NewErrorResponse creates a new error response
func NewErrorResponse(errorType, message string) *ErrorResponse {
	return &ErrorResponse{
		Type: "error",
		Error: ErrorDetail{
			Type:    errorType,
			Message: message,
		},
	}
}

// NewMessagesResponse creates a new messages response
func NewMessagesResponse(id, model string, content []ContentBlock, stopReason string, usage *Usage) *MessagesResponse {
	return &MessagesResponse{
		ID:         id,
		Type:       "message",
		Role:       "assistant",
		Content:    content,
		Model:      model,
		StopReason: stopReason,
		Usage:      usage,
	}
}

// IsToolUse checks if a content block is a tool use
func (cb *ContentBlock) IsToolUse() bool {
	return cb.Type == "tool_use"
}

// IsToolResult checks if a content block is a tool result
func (cb *ContentBlock) IsToolResult() bool {
	return cb.Type == "tool_result"
}

// IsText checks if a content block is text
func (cb *ContentBlock) IsText() bool {
	return cb.Type == "text"
}

// IsThinking checks if a content block is thinking
func (cb *ContentBlock) IsThinking() bool {
	return cb.Type == "thinking"
}

// IsImage checks if a content block is an image
func (cb *ContentBlock) IsImage() bool {
	return cb.Type == "image"
}

// HasSignature checks if a thinking block has a valid signature
func (cb *ContentBlock) HasSignature() bool {
	return cb.IsThinking() && len(cb.Signature) >= 50
}

// GenerateMessageID generates a unique message ID
func GenerateMessageID() string {
	return "msg_" + generateRandomHex(24)
}

// GenerateToolUseID generates a unique tool use ID
func GenerateToolUseID() string {
	return "toolu_" + generateRandomHex(24)
}

// generateRandomHex generates a random hex string of the specified length
func generateRandomHex(length int) string {
	const chars = "0123456789abcdef"
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[time.Now().UnixNano()%16]
	}
	return string(result)
}

// CloneContentBlock creates a deep copy of a content block
func CloneContentBlock(cb ContentBlock) ContentBlock {
	clone := cb
	if cb.Input != nil {
		clone.Input = make(json.RawMessage, len(cb.Input))
		copy(clone.Input, cb.Input)
	}
	if cb.Source != nil {
		src := *cb.Source
		clone.Source = &src
	}
	if cb.CacheControl != nil {
		cc := *cb.CacheControl
		clone.CacheControl = &cc
	}
	return clone
}

// CloneMessage creates a deep copy of a message
func CloneMessage(msg Message) Message {
	clone := msg
	clone.Content = make([]ContentBlock, len(msg.Content))
	for i, cb := range msg.Content {
		clone.Content[i] = CloneContentBlock(cb)
	}
	return clone
}

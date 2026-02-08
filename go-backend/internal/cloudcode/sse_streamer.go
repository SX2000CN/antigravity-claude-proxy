// Package cloudcode provides Cloud Code API client implementation.
// This file corresponds to src/cloudcode/sse-streamer.js in the Node.js version.
package cloudcode

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/format"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/anthropic"
)

// SSEEvent represents an Anthropic SSE event
type SSEEvent struct {
	Type         string               `json:"type"`
	Index        int                  `json:"index,omitempty"`
	Message      *anthropic.MessagesResponse `json:"message,omitempty"`
	ContentBlock *anthropic.ContentBlock     `json:"content_block,omitempty"`
	Delta        map[string]interface{}      `json:"delta,omitempty"`
	Usage        *anthropic.Usage            `json:"usage,omitempty"`
}

// StreamSSEResponse streams SSE response and yields Anthropic-format events
// Returns a channel of SSEEvent and error channel
func StreamSSEResponse(reader io.Reader, originalModel string) (<-chan *SSEEvent, <-chan error) {
	events := make(chan *SSEEvent, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		messageID := "msg_" + generateHexID(16)
		hasEmittedStart := false
		blockIndex := 0
		var currentBlockType string // "", "thinking", "text", "tool_use", "image"
		var currentThinkingSignature string
		inputTokens := 0
		outputTokens := 0
		cacheReadTokens := 0
		var stopReason string

		scanner := bufio.NewScanner(reader)
		// Increase buffer size for large responses
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			jsonText := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if jsonText == "" {
				continue
			}

			var data SSEResponse
			if err := json.Unmarshal([]byte(jsonText), &data); err != nil {
				utils.Warn("[CloudCode] SSE parse error: %v", err)
				continue
			}

			// Get inner response
			innerResponse := data.Response
			if innerResponse == nil {
				innerResponse = &SSEInnerResponse{
					Candidates:    data.Candidates,
					UsageMetadata: data.UsageMetadata,
				}
			}

			// Extract usage metadata
			if innerResponse.UsageMetadata != nil {
				inputTokens = max(inputTokens, innerResponse.UsageMetadata.PromptTokenCount)
				outputTokens = max(outputTokens, innerResponse.UsageMetadata.CandidatesTokenCount)
				cacheReadTokens = max(cacheReadTokens, innerResponse.UsageMetadata.CachedContentTokenCount)
			}

			if len(innerResponse.Candidates) == 0 {
				continue
			}

			firstCandidate := innerResponse.Candidates[0]
			if firstCandidate.Content == nil {
				if firstCandidate.FinishReason != "" && stopReason == "" {
					stopReason = mapFinishReason(firstCandidate.FinishReason)
				}
				continue
			}

			parts := firstCandidate.Content.Parts

			// Emit message_start on first data
			if !hasEmittedStart && len(parts) > 0 {
				hasEmittedStart = true
				events <- &SSEEvent{
					Type: "message_start",
					Message: &anthropic.MessagesResponse{
						ID:           messageID,
						Type:         "message",
						Role:         "assistant",
						Content:      []anthropic.ContentBlock{},
						Model:        originalModel,
						StopReason:   "",
						StopSequence: nil,
						Usage: &anthropic.Usage{
							InputTokens:             inputTokens - cacheReadTokens,
							OutputTokens:            0,
							CacheReadInputTokens:    cacheReadTokens,
							CacheCreationInputTokens: 0,
						},
					},
				}
			}

			// Process each part
			for _, part := range parts {
				if part.Thought {
					// Handle thinking block
					text := part.Text
					signature := part.ThoughtSignature

					if currentBlockType != "thinking" {
						if currentBlockType != "" {
							events <- &SSEEvent{Type: "content_block_stop", Index: blockIndex}
							blockIndex++
						}
						currentBlockType = "thinking"
						currentThinkingSignature = ""
						events <- &SSEEvent{
							Type:  "content_block_start",
							Index: blockIndex,
							ContentBlock: &anthropic.ContentBlock{
								Type:     "thinking",
								Thinking: "",
							},
						}
					}

					if signature != "" && len(signature) >= config.MinSignatureLength {
						currentThinkingSignature = signature
						// Cache thinking signature with model family for cross-model compatibility
						modelFamily := config.GetModelFamily(originalModel)
						format.GetGlobalSignatureCache().CacheThinkingSignature(signature, string(modelFamily))
					}

					events <- &SSEEvent{
						Type:  "content_block_delta",
						Index: blockIndex,
						Delta: map[string]interface{}{
							"type":     "thinking_delta",
							"thinking": text,
						},
					}

				} else if part.Text != "" {
					// Handle regular text
					if currentBlockType != "text" {
						if currentBlockType == "thinking" && currentThinkingSignature != "" {
							events <- &SSEEvent{
								Type:  "content_block_delta",
								Index: blockIndex,
								Delta: map[string]interface{}{
									"type":      "signature_delta",
									"signature": currentThinkingSignature,
								},
							}
							currentThinkingSignature = ""
						}
						if currentBlockType != "" {
							events <- &SSEEvent{Type: "content_block_stop", Index: blockIndex}
							blockIndex++
						}
						currentBlockType = "text"
						events <- &SSEEvent{
							Type:  "content_block_start",
							Index: blockIndex,
							ContentBlock: &anthropic.ContentBlock{
								Type: "text",
								Text: "",
							},
						}
					}

					events <- &SSEEvent{
						Type:  "content_block_delta",
						Index: blockIndex,
						Delta: map[string]interface{}{
							"type": "text_delta",
							"text": part.Text,
						},
					}

				} else if part.FunctionCall != nil {
					// Handle tool use
					functionCallSignature := part.ThoughtSignature

					if currentBlockType == "thinking" && currentThinkingSignature != "" {
						events <- &SSEEvent{
							Type:  "content_block_delta",
							Index: blockIndex,
							Delta: map[string]interface{}{
								"type":      "signature_delta",
								"signature": currentThinkingSignature,
							},
						}
						currentThinkingSignature = ""
					}
					if currentBlockType != "" {
						events <- &SSEEvent{Type: "content_block_stop", Index: blockIndex}
						blockIndex++
					}
					currentBlockType = "tool_use"
					stopReason = "tool_use"

					toolID := part.FunctionCall.ID
					if toolID == "" {
						toolID = "toolu_" + generateHexID(12)
					}

					toolUseBlock := &anthropic.ContentBlock{
						Type: "tool_use",
						ID:   toolID,
						Name: part.FunctionCall.Name,
					}

					// Store the signature in the tool_use block for later retrieval
					if functionCallSignature != "" && len(functionCallSignature) >= config.MinSignatureLength {
						toolUseBlock.ThoughtSignature = functionCallSignature
						// Cache for future requests (Claude Code may strip this field)
						format.GetGlobalSignatureCache().CacheSignature(toolID, functionCallSignature)
					}

					events <- &SSEEvent{
						Type:         "content_block_start",
						Index:        blockIndex,
						ContentBlock: toolUseBlock,
					}

					argsJSON, _ := json.Marshal(part.FunctionCall.Args)
					events <- &SSEEvent{
						Type:  "content_block_delta",
						Index: blockIndex,
						Delta: map[string]interface{}{
							"type":         "input_json_delta",
							"partial_json": string(argsJSON),
						},
					}

				} else if part.InlineData != nil {
					// Handle image content
					if currentBlockType == "thinking" && currentThinkingSignature != "" {
						events <- &SSEEvent{
							Type:  "content_block_delta",
							Index: blockIndex,
							Delta: map[string]interface{}{
								"type":      "signature_delta",
								"signature": currentThinkingSignature,
							},
						}
						currentThinkingSignature = ""
					}
					if currentBlockType != "" {
						events <- &SSEEvent{Type: "content_block_stop", Index: blockIndex}
						blockIndex++
					}
					currentBlockType = "image"

					events <- &SSEEvent{
						Type:  "content_block_start",
						Index: blockIndex,
						ContentBlock: &anthropic.ContentBlock{
							Type: "image",
							Source: &anthropic.ImageSource{
								Type:      "base64",
								MediaType: part.InlineData.MimeType,
								Data:      part.InlineData.Data,
							},
						},
					}

					events <- &SSEEvent{Type: "content_block_stop", Index: blockIndex}
					blockIndex++
					currentBlockType = ""
				}
			}

			// Check finish reason
			if firstCandidate.FinishReason != "" && stopReason == "" {
				stopReason = mapFinishReason(firstCandidate.FinishReason)
			}
		}

		if err := scanner.Err(); err != nil {
			errs <- err
			return
		}

		// Handle no content received
		if !hasEmittedStart {
			utils.Warn("[CloudCode] No content parts received, throwing for retry")
			errs <- NewEmptyResponseError("No content parts received from API")
			return
		}

		// Close any open block
		if currentBlockType != "" {
			if currentBlockType == "thinking" && currentThinkingSignature != "" {
				events <- &SSEEvent{
					Type:  "content_block_delta",
					Index: blockIndex,
					Delta: map[string]interface{}{
						"type":      "signature_delta",
						"signature": currentThinkingSignature,
					},
				}
			}
			events <- &SSEEvent{Type: "content_block_stop", Index: blockIndex}
		}

		// Emit message_delta and message_stop
		if stopReason == "" {
			stopReason = "end_turn"
		}

		events <- &SSEEvent{
			Type: "message_delta",
			Delta: map[string]interface{}{
				"stop_reason":   stopReason,
				"stop_sequence": nil,
			},
			Usage: &anthropic.Usage{
				OutputTokens:            outputTokens,
				CacheReadInputTokens:    cacheReadTokens,
				CacheCreationInputTokens: 0,
			},
		}

		events <- &SSEEvent{Type: "message_stop"}
	}()

	return events, errs
}

// mapFinishReason maps Google finish reasons to Anthropic format
func mapFinishReason(reason string) string {
	switch reason {
	case "MAX_TOKENS":
		return "max_tokens"
	case "STOP":
		return "end_turn"
	default:
		return "end_turn"
	}
}

// generateHexID generates a random hex ID using crypto/rand
func generateHexID(length int) string {
	bytes := make([]byte, length)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// max returns the maximum of two ints
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

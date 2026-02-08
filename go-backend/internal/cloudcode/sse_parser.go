// Package cloudcode provides Cloud Code API client implementation.
// This file corresponds to src/cloudcode/sse-parser.js in the Node.js version.
package cloudcode

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/poemonsense/antigravity-proxy-go/internal/format"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/anthropic"
)

// SSEPart represents a part in an SSE response
type SSEPart struct {
	Thought          bool                   `json:"thought,omitempty"`
	Text             string                 `json:"text,omitempty"`
	ThoughtSignature string                 `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall          `json:"functionCall,omitempty"`
	InlineData       *InlineData            `json:"inlineData,omitempty"`
}

// FunctionCall represents a function call in an SSE response
type FunctionCall struct {
	ID   string                 `json:"id,omitempty"`
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// InlineData represents inline data (e.g., images) in an SSE response
type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// UsageMetadata represents usage metadata from SSE response
type UsageMetadata struct {
	PromptTokenCount      int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount  int `json:"candidatesTokenCount,omitempty"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
}

// SSECandidate represents a candidate in an SSE response
type SSECandidate struct {
	Content      *SSEContent `json:"content,omitempty"`
	FinishReason string      `json:"finishReason,omitempty"`
}

// SSEContent represents content in an SSE candidate
type SSEContent struct {
	Parts []SSEPart `json:"parts,omitempty"`
}

// SSEResponse represents a single SSE data payload
type SSEResponse struct {
	Response *SSEInnerResponse `json:"response,omitempty"`
	// Direct fields (when response is not wrapped)
	Candidates    []SSECandidate `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// SSEInnerResponse represents the inner response when wrapped
type SSEInnerResponse struct {
	Candidates    []SSECandidate `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// AccumulatedResponse represents the accumulated response from SSE parsing
type AccumulatedResponse struct {
	Candidates    []AccumulatedCandidate `json:"candidates"`
	UsageMetadata *UsageMetadata         `json:"usageMetadata,omitempty"`
}

// AccumulatedCandidate represents an accumulated candidate
type AccumulatedCandidate struct {
	Content      AccumulatedContent `json:"content"`
	FinishReason string             `json:"finishReason"`
}

// AccumulatedContent represents accumulated content
type AccumulatedContent struct {
	Parts []map[string]interface{} `json:"parts"`
}

// ToGoogleResponse converts AccumulatedResponse to format.GoogleResponse
func (a *AccumulatedResponse) ToGoogleResponse() *format.GoogleResponse {
	// Convert through JSON for reliable type conversion
	data, err := json.Marshal(a)
	if err != nil {
		return &format.GoogleResponse{}
	}
	var result format.GoogleResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return &format.GoogleResponse{}
	}
	return &result
}

// ParseThinkingSSEResponse parses SSE response for thinking models and accumulates all parts
func ParseThinkingSSEResponse(reader io.Reader, originalModel string) (*anthropic.MessagesResponse, error) {
	var accumulatedThinkingText string
	var accumulatedThinkingSignature string
	var accumulatedText string
	finalParts := make([]map[string]interface{}, 0)
	usageMetadata := &UsageMetadata{}
	finishReason := "STOP"

	flushThinking := func() {
		if accumulatedThinkingText != "" {
			part := map[string]interface{}{
				"thought": true,
				"text":    accumulatedThinkingText,
			}
			if accumulatedThinkingSignature != "" {
				part["thoughtSignature"] = accumulatedThinkingSignature
			}
			finalParts = append(finalParts, part)
			accumulatedThinkingText = ""
			accumulatedThinkingSignature = ""
		}
	}

	flushText := func() {
		if accumulatedText != "" {
			finalParts = append(finalParts, map[string]interface{}{"text": accumulatedText})
			accumulatedText = ""
		}
	}

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
			utils.Debug("[CloudCode] SSE parse warning: %v, Raw: %.100s", err, jsonText)
			continue
		}

		// Get inner response (may be wrapped or direct)
		innerResponse := data.Response
		if innerResponse == nil {
			innerResponse = &SSEInnerResponse{
				Candidates:    data.Candidates,
				UsageMetadata: data.UsageMetadata,
			}
		}

		if innerResponse.UsageMetadata != nil {
			usageMetadata = innerResponse.UsageMetadata
		}

		candidates := innerResponse.Candidates
		if len(candidates) == 0 {
			continue
		}

		firstCandidate := candidates[0]
		if firstCandidate.FinishReason != "" {
			finishReason = firstCandidate.FinishReason
		}

		if firstCandidate.Content == nil {
			continue
		}

		for _, part := range firstCandidate.Content.Parts {
			if part.Thought {
				flushText()
				accumulatedThinkingText += part.Text
				if part.ThoughtSignature != "" {
					accumulatedThinkingSignature = part.ThoughtSignature
				}
			} else if part.FunctionCall != nil {
				flushThinking()
				flushText()
				fcPart := map[string]interface{}{
					"functionCall": map[string]interface{}{
						"name": part.FunctionCall.Name,
						"args": part.FunctionCall.Args,
					},
				}
				if part.FunctionCall.ID != "" {
					fcPart["functionCall"].(map[string]interface{})["id"] = part.FunctionCall.ID
				}
				finalParts = append(finalParts, fcPart)
			} else if part.Text != "" {
				flushThinking()
				accumulatedText += part.Text
			} else if part.InlineData != nil {
				// Handle image content
				flushThinking()
				flushText()
				finalParts = append(finalParts, map[string]interface{}{
					"inlineData": map[string]interface{}{
						"mimeType": part.InlineData.MimeType,
						"data":     part.InlineData.Data,
					},
				})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	flushThinking()
	flushText()

	// Build accumulated response
	accumulatedResponse := &AccumulatedResponse{
		Candidates: []AccumulatedCandidate{
			{
				Content:      AccumulatedContent{Parts: finalParts},
				FinishReason: finishReason,
			},
		},
		UsageMetadata: usageMetadata,
	}

	// Log part types
	partTypes := make([]string, 0, len(finalParts))
	for _, p := range finalParts {
		if _, ok := p["thought"]; ok {
			partTypes = append(partTypes, "thought")
		} else if _, ok := p["functionCall"]; ok {
			partTypes = append(partTypes, "functionCall")
		} else if _, ok := p["inlineData"]; ok {
			partTypes = append(partTypes, "inlineData")
		} else {
			partTypes = append(partTypes, "text")
		}
	}
	utils.Debug("[CloudCode] Response received (SSE), part types: %v", partTypes)

	// Check for thinking signature length
	for _, p := range finalParts {
		if thought, ok := p["thought"].(bool); ok && thought {
			if sig, ok := p["thoughtSignature"].(string); ok {
				utils.Debug("[CloudCode] Thinking signature length: %d", len(sig))
			}
			break
		}
	}

	return format.ConvertGoogleToAnthropic(accumulatedResponse.ToGoogleResponse(), originalModel), nil
}

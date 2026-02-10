// Package cloudcode provides Cloud Code API client implementation.
// This file corresponds to src/cloudcode/session-manager.js in the Node.js version.
package cloudcode

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"github.com/poemonsense/antigravity-proxy-go/pkg/anthropic"
)

// DeriveSessionID derives a stable session ID from the first user message.
// This ensures the same conversation uses the same session ID across turns,
// enabling prompt caching (cache is scoped to session + organization).
func DeriveSessionID(request *anthropic.MessagesRequest) string {
	for _, msg := range request.Messages {
		if msg.Role == "user" {
			content := extractTextContent(msg)
			if content != "" {
				hash := sha256.Sum256([]byte(content))
				return hex.EncodeToString(hash[:16]) // First 32 hex chars
			}
		}
	}

	// Fallback to random UUID if no user message found
	return uuid.New().String()
}

// extractTextContent extracts text content from a message
func extractTextContent(msg anthropic.Message) string {
	var result string
	for _, block := range msg.Content {
		if block.Type == "text" && block.Text != "" {
			if result != "" {
				result += "\n"
			}
			result += block.Text
		}
	}
	return result
}

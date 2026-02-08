// Package format provides conversion between Anthropic and Google Generative AI formats.
// This file corresponds to src/format/index.js in the Node.js version.
//
// The format package handles all format conversion between:
// - Anthropic Messages API format (used by Claude Code CLI)
// - Google Generative AI format (used by Cloud Code API)
//
// Key components:
//
// Request Conversion:
//   - ConvertAnthropicToGoogle: Main entry point for request conversion
//   - Handles system prompts, messages, tools, and thinking configuration
//   - Applies cache_control cleaning, thinking recovery, and schema sanitization
//
// Response Conversion:
//   - ConvertGoogleToAnthropic: Main entry point for response conversion
//   - Converts candidates, parts, function calls, and usage metadata
//   - Caches thinking signatures for cross-model compatibility
//
// Thinking Utilities:
//   - CleanCacheControl: Removes cache_control fields from messages
//   - RestoreThinkingSignatures: Restores signatures from cache
//   - ReorderAssistantContent: Reorders content blocks for API requirements
//   - CloseToolLoopForThinking: Injects synthetic messages for thinking recovery
//
// Schema Sanitization:
//   - SanitizeSchema: Removes unsupported JSON Schema features
//   - CleanSchema: Multi-phase pipeline for Gemini compatibility
//
// Signature Cache:
//   - SignatureCache: Caches thoughtSignatures for tool calls and thinking blocks
//   - Uses Redis for persistence, falls back to in-memory cache
package format

import (
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// Initialize sets up the format package with required dependencies
func Initialize(redisClient *redis.Client) {
	InitGlobalSignatureCache(redisClient)
}

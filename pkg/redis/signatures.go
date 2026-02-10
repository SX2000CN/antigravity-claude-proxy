// Package redis provides Redis operations for signature caching.
// This file corresponds to src/format/signature-cache.js in the Node.js version.
package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// SignatureStore provides signature caching operations
type SignatureStore struct {
	client *Client
}

// NewSignatureStore creates a new SignatureStore
func NewSignatureStore(client *Client) *SignatureStore {
	return &SignatureStore{client: client}
}

// Default TTL for signatures (2 hours)
const SignatureTTL = 2 * time.Hour

// ThinkingSignatureInfo represents cached thinking signature metadata
type ThinkingSignatureInfo struct {
	ModelFamily string    `json:"modelFamily"` // "claude" or "gemini"
	Timestamp   time.Time `json:"timestamp"`
}

// ============================================================
// Tool Use Signature Operations
// ============================================================

// GetToolSignature retrieves a cached thoughtSignature for a tool use ID
func (s *SignatureStore) GetToolSignature(ctx context.Context, toolUseID string) (string, error) {
	key := PrefixSignatureTool + toolUseID
	sig, err := s.client.GetString(ctx, key)
	if err != nil {
		if IsNil(err) {
			return "", nil
		}
		return "", err
	}
	return sig, nil
}

// SetToolSignature caches a thoughtSignature for a tool use ID
func (s *SignatureStore) SetToolSignature(ctx context.Context, toolUseID, signature string) error {
	key := PrefixSignatureTool + toolUseID
	return s.client.SetString(ctx, key, signature, SignatureTTL)
}

// ClearToolSignature removes a cached tool signature
func (s *SignatureStore) ClearToolSignature(ctx context.Context, toolUseID string) error {
	key := PrefixSignatureTool + toolUseID
	return s.client.Delete(ctx, key)
}

// ============================================================
// Thinking Signature Operations
// ============================================================

// GetThinkingSignatureFamily retrieves the model family for a thinking signature
func (s *SignatureStore) GetThinkingSignatureFamily(ctx context.Context, signature string) (string, error) {
	hash := hashSignature(signature)
	key := PrefixSignatureThinking + hash

	data, err := s.client.HGetAll(ctx, key)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}

	if family, ok := data["modelFamily"]; ok {
		return family, nil
	}

	return "", nil
}

// SetThinkingSignature caches a thinking signature with its model family
func (s *SignatureStore) SetThinkingSignature(ctx context.Context, signature, modelFamily string) error {
	hash := hashSignature(signature)
	key := PrefixSignatureThinking + hash

	values := map[string]interface{}{
		"modelFamily": modelFamily,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	if err := s.client.HSet(ctx, key, values); err != nil {
		return err
	}

	return s.client.Expire(ctx, key, SignatureTTL)
}

// IsThinkingSignatureKnown checks if a thinking signature is cached
func (s *SignatureStore) IsThinkingSignatureKnown(ctx context.Context, signature string) (bool, error) {
	hash := hashSignature(signature)
	key := PrefixSignatureThinking + hash
	return s.client.Exists(ctx, key)
}

// ClearThinkingSignature removes a cached thinking signature
func (s *SignatureStore) ClearThinkingSignature(ctx context.Context, signature string) error {
	hash := hashSignature(signature)
	key := PrefixSignatureThinking + hash
	return s.client.Delete(ctx, key)
}

// ============================================================
// Batch Operations
// ============================================================

// ClearAllSignatures clears all cached signatures
func (s *SignatureStore) ClearAllSignatures(ctx context.Context) error {
	// Clear tool signatures
	toolKeys, err := s.client.ScanAll(ctx, PrefixSignatureTool+"*")
	if err != nil {
		return err
	}
	if len(toolKeys) > 0 {
		if err := s.client.Delete(ctx, toolKeys...); err != nil {
			return err
		}
	}

	// Clear thinking signatures
	thinkingKeys, err := s.client.ScanAll(ctx, PrefixSignatureThinking+"*")
	if err != nil {
		return err
	}
	if len(thinkingKeys) > 0 {
		if err := s.client.Delete(ctx, thinkingKeys...); err != nil {
			return err
		}
	}

	return nil
}

// GetSignatureStats returns statistics about cached signatures
func (s *SignatureStore) GetSignatureStats(ctx context.Context) (map[string]int64, error) {
	stats := make(map[string]int64)

	// Count tool signatures
	toolKeys, err := s.client.ScanAll(ctx, PrefixSignatureTool+"*")
	if err != nil {
		return nil, err
	}
	stats["tool"] = int64(len(toolKeys))

	// Count thinking signatures
	thinkingKeys, err := s.client.ScanAll(ctx, PrefixSignatureThinking+"*")
	if err != nil {
		return nil, err
	}
	stats["thinking"] = int64(len(thinkingKeys))
	stats["total"] = stats["tool"] + stats["thinking"]

	return stats, nil
}

// ============================================================
// Helper Functions
// ============================================================

// hashSignature creates a SHA256 hash of a signature for use as a key
func hashSignature(signature string) string {
	hash := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(hash[:])
}

// MinSignatureLength is the minimum valid signature length
const MinSignatureLength = 50

// IsValidSignature checks if a signature meets minimum length requirements
func IsValidSignature(signature string) bool {
	return len(signature) >= MinSignatureLength
}

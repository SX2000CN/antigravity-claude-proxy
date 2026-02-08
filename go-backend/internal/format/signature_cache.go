// Package format provides conversion between Anthropic and Google Generative AI formats.
// This file corresponds to src/format/signature-cache.js in the Node.js version.
package format

import (
	"context"
	"sync"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// SignatureCache caches Gemini thoughtSignatures for tool calls and thinking blocks.
// Gemini models require thoughtSignature on tool calls, but Claude Code strips non-standard fields.
// This cache stores signatures so they can be restored in subsequent requests.
//
// For the Go version, we use Redis for persistence instead of in-memory Map.
// Fallback to in-memory cache when Redis is unavailable.
type SignatureCache struct {
	mu           sync.RWMutex
	redisClient  *redis.Client
	useRedis     bool
	memoryCache  map[string]*signatureEntry
	thinkingCache map[string]*thinkingEntry
}

type signatureEntry struct {
	Signature string
	Timestamp time.Time
}

type thinkingEntry struct {
	ModelFamily string
	Timestamp   time.Time
}

// NewSignatureCache creates a new SignatureCache
func NewSignatureCache(redisClient *redis.Client) *SignatureCache {
	cache := &SignatureCache{
		redisClient:   redisClient,
		useRedis:      redisClient != nil,
		memoryCache:   make(map[string]*signatureEntry),
		thinkingCache: make(map[string]*thinkingEntry),
	}
	return cache
}

// CacheSignature stores a signature for a tool_use_id
func (c *SignatureCache) CacheSignature(toolUseID, signature string) {
	if toolUseID == "" || signature == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.useRedis {
		ctx := context.Background()
		ttl := time.Duration(config.GeminiSignatureCacheTTLMs) * time.Millisecond
		_ = c.redisClient.SetSignature(ctx, toolUseID, signature, ttl)
	} else {
		c.memoryCache[toolUseID] = &signatureEntry{
			Signature: signature,
			Timestamp: time.Now(),
		}
	}
}

// GetCachedSignature retrieves a cached signature for a tool_use_id
func (c *SignatureCache) GetCachedSignature(toolUseID string) string {
	if toolUseID == "" {
		return ""
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.useRedis {
		ctx := context.Background()
		signature, err := c.redisClient.GetSignature(ctx, toolUseID)
		if err != nil || signature == "" {
			return ""
		}
		return signature
	}

	// Memory cache fallback
	entry, ok := c.memoryCache[toolUseID]
	if !ok {
		return ""
	}

	// Check TTL
	ttl := time.Duration(config.GeminiSignatureCacheTTLMs) * time.Millisecond
	if time.Since(entry.Timestamp) > ttl {
		delete(c.memoryCache, toolUseID)
		return ""
	}

	return entry.Signature
}

// CacheThinkingSignature caches a thinking block signature with its model family
func (c *SignatureCache) CacheThinkingSignature(signature, modelFamily string) {
	if signature == "" || len(signature) < config.MinSignatureLength {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.useRedis {
		ctx := context.Background()
		ttl := time.Duration(config.GeminiSignatureCacheTTLMs) * time.Millisecond
		_ = c.redisClient.SetThinkingSignature(ctx, signature, modelFamily, ttl)
	} else {
		c.thinkingCache[signature] = &thinkingEntry{
			ModelFamily: modelFamily,
			Timestamp:   time.Now(),
		}
	}
}

// GetCachedSignatureFamily returns the cached model family for a thinking signature
func (c *SignatureCache) GetCachedSignatureFamily(signature string) string {
	if signature == "" {
		return ""
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.useRedis {
		ctx := context.Background()
		family, err := c.redisClient.GetThinkingSignature(ctx, signature)
		if err != nil || family == "" {
			return ""
		}
		return family
	}

	// Memory cache fallback
	entry, ok := c.thinkingCache[signature]
	if !ok {
		return ""
	}

	// Check TTL
	ttl := time.Duration(config.GeminiSignatureCacheTTLMs) * time.Millisecond
	if time.Since(entry.Timestamp) > ttl {
		delete(c.thinkingCache, signature)
		return ""
	}

	return entry.ModelFamily
}

// ClearThinkingSignatureCache clears all entries from the thinking signature cache
func (c *SignatureCache) ClearThinkingSignatureCache() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.useRedis {
		// Redis entries will auto-expire via TTL
		// For testing, we clear the memory cache
	}

	c.thinkingCache = make(map[string]*thinkingEntry)
}

// Global instance for convenience
var globalSignatureCache *SignatureCache
var signatureCacheOnce sync.Once

// InitGlobalSignatureCache initializes the global signature cache
func InitGlobalSignatureCache(redisClient *redis.Client) {
	signatureCacheOnce.Do(func() {
		globalSignatureCache = NewSignatureCache(redisClient)
	})
}

// GetGlobalSignatureCache returns the global signature cache instance
func GetGlobalSignatureCache() *SignatureCache {
	if globalSignatureCache == nil {
		// Fallback to memory-only cache if not initialized
		globalSignatureCache = NewSignatureCache(nil)
	}
	return globalSignatureCache
}

// ClearThinkingSignatureCache clears the global thinking signature cache
func ClearThinkingSignatureCache() {
	GetGlobalSignatureCache().ClearThinkingSignatureCache()
}

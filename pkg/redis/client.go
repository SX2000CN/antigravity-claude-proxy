// Package redis provides Redis client wrapper and domain-specific operations.
// This file corresponds to the storage layer that replaces JSON file persistence.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Key prefixes for Redis data
const (
	PrefixAccounts         = "antigravity:accounts:"
	PrefixAccountIndex     = "antigravity:accounts:index"
	PrefixRateLimits       = "antigravity:ratelimits:"
	PrefixQuotas           = "antigravity:quotas:"
	PrefixHealth           = "antigravity:health:"
	PrefixTokens           = "antigravity:tokens:"
	PrefixSignatureTool    = "antigravity:signatures:tool:"
	PrefixSignatureThinking = "antigravity:signatures:thinking:"
	PrefixStats            = "antigravity:stats:"
	PrefixConfig           = "antigravity:config"
	PrefixTokenCache       = "antigravity:token_cache:"
	PrefixProjectCache     = "antigravity:project_cache:"
	PrefixOAuth            = "antigravity:oauth:"
	KeyActiveIndex         = "antigravity:active_index"
)

// Client wraps the Redis client with domain-specific operations
type Client struct {
	rdb *redis.Client
}

// Config represents Redis connection configuration
type Config struct {
	Addr     string
	Password string
	DB       int
}

// NewClient creates a new Redis client
func NewClient(cfg Config) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

// Close closes the Redis connection
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Ping checks the Redis connection
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Raw returns the underlying Redis client for advanced operations
func (c *Client) Raw() *redis.Client {
	return c.rdb
}

// ============================================================
// Generic Operations
// ============================================================

// Set stores a value with optional TTL
func (c *Client) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, key, data, ttl).Err()
}

// Get retrieves a value and unmarshals it
func (c *Client) Get(ctx context.Context, key string, dest interface{}) error {
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// Delete removes a key
func (c *Client) Delete(ctx context.Context, keys ...string) error {
	return c.rdb.Del(ctx, keys...).Err()
}

// Exists checks if a key exists
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	count, err := c.rdb.Exists(ctx, key).Result()
	return count > 0, err
}

// SetNX sets a value only if it doesn't exist
func (c *Client) SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	return c.rdb.SetNX(ctx, key, data, ttl).Result()
}

// Expire sets a TTL on a key
func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// ============================================================
// Hash Operations (for structured data)
// ============================================================

// HSet sets fields in a hash
func (c *Client) HSet(ctx context.Context, key string, values map[string]interface{}) error {
	args := make([]interface{}, 0, len(values)*2)
	for k, v := range values {
		args = append(args, k)
		switch val := v.(type) {
		case string:
			args = append(args, val)
		default:
			data, err := json.Marshal(v)
			if err != nil {
				return err
			}
			args = append(args, string(data))
		}
	}
	return c.rdb.HSet(ctx, key, args...).Err()
}

// HGet retrieves a single field from a hash
func (c *Client) HGet(ctx context.Context, key, field string) (string, error) {
	return c.rdb.HGet(ctx, key, field).Result()
}

// HGetAll retrieves all fields from a hash
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return c.rdb.HGetAll(ctx, key).Result()
}

// HDel deletes fields from a hash
func (c *Client) HDel(ctx context.Context, key string, fields ...string) error {
	return c.rdb.HDel(ctx, key, fields...).Err()
}

// HIncrBy increments a hash field by an integer
func (c *Client) HIncrBy(ctx context.Context, key, field string, incr int64) (int64, error) {
	return c.rdb.HIncrBy(ctx, key, field, incr).Result()
}

// HIncrByFloat increments a hash field by a float
func (c *Client) HIncrByFloat(ctx context.Context, key, field string, incr float64) (float64, error) {
	return c.rdb.HIncrByFloat(ctx, key, field, incr).Result()
}

// ============================================================
// Set Operations (for indexes)
// ============================================================

// SAdd adds members to a set
func (c *Client) SAdd(ctx context.Context, key string, members ...interface{}) error {
	return c.rdb.SAdd(ctx, key, members...).Err()
}

// SRem removes members from a set
func (c *Client) SRem(ctx context.Context, key string, members ...interface{}) error {
	return c.rdb.SRem(ctx, key, members...).Err()
}

// SMembers returns all members of a set
func (c *Client) SMembers(ctx context.Context, key string) ([]string, error) {
	return c.rdb.SMembers(ctx, key).Result()
}

// SIsMember checks if a value is a member of a set
func (c *Client) SIsMember(ctx context.Context, key string, member interface{}) (bool, error) {
	return c.rdb.SIsMember(ctx, key, member).Result()
}

// SCard returns the number of members in a set
func (c *Client) SCard(ctx context.Context, key string) (int64, error) {
	return c.rdb.SCard(ctx, key).Result()
}

// ============================================================
// String Operations
// ============================================================

// SetString stores a plain string
func (c *Client) SetString(ctx context.Context, key, value string, ttl time.Duration) error {
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

// GetString retrieves a plain string
func (c *Client) GetString(ctx context.Context, key string) (string, error) {
	return c.rdb.Get(ctx, key).Result()
}

// Incr increments an integer value
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	return c.rdb.Incr(ctx, key).Result()
}

// IncrBy increments an integer value by a specific amount
func (c *Client) IncrBy(ctx context.Context, key string, value int64) (int64, error) {
	return c.rdb.IncrBy(ctx, key, value).Result()
}

// ============================================================
// List Operations
// ============================================================

// LPush prepends values to a list
func (c *Client) LPush(ctx context.Context, key string, values ...interface{}) error {
	return c.rdb.LPush(ctx, key, values...).Err()
}

// RPush appends values to a list
func (c *Client) RPush(ctx context.Context, key string, values ...interface{}) error {
	return c.rdb.RPush(ctx, key, values...).Err()
}

// LRange returns a range of elements from a list
func (c *Client) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return c.rdb.LRange(ctx, key, start, stop).Result()
}

// LTrim trims a list to the specified range
func (c *Client) LTrim(ctx context.Context, key string, start, stop int64) error {
	return c.rdb.LTrim(ctx, key, start, stop).Err()
}

// LLen returns the length of a list
func (c *Client) LLen(ctx context.Context, key string) (int64, error) {
	return c.rdb.LLen(ctx, key).Result()
}

// ============================================================
// Key Pattern Operations
// ============================================================

// Keys returns all keys matching a pattern (use with caution in production)
func (c *Client) Keys(ctx context.Context, pattern string) ([]string, error) {
	return c.rdb.Keys(ctx, pattern).Result()
}

// Scan iterates through keys matching a pattern
func (c *Client) Scan(ctx context.Context, cursor uint64, pattern string, count int64) ([]string, uint64, error) {
	return c.rdb.Scan(ctx, cursor, pattern, count).Result()
}

// ScanAll returns all keys matching a pattern using SCAN
func (c *Client) ScanAll(ctx context.Context, pattern string) ([]string, error) {
	var cursor uint64
	var keys []string

	for {
		var batch []string
		var err error
		batch, cursor, err = c.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		if cursor == 0 {
			break
		}
	}

	return keys, nil
}

// ============================================================
// Transaction Operations
// ============================================================

// Watch starts a transaction with WATCH
func (c *Client) Watch(ctx context.Context, fn func(*redis.Tx) error, keys ...string) error {
	return c.rdb.Watch(ctx, fn, keys...)
}

// Pipeline creates a new pipeline
func (c *Client) Pipeline() redis.Pipeliner {
	return c.rdb.Pipeline()
}

// TxPipeline creates a new transactional pipeline
func (c *Client) TxPipeline() redis.Pipeliner {
	return c.rdb.TxPipeline()
}

// IsNil checks if an error is redis.Nil (key not found)
func IsNil(err error) bool {
	return err == redis.Nil
}

// ============================================================
// Signature Cache Operations (convenience methods)
// ============================================================

// SetSignature stores a tool signature with TTL
func (c *Client) SetSignature(ctx context.Context, toolUseID, signature string, ttl time.Duration) error {
	key := PrefixSignatureTool + toolUseID
	return c.rdb.Set(ctx, key, signature, ttl).Err()
}

// GetSignature retrieves a tool signature
func (c *Client) GetSignature(ctx context.Context, toolUseID string) (string, error) {
	key := PrefixSignatureTool + toolUseID
	result, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return result, err
}

// SetThinkingSignature stores a thinking signature with model family
func (c *Client) SetThinkingSignature(ctx context.Context, signatureHash, modelFamily string, ttl time.Duration) error {
	key := PrefixSignatureThinking + signatureHash
	values := map[string]interface{}{
		"modelFamily": modelFamily,
		"timestamp":   time.Now().Format(time.RFC3339),
	}
	if err := c.HSet(ctx, key, values); err != nil {
		return err
	}
	return c.Expire(ctx, key, ttl)
}

// GetThinkingSignature retrieves the model family for a thinking signature
func (c *Client) GetThinkingSignature(ctx context.Context, signatureHash string) (string, error) {
	key := PrefixSignatureThinking + signatureHash
	data, err := c.HGetAll(ctx, key)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	return data["modelFamily"], nil
}

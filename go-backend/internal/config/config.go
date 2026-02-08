// Package config provides runtime configuration management.
// This file corresponds to src/config.js in the Node.js version.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// HealthScoreConfig configures the health scoring for hybrid strategy
type HealthScoreConfig struct {
	Initial          float64 `json:"initial"`
	SuccessReward    float64 `json:"successReward"`
	RateLimitPenalty float64 `json:"rateLimitPenalty"`
	FailurePenalty   float64 `json:"failurePenalty"`
	RecoveryPerHour  float64 `json:"recoveryPerHour"`
	MinUsable        float64 `json:"minUsable"`
	MaxScore         float64 `json:"maxScore"`
}

// TokenBucketConfig configures the token bucket for hybrid strategy
type TokenBucketConfig struct {
	MaxTokens       float64 `json:"maxTokens"`
	TokensPerMinute float64 `json:"tokensPerMinute"`
	InitialTokens   float64 `json:"initialTokens"`
}

// QuotaConfig configures quota thresholds for hybrid strategy
type QuotaConfig struct {
	LowThreshold      float64 `json:"lowThreshold"`
	CriticalThreshold float64 `json:"criticalThreshold"`
	StaleMs           int64   `json:"staleMs"`
	UnknownScore      float64 `json:"unknownScore"`
}

// AccountSelectionConfig configures account selection behavior
type AccountSelectionConfig struct {
	Strategy    string             `json:"strategy"`
	HealthScore *HealthScoreConfig `json:"healthScore,omitempty"`
	TokenBucket *TokenBucketConfig `json:"tokenBucket,omitempty"`
	Quota       *QuotaConfig       `json:"quota,omitempty"`
}

// Config represents the runtime configuration
type Config struct {
	mu sync.RWMutex

	// API access
	APIKey        string `json:"apiKey"`
	WebUIPassword string `json:"webuiPassword"`

	// Logging and debugging
	Debug    bool   `json:"debug"`
	DevMode  bool   `json:"devMode"`
	LogLevel string `json:"logLevel"`

	// Retry configuration
	MaxRetries   int   `json:"maxRetries"`
	RetryBaseMs  int64 `json:"retryBaseMs"`
	RetryMaxMs   int64 `json:"retryMaxMs"`

	// Token handling
	PersistTokenCache bool `json:"persistTokenCache"`

	// Cooldown configuration
	DefaultCooldownMs    int64 `json:"defaultCooldownMs"`
	MaxWaitBeforeErrorMs int64 `json:"maxWaitBeforeErrorMs"`

	// Account limits
	MaxAccounts          int     `json:"maxAccounts"`
	GlobalQuotaThreshold float64 `json:"globalQuotaThreshold"`

	// Rate limit handling
	RateLimitDedupWindowMs int64 `json:"rateLimitDedupWindowMs"`
	MaxConsecutiveFailures int   `json:"maxConsecutiveFailures"`
	ExtendedCooldownMs     int64 `json:"extendedCooldownMs"`
	MaxCapacityRetries     int   `json:"maxCapacityRetries"`

	// Model mapping (for hiding/aliasing models)
	ModelMapping map[string]string `json:"modelMapping"`

	// Account selection strategy
	AccountSelection AccountSelectionConfig `json:"accountSelection"`

	// Redis configuration
	RedisAddr     string `json:"redisAddr"`
	RedisPassword string `json:"redisPassword"`
	RedisDB       int    `json:"redisDB"`

	// Server configuration
	Port int    `json:"port"`
	Host string `json:"host"`

	// Fallback configuration
	FallbackEnabled bool `json:"fallbackEnabled"`
}

// DefaultConfig returns a new Config with default values
func DefaultConfig() *Config {
	return &Config{
		APIKey:        "",
		WebUIPassword: "",
		Debug:         false,
		DevMode:       false,
		LogLevel:      "info",
		MaxRetries:    5,
		RetryBaseMs:   1000,
		RetryMaxMs:    30000,
		PersistTokenCache:    false,
		DefaultCooldownMs:    10000,  // 10 seconds
		MaxWaitBeforeErrorMs: 120000, // 2 minutes
		MaxAccounts:          10,
		GlobalQuotaThreshold: 0, // 0 = disabled
		RateLimitDedupWindowMs: 2000,
		MaxConsecutiveFailures: 3,
		ExtendedCooldownMs:     60000, // 1 minute
		MaxCapacityRetries:     5,
		ModelMapping:           make(map[string]string),
		AccountSelection: AccountSelectionConfig{
			Strategy: "hybrid",
			HealthScore: &HealthScoreConfig{
				Initial:          70,
				SuccessReward:    1,
				RateLimitPenalty: -10,
				FailurePenalty:   -20,
				RecoveryPerHour:  2,
				MinUsable:        50,
				MaxScore:         100,
			},
			TokenBucket: &TokenBucketConfig{
				MaxTokens:       50,
				TokensPerMinute: 6,
				InitialTokens:   50,
			},
			Quota: &QuotaConfig{
				LowThreshold:      0.10,
				CriticalThreshold: 0.05,
				StaleMs:           300000, // 5 minutes
			},
		},
		RedisAddr:       "localhost:6379",
		RedisPassword:   "",
		RedisDB:         0,
		Port:            8080,
		Host:            "0.0.0.0",
		FallbackEnabled: false,
	}
}

// Config paths
var (
	configDir  string
	configFile string
)

func init() {
	home := utils.GetHomeDir()
	configDir = filepath.Join(home, ".config", "antigravity-proxy")
	configFile = filepath.Join(configDir, "config.json")
}

// Global config instance
var (
	globalConfig     *Config
	globalConfigOnce sync.Once
)

// GetConfig returns the global config instance
func GetConfig() *Config {
	globalConfigOnce.Do(func() {
		globalConfig = DefaultConfig()
		globalConfig.Load()
	})
	return globalConfig
}

// Load loads configuration from file and environment
func (c *Config) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Ensure config directory exists
	if err := utils.EnsureDir(configDir); err != nil {
		utils.Warn("Failed to create config directory: %v", err)
	}

	// Try to load from config file
	if utils.FileExists(configFile) {
		if err := c.loadFromFile(configFile); err != nil {
			utils.Warn("Failed to load config from %s: %v", configFile, err)
		}
	} else {
		// Fallback to local config.json
		localConfig := filepath.Join(".", "config.json")
		if utils.FileExists(localConfig) {
			if err := c.loadFromFile(localConfig); err != nil {
				utils.Warn("Failed to load local config: %v", err)
			}
		}
	}

	// Environment overrides
	c.loadFromEnv()

	// Backward compatibility: debug implies devMode
	if c.Debug && !c.DevMode {
		c.DevMode = true
	}

	// Update logger debug mode
	utils.SetDebug(c.Debug || c.DevMode)

	return nil
}

// loadFromFile loads config from a JSON file
func (c *Config) loadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Create a temporary config with defaults
	tempConfig := DefaultConfig()

	// Unmarshal into temp config (preserves defaults for missing fields)
	if err := json.Unmarshal(data, tempConfig); err != nil {
		return err
	}

	// Copy values from temp config
	c.APIKey = tempConfig.APIKey
	c.WebUIPassword = tempConfig.WebUIPassword
	c.Debug = tempConfig.Debug
	c.DevMode = tempConfig.DevMode
	c.LogLevel = tempConfig.LogLevel
	c.MaxRetries = tempConfig.MaxRetries
	c.RetryBaseMs = tempConfig.RetryBaseMs
	c.RetryMaxMs = tempConfig.RetryMaxMs
	c.PersistTokenCache = tempConfig.PersistTokenCache
	c.DefaultCooldownMs = tempConfig.DefaultCooldownMs
	c.MaxWaitBeforeErrorMs = tempConfig.MaxWaitBeforeErrorMs
	c.MaxAccounts = tempConfig.MaxAccounts
	c.GlobalQuotaThreshold = tempConfig.GlobalQuotaThreshold
	c.RateLimitDedupWindowMs = tempConfig.RateLimitDedupWindowMs
	c.MaxConsecutiveFailures = tempConfig.MaxConsecutiveFailures
	c.ExtendedCooldownMs = tempConfig.ExtendedCooldownMs
	c.MaxCapacityRetries = tempConfig.MaxCapacityRetries
	c.ModelMapping = tempConfig.ModelMapping
	c.AccountSelection = tempConfig.AccountSelection
	c.RedisAddr = tempConfig.RedisAddr
	c.RedisPassword = tempConfig.RedisPassword
	c.RedisDB = tempConfig.RedisDB
	c.Port = tempConfig.Port
	c.Host = tempConfig.Host
	c.FallbackEnabled = tempConfig.FallbackEnabled

	return nil
}

// loadFromEnv loads config from environment variables
func (c *Config) loadFromEnv() {
	if v := os.Getenv("API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("WEBUI_PASSWORD"); v != "" {
		c.WebUIPassword = v
	}
	if os.Getenv("DEBUG") == "true" {
		c.Debug = true
	}
	if os.Getenv("DEV_MODE") == "true" {
		c.DevMode = true
	}
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		c.RedisAddr = v
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		c.RedisPassword = v
	}
	if os.Getenv("FALLBACK") == "true" {
		c.FallbackEnabled = true
	}
}

// Save saves the current configuration to disk
func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if err := utils.EnsureDir(configDir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configFile, data, 0644)
}

// Update applies updates to the configuration and saves
func (c *Config) Update(updates map[string]interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Apply updates
	for key, value := range updates {
		switch key {
		case "apiKey":
			if v, ok := value.(string); ok {
				c.APIKey = v
			}
		case "webuiPassword":
			if v, ok := value.(string); ok {
				c.WebUIPassword = v
			}
		case "debug":
			if v, ok := value.(bool); ok {
				c.Debug = v
			}
		case "devMode":
			if v, ok := value.(bool); ok {
				c.DevMode = v
			}
		case "globalQuotaThreshold":
			if v, ok := value.(float64); ok {
				c.GlobalQuotaThreshold = v
			}
		case "maxAccounts":
			if v, ok := value.(float64); ok {
				c.MaxAccounts = int(v)
			}
		case "fallbackEnabled":
			if v, ok := value.(bool); ok {
				c.FallbackEnabled = v
			}
		}
	}

	// Update logger
	utils.SetDebug(c.Debug || c.DevMode)

	// Save to disk
	if err := utils.EnsureDir(configDir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configFile, data, 0644)
}

// GetPublic returns a copy of the config with sensitive fields redacted
func (c *Config) GetPublic() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Create a copy
	result := map[string]interface{}{
		"apiKey":                 redact(c.APIKey),
		"webuiPassword":          redact(c.WebUIPassword),
		"debug":                  c.Debug,
		"devMode":                c.DevMode,
		"logLevel":               c.LogLevel,
		"maxRetries":             c.MaxRetries,
		"retryBaseMs":            c.RetryBaseMs,
		"retryMaxMs":             c.RetryMaxMs,
		"persistTokenCache":      c.PersistTokenCache,
		"defaultCooldownMs":      c.DefaultCooldownMs,
		"maxWaitBeforeErrorMs":   c.MaxWaitBeforeErrorMs,
		"maxAccounts":            c.MaxAccounts,
		"globalQuotaThreshold":   c.GlobalQuotaThreshold,
		"rateLimitDedupWindowMs": c.RateLimitDedupWindowMs,
		"maxConsecutiveFailures": c.MaxConsecutiveFailures,
		"extendedCooldownMs":     c.ExtendedCooldownMs,
		"maxCapacityRetries":     c.MaxCapacityRetries,
		"modelMapping":           c.ModelMapping,
		"accountSelection":       c.AccountSelection,
		"redisAddr":              c.RedisAddr,
		"redisPassword":          redact(c.RedisPassword),
		"redisDB":                c.RedisDB,
		"port":                   c.Port,
		"host":                   c.Host,
		"fallbackEnabled":        c.FallbackEnabled,
	}

	return result
}

// GetStrategy returns the current account selection strategy
func (c *Config) GetStrategy() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AccountSelection.Strategy
}

// SetStrategy updates the account selection strategy
func (c *Config) SetStrategy(strategy string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AccountSelection.Strategy = strategy
}

// IsDevMode returns whether dev mode is enabled
func (c *Config) IsDevMode() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DevMode
}

// redact returns "********" if the string is non-empty, otherwise empty string
func redact(s string) string {
	if s == "" {
		return ""
	}
	return "********"
}

// Convenience functions

// GetPort returns the server port from global config
func GetPort() int {
	return GetConfig().Port
}

// GetHost returns the server host from global config
func GetHost() string {
	return GetConfig().Host
}

// IsDebug returns whether debug mode is enabled
func IsDebug() bool {
	cfg := GetConfig()
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	return cfg.Debug
}

// IsDevModeEnabled returns whether dev mode is enabled
func IsDevModeEnabled() bool {
	return GetConfig().IsDevMode()
}

// GetGlobalQuotaThreshold returns the global quota threshold
func GetGlobalQuotaThreshold() float64 {
	cfg := GetConfig()
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	return cfg.GlobalQuotaThreshold
}

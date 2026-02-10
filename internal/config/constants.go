// Package config provides configuration constants and runtime configuration management.
// This file corresponds to src/constants.js in the Node.js version.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Version information
const Version = "1.0.0"

// Cloud Code API endpoints (in fallback order)
const (
	AntigravityEndpointDaily = "https://daily-cloudcode-pa.googleapis.com"
	AntigravityEndpointProd  = "https://cloudcode-pa.googleapis.com"
)

// AntigravityEndpointFallbacks is the endpoint fallback order (daily → prod)
var AntigravityEndpointFallbacks = []string{
	AntigravityEndpointDaily,
	AntigravityEndpointProd,
}

// LoadCodeAssistEndpoints is the endpoint order for loadCodeAssist (prod first)
// loadCodeAssist works better on prod for fresh/unprovisioned accounts
var LoadCodeAssistEndpoints = []string{
	AntigravityEndpointProd,
	AntigravityEndpointDaily,
}

// OnboardUserEndpoints is the endpoint order for onboardUser (same as generateContent fallbacks)
var OnboardUserEndpoints = AntigravityEndpointFallbacks

// DefaultProjectID is the default project ID if none can be discovered
const DefaultProjectID = "rising-fact-p41fc"

// AntigravityHeaders are the required headers for Antigravity API requests
func AntigravityHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      getPlatformUserAgent(),
		"X-Goog-Api-Client": "google-cloud-sdk vscode_cloudshelleditor/0.1",
		"Client-Metadata": getClientMetadata(),
	}
}

// LoadCodeAssistHeaders are the headers for loadCodeAssist API
func LoadCodeAssistHeaders() map[string]string {
	return AntigravityHeaders()
}

// Exported OAuth constants for easy access
var (
	OAuthClientID              = OAuthConfig.ClientID
	OAuthClientSecret          = OAuthConfig.ClientSecret
	OAuthAuthURL               = OAuthConfig.AuthURL
	OAuthTokenURL              = OAuthConfig.TokenURL
	OAuthUserInfoURL           = OAuthConfig.UserInfoURL
	OAuthCallbackPort          = OAuthConfig.CallbackPort
	OAuthCallbackFallbackPorts = OAuthConfig.CallbackFallbackPorts
	OAuthScopes                = OAuthConfig.Scopes
)

// getPlatformUserAgent generates platform-specific User-Agent string
func getPlatformUserAgent() string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("antigravity/1.16.5 %s/%s", os, arch)
}

// IDE Type enum (numeric values as expected by Cloud Code API)
// Reference: Antigravity binary analysis - google.internal.cloud.code.v1internal.ClientMetadata.IdeType
const (
	IdeTypeUnspecified = 0
	IdeTypeJetski      = 5 // Internal codename for Gemini CLI
	IdeTypeAntigravity = 6
	IdeTypePlugins     = 7
)

// Platform enum
// Reference: Antigravity binary analysis - google.internal.cloud.code.v1internal.ClientMetadata.Platform
const (
	PlatformUnspecified = 0
	PlatformWindows     = 1
	PlatformLinux       = 2
	PlatformMacOS       = 3
)

// Plugin Type enum
const (
	PluginTypeUnspecified = 0
	PluginTypeDuetAI      = 1
	PluginTypeGemini      = 2
)

// getPlatformEnum returns the platform enum value based on the current OS
func getPlatformEnum() int {
	switch runtime.GOOS {
	case "darwin":
		return PlatformMacOS
	case "windows":
		return PlatformWindows
	case "linux":
		return PlatformLinux
	default:
		return PlatformUnspecified
	}
}

// getClientMetadata returns the client metadata JSON string
// Using numeric enum values as expected by the Cloud Code API
func getClientMetadata() string {
	metadata := map[string]int{
		"ideType":    IdeTypeAntigravity, // 6 - identifies as Antigravity client
		"platform":   getPlatformEnum(),  // Runtime platform detection
		"pluginType": PluginTypeGemini,   // 2
	}
	data, _ := json.Marshal(metadata)
	return string(data)
}

// Timing constants
const (
	// TokenRefreshIntervalMs is the token cache TTL (5 minutes)
	TokenRefreshIntervalMs = 5 * 60 * 1000
	// RequestBodyLimit is the max request body size (50MB in bytes)
	RequestBodyLimit int64 = 50 * 1024 * 1024
	// AntigravityAuthPort is the port for auth server
	AntigravityAuthPort = 9092
	// DefaultPort is the default server port
	DefaultPort = 8080
)

// Multi-account configuration
var (
	// AccountConfigPath is the path to the accounts configuration file
	AccountConfigPath = filepath.Join(getHomeDir(), ".config", "antigravity-proxy", "accounts.json")
	// UsageHistoryPath is the path to the usage history file
	UsageHistoryPath = filepath.Join(getHomeDir(), ".config", "antigravity-proxy", "usage-history.json")
	// AntigravityDBPath is the path to the Antigravity app database
	AntigravityDBPath = getAntigravityDbPath()
)

// Rate limit and retry constants
const (
	DefaultCooldownMs         = 10 * 1000  // 10 seconds
	MaxRetries                = 5
	MaxEmptyResponseRetries   = 2
	MaxAccounts               = 10
	MaxWaitBeforeErrorMs      = 120000     // 2 minutes
	RateLimitDedupWindowMs    = 2000       // 2 seconds
	RateLimitStateResetMs     = 120000     // 2 minutes
	FirstRetryDelayMs         = 1000       // 1 second
	SwitchAccountDelayMs      = 5000       // 5 seconds
	MaxConsecutiveFailures    = 3
	ExtendedCooldownMs        = 60000      // 1 minute
	MaxCapacityRetries        = 5
	MinBackoffMs              = 2000       // 2 seconds
	CapacityJitterMaxMs       = 10000      // ±5s jitter range
)

// CapacityBackoffTiersMs is progressive backoff tiers for model capacity issues
var CapacityBackoffTiersMs = []int64{5000, 10000, 20000, 30000, 60000}

// QuotaExhaustedBackoffTiersMs is progressive backoff tiers for QUOTA_EXHAUSTED (60s, 5m, 30m, 2h)
var QuotaExhaustedBackoffTiersMs = []int64{60000, 300000, 1800000, 7200000}

// BackoffByErrorType is smart backoff by error type
var BackoffByErrorType = map[string]int64{
	"RATE_LIMIT_EXCEEDED":      30000,  // 30 seconds
	"MODEL_CAPACITY_EXHAUSTED": 15000,  // 15 seconds
	"SERVER_ERROR":             20000,  // 20 seconds
	"UNKNOWN":                  60000,  // 1 minute
}

// Thinking model constants
const (
	MinSignatureLength = 50
)

// Account selection strategies
var SelectionStrategies = []string{"sticky", "round-robin", "hybrid"}
const DefaultSelectionStrategy = "hybrid"

// StrategyLabels are the display labels for strategies
var StrategyLabels = map[string]string{
	"sticky":      "Sticky (Cache Optimized)",
	"round-robin": "Round Robin (Load Balanced)",
	"hybrid":      "Hybrid (Smart Distribution)",
}

// Gemini-specific limits
const (
	GeminiMaxOutputTokens      = 16384
	GeminiSkipSignature        = "skip_thought_signature_validator"
	GeminiSignatureCacheTTLMs  = 2 * 60 * 60 * 1000  // 2 hours
	ModelValidationCacheTTLMs  = 5 * 60 * 1000       // 5 minutes
)

// OAuth configuration
type OAuthConfigType struct {
	ClientID              string
	ClientSecret          string
	AuthURL               string
	TokenURL              string
	UserInfoURL           string
	CallbackPort          int
	CallbackFallbackPorts []int
	Scopes                []string
}

// OAuthConfig is the Google OAuth configuration
var OAuthConfig = OAuthConfigType{
	ClientID:     "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
	ClientSecret: "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",
	AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:     "https://oauth2.googleapis.com/token",
	UserInfoURL:  "https://www.googleapis.com/oauth2/v1/userinfo",
	CallbackPort: getOAuthCallbackPort(),
	CallbackFallbackPorts: []int{51122, 51123, 51124, 51125, 51126},
	Scopes: []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
		"https://www.googleapis.com/auth/cclog",
		"https://www.googleapis.com/auth/experimentsandconfigs",
	},
}

// OAuthRedirectURI returns the OAuth redirect URI
func OAuthRedirectURI() string {
	return fmt.Sprintf("http://localhost:%d/oauth-callback", OAuthConfig.CallbackPort)
}

// AntigravitySystemInstruction is the minimal system instruction
const AntigravitySystemInstruction = `You are Antigravity, a powerful agentic AI coding assistant designed by the Google Deepmind team working on Advanced Agentic Coding.You are pair programming with a USER to solve their coding task. The task may require creating a new codebase, modifying or debugging an existing codebase, or simply answering a question.**Absolute paths only****Proactiveness**`

// ModelFallbackMap maps primary model to fallback when quota exhausted
var ModelFallbackMap = map[string]string{
	"gemini-3-pro-high":         "claude-opus-4-6-thinking",
	"gemini-3-pro-low":          "claude-sonnet-4-5",
	"gemini-3-flash":            "claude-sonnet-4-5-thinking",
	"claude-opus-4-6-thinking":  "gemini-3-pro-high",
	"claude-sonnet-4-5-thinking": "gemini-3-flash",
	"claude-sonnet-4-5":         "gemini-3-flash",
}

// TestModels are the default test models for each family
var TestModels = map[string]string{
	"claude": "claude-sonnet-4-5-thinking",
	"gemini": "gemini-3-flash",
}

// ModelFamily represents the model family type
type ModelFamily string

const (
	ModelFamilyClaude  ModelFamily = "claude"
	ModelFamilyGemini  ModelFamily = "gemini"
	ModelFamilyUnknown ModelFamily = "unknown"
)

// GetModelFamily returns the model family from model name (dynamic detection)
func GetModelFamily(modelName string) ModelFamily {
	lower := strings.ToLower(modelName)
	if strings.Contains(lower, "claude") {
		return ModelFamilyClaude
	}
	if strings.Contains(lower, "gemini") {
		return ModelFamilyGemini
	}
	return ModelFamilyUnknown
}

// IsThinkingModel checks if a model supports thinking/reasoning output
func IsThinkingModel(modelName string) bool {
	lower := strings.ToLower(modelName)

	// Claude thinking models have "thinking" in the name
	if strings.Contains(lower, "claude") && strings.Contains(lower, "thinking") {
		return true
	}

	// Gemini thinking models: explicit "thinking" in name, OR gemini version 3+
	if strings.Contains(lower, "gemini") {
		if strings.Contains(lower, "thinking") {
			return true
		}
		// Check for gemini-3 or higher (e.g., gemini-3, gemini-3.5, gemini-4, etc.)
		re := regexp.MustCompile(`gemini-(\d+)`)
		matches := re.FindStringSubmatch(lower)
		if len(matches) >= 2 {
			version, err := strconv.Atoi(matches[1])
			if err == nil && version >= 3 {
				return true
			}
		}
	}

	return false
}

// GetFallbackModel returns the fallback model for the given model
func GetFallbackModel(modelName string) (string, bool) {
	fallback, ok := ModelFallbackMap[modelName]
	return fallback, ok
}

// HasFallback checks if a model has a fallback configured
func HasFallback(modelName string) bool {
	_, ok := ModelFallbackMap[modelName]
	return ok
}

// Helper functions

func getHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func getAntigravityDbPath() string {
	home := getHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library/Application Support/Antigravity/User/globalStorage/state.vscdb")
	case "windows":
		return filepath.Join(home, "AppData/Roaming/Antigravity/User/globalStorage/state.vscdb")
	default: // linux, freebsd, etc.
		return filepath.Join(home, ".config/Antigravity/User/globalStorage/state.vscdb")
	}
}

func getOAuthCallbackPort() int {
	portStr := os.Getenv("OAUTH_CALLBACK_PORT")
	if portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err == nil {
			return port
		}
	}
	return 51121
}

// DefaultPreset represents a Claude CLI preset configuration
type DefaultPreset struct {
	Name   string            `json:"name"`
	Config map[string]string `json:"config"`
}

// DefaultPresets are the default Claude CLI presets
var DefaultPresets = []DefaultPreset{
	{
		Name: "Claude Thinking",
		Config: map[string]string{
			"ANTHROPIC_AUTH_TOKEN":          "test",
			"ANTHROPIC_BASE_URL":            "http://localhost:8080",
			"ANTHROPIC_MODEL":               "claude-opus-4-6-thinking",
			"ANTHROPIC_DEFAULT_OPUS_MODEL":  "claude-opus-4-6-thinking",
			"ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-sonnet-4-5-thinking",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL": "claude-sonnet-4-5",
			"CLAUDE_CODE_SUBAGENT_MODEL":    "claude-sonnet-4-5-thinking",
			"ENABLE_EXPERIMENTAL_MCP_CLI":   "true",
		},
	},
	{
		Name: "Gemini 1M",
		Config: map[string]string{
			"ANTHROPIC_AUTH_TOKEN":          "test",
			"ANTHROPIC_BASE_URL":            "http://localhost:8080",
			"ANTHROPIC_MODEL":               "gemini-3-pro-high[1m]",
			"ANTHROPIC_DEFAULT_OPUS_MODEL":  "gemini-3-pro-high[1m]",
			"ANTHROPIC_DEFAULT_SONNET_MODEL": "gemini-3-flash[1m]",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL": "gemini-3-flash[1m]",
			"CLAUDE_CODE_SUBAGENT_MODEL":    "gemini-3-flash[1m]",
			"ENABLE_EXPERIMENTAL_MCP_CLI":   "true",
		},
	},
}

// ServerPresetConfig represents the configuration values in a server preset
type ServerPresetConfig struct {
	MaxRetries             int                    `json:"maxRetries"`
	RetryBaseMs            int64                  `json:"retryBaseMs"`
	RetryMaxMs             int64                  `json:"retryMaxMs"`
	DefaultCooldownMs      int64                  `json:"defaultCooldownMs"`
	MaxWaitBeforeErrorMs   int64                  `json:"maxWaitBeforeErrorMs"`
	MaxAccounts            int                    `json:"maxAccounts"`
	GlobalQuotaThreshold   float64                `json:"globalQuotaThreshold"`
	RateLimitDedupWindowMs int64                  `json:"rateLimitDedupWindowMs"`
	MaxConsecutiveFailures int                    `json:"maxConsecutiveFailures"`
	ExtendedCooldownMs     int64                  `json:"extendedCooldownMs"`
	MaxCapacityRetries     int                    `json:"maxCapacityRetries"`
	SwitchAccountDelayMs   int64                  `json:"switchAccountDelayMs"`
	CapacityBackoffTiersMs []int64                `json:"capacityBackoffTiersMs"`
	AccountSelection       AccountSelectionConfig `json:"accountSelection"`
}

// ServerPreset represents a server configuration preset
type ServerPreset struct {
	Name           string             `json:"name"`
	BuiltIn        bool               `json:"builtIn,omitempty"`
	DescriptionKey string             `json:"descriptionKey,omitempty"`
	Description    string             `json:"description,omitempty"`
	Config         ServerPresetConfig `json:"config"`
}

// DefaultServerPresets are the built-in server configuration presets
var DefaultServerPresets = []ServerPreset{
	{
		Name:           "Default (3-5 Accounts)",
		BuiltIn:        true,
		DescriptionKey: "presetDefaultDesc",
		Config: ServerPresetConfig{
			MaxRetries:             5,
			RetryBaseMs:            1000,
			RetryMaxMs:             30000,
			DefaultCooldownMs:      10000,
			MaxWaitBeforeErrorMs:   120000,
			MaxAccounts:            10,
			GlobalQuotaThreshold:   0,
			RateLimitDedupWindowMs: 2000,
			MaxConsecutiveFailures: 3,
			ExtendedCooldownMs:     60000,
			MaxCapacityRetries:     5,
			SwitchAccountDelayMs:   5000,
			CapacityBackoffTiersMs: []int64{5000, 10000, 20000, 30000, 60000},
			AccountSelection: AccountSelectionConfig{
				Strategy: "hybrid",
				HealthScore: &HealthScoreConfig{
					Initial:          70,
					SuccessReward:    1,
					RateLimitPenalty: -10,
					FailurePenalty:   -20,
					RecoveryPerHour:  10,
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
					StaleMs:           300000,
				},
				Weights: &WeightsConfig{
					Health: 2,
					Tokens: 5,
					Quota:  3,
					Lru:    0.1,
				},
			},
		},
	},
	{
		Name:           "Many Accounts (10+)",
		BuiltIn:        true,
		DescriptionKey: "presetManyAccountsDesc",
		Config: ServerPresetConfig{
			MaxRetries:             3,
			RetryBaseMs:            500,
			RetryMaxMs:             15000,
			DefaultCooldownMs:      5000,
			MaxWaitBeforeErrorMs:   60000,
			MaxAccounts:            50,
			GlobalQuotaThreshold:   0.10,
			RateLimitDedupWindowMs: 1000,
			MaxConsecutiveFailures: 2,
			ExtendedCooldownMs:     30000,
			MaxCapacityRetries:     3,
			SwitchAccountDelayMs:   3000,
			CapacityBackoffTiersMs: []int64{3000, 6000, 12000, 20000, 40000},
			AccountSelection: AccountSelectionConfig{
				Strategy: "hybrid",
				HealthScore: &HealthScoreConfig{
					Initial:          70,
					SuccessReward:    1,
					RateLimitPenalty: -15,
					FailurePenalty:   -25,
					RecoveryPerHour:  5,
					MinUsable:        40,
					MaxScore:         100,
				},
				TokenBucket: &TokenBucketConfig{
					MaxTokens:       30,
					TokensPerMinute: 8,
					InitialTokens:   30,
				},
				Quota: &QuotaConfig{
					LowThreshold:      0.15,
					CriticalThreshold: 0.05,
					StaleMs:           180000,
				},
				Weights: &WeightsConfig{
					Health: 5,
					Tokens: 2,
					Quota:  3,
					Lru:    0.01,
				},
			},
		},
	},
	{
		Name:           "Conservative",
		BuiltIn:        true,
		DescriptionKey: "presetConservativeDesc",
		Config: ServerPresetConfig{
			MaxRetries:             8,
			RetryBaseMs:            2000,
			RetryMaxMs:             60000,
			DefaultCooldownMs:      20000,
			MaxWaitBeforeErrorMs:   240000,
			MaxAccounts:            10,
			GlobalQuotaThreshold:   0.20,
			RateLimitDedupWindowMs: 3000,
			MaxConsecutiveFailures: 5,
			ExtendedCooldownMs:     120000,
			MaxCapacityRetries:     8,
			SwitchAccountDelayMs:   8000,
			CapacityBackoffTiersMs: []int64{8000, 15000, 30000, 45000, 90000},
			AccountSelection: AccountSelectionConfig{
				Strategy: "sticky",
				HealthScore: &HealthScoreConfig{
					Initial:          80,
					SuccessReward:    2,
					RateLimitPenalty: -5,
					FailurePenalty:   -10,
					RecoveryPerHour:  3,
					MinUsable:        50,
					MaxScore:         100,
				},
				TokenBucket: &TokenBucketConfig{
					MaxTokens:       80,
					TokensPerMinute: 4,
					InitialTokens:   80,
				},
				Quota: &QuotaConfig{
					LowThreshold:      0.20,
					CriticalThreshold: 0.10,
					StaleMs:           300000,
				},
				Weights: &WeightsConfig{
					Health: 3,
					Tokens: 4,
					Quota:  2,
					Lru:    0.05,
				},
			},
		},
	},
}

// ServerPresetsPath is the path to the server presets file
var ServerPresetsPath = filepath.Join(getHomeDir(), ".config", "antigravity-proxy", "server-presets.json")

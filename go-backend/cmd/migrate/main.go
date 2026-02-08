// Package main provides the JSON to Redis migration tool.
// This tool migrates data from the Node.js JSON file format to Redis.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// LegacyAccountConfig represents the Node.js accounts.json structure
type LegacyAccountConfig struct {
	Accounts    []LegacyAccount        `json:"accounts"`
	Settings    map[string]interface{} `json:"settings"`
	ActiveIndex int                    `json:"activeIndex"`
}

// LegacyAccount represents an account in the old format
type LegacyAccount struct {
	Email            string                        `json:"email"`
	Source           string                        `json:"source"`
	RefreshToken     string                        `json:"refreshToken"`
	ProjectID        string                        `json:"projectId"`
	AddedAt          string                        `json:"addedAt"`
	LastUsed         string                        `json:"lastUsed"`
	Enabled          *bool                         `json:"enabled"`
	IsInvalid        bool                          `json:"isInvalid"`
	InvalidReason    string                        `json:"invalidReason"`
	QuotaThreshold   *float64                      `json:"quotaThreshold"`
	ModelRateLimits  map[string]*LegacyRateLimit   `json:"modelRateLimits"`
	ModelQuotas      map[string]*LegacyQuotaInfo   `json:"modelQuotas,omitempty"`
	Quota            *LegacyAccountQuota           `json:"quota,omitempty"`
	Subscription     *LegacySubscription           `json:"subscription,omitempty"`
}

// LegacyRateLimit represents a rate limit in the old format
type LegacyRateLimit struct {
	IsRateLimited bool  `json:"isRateLimited"`
	ResetTime     int64 `json:"resetTime"`
	ActualResetMs int64 `json:"actualResetMs,omitempty"`
}

// LegacyQuotaInfo represents quota info in the old format
type LegacyQuotaInfo struct {
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         int64   `json:"resetTime"`
}

// LegacyAccountQuota represents account-level quota in the old format
type LegacyAccountQuota struct {
	Models      map[string]*LegacyQuotaInfo `json:"models"`
	LastChecked int64                       `json:"lastChecked"`
}

// LegacySubscription represents subscription info in the old format
type LegacySubscription struct {
	Tier       string `json:"tier"`
	ProjectID  string `json:"projectId"`
	DetectedAt int64  `json:"detectedAt"`
}

// LegacyUsageStats represents the usage-history.json structure
// Format: { "YYYY-MM-DDTHH:00:00.000Z": { "_total": 10, "claude": { "_subtotal": 5, "opus-4-5": 5 } } }
type LegacyUsageStats map[string]map[string]interface{}

func main() {
	var (
		accountsFile string
		usageFile    string
		redisAddr    string
		dryRun       bool
	)

	flag.StringVar(&accountsFile, "accounts", "", "Path to accounts.json (default: ~/.config/antigravity-proxy/accounts.json)")
	flag.StringVar(&usageFile, "usage", "", "Path to usage-history.json (default: ~/.config/antigravity-proxy/usage-history.json)")
	flag.StringVar(&redisAddr, "redis", "localhost:6379", "Redis server address")
	flag.BoolVar(&dryRun, "dry-run", false, "Print what would be migrated without actually migrating")
	flag.Parse()

	// Default paths
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".config", "antigravity-proxy")

	if accountsFile == "" {
		accountsFile = filepath.Join(configDir, "accounts.json")
	}
	if usageFile == "" {
		usageFile = filepath.Join(configDir, "usage-history.json")
	}

	fmt.Println("╔════════════════════════════════════════╗")
	fmt.Println("║    JSON to Redis Migration Tool        ║")
	fmt.Println("╚════════════════════════════════════════╝")
	fmt.Println()

	if dryRun {
		fmt.Println("🔍 DRY RUN MODE - No changes will be made")
		fmt.Println()
	}

	// Connect to Redis
	var redisClient *redis.Client
	var err error
	if !dryRun {
		redisClient, err = redis.NewClient(redis.Config{
			Addr: redisAddr,
		})
		if err != nil {
			fmt.Printf("❌ Failed to connect to Redis at %s: %v\n", redisAddr, err)
			os.Exit(1)
		}
		defer redisClient.Close()
		fmt.Printf("✓ Connected to Redis at %s\n", redisAddr)
	} else {
		fmt.Printf("  Would connect to Redis at %s\n", redisAddr)
	}

	// Migrate accounts
	if err := migrateAccounts(accountsFile, redisClient, dryRun); err != nil {
		fmt.Printf("⚠ Accounts migration warning: %v\n", err)
	}

	// Migrate usage stats
	if err := migrateUsageStats(usageFile, redisClient, dryRun); err != nil {
		fmt.Printf("⚠ Usage stats migration warning: %v\n", err)
	}

	fmt.Println()
	if dryRun {
		fmt.Println("✓ Dry run complete. No changes were made.")
	} else {
		fmt.Println("✓ Migration complete!")
	}
}

func migrateAccounts(path string, redisClient *redis.Client, dryRun bool) error {
	fmt.Printf("\n📁 Migrating accounts from %s\n", path)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("  ℹ No accounts.json found, skipping...")
			return nil
		}
		return fmt.Errorf("failed to read accounts file: %w", err)
	}

	var config LegacyAccountConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse accounts.json: %w", err)
	}

	fmt.Printf("  Found %d accounts\n", len(config.Accounts))

	if dryRun {
		for _, acc := range config.Accounts {
			enabled := acc.Enabled == nil || *acc.Enabled
			fmt.Printf("    • %s (source: %s, enabled: %t)\n", acc.Email, acc.Source, enabled)
		}
		return nil
	}

	// Migrate each account
	ctx := context.Background()
	accountStore := redis.NewAccountStore(redisClient)

	for _, legacyAcc := range config.Accounts {
		enabled := legacyAcc.Enabled == nil || *legacyAcc.Enabled

		// Convert last used time
		var lastUsed int64
		if legacyAcc.LastUsed != "" {
			if t, err := time.Parse(time.RFC3339, legacyAcc.LastUsed); err == nil {
				lastUsed = t.UnixMilli()
			}
		}

		// Build new account structure
		acc := &redis.Account{
			Email:        legacyAcc.Email,
			Source:       legacyAcc.Source,
			Enabled:      enabled,
			RefreshToken: legacyAcc.RefreshToken,
			ProjectID:    legacyAcc.ProjectID,
			LastUsed:     lastUsed,
			IsInvalid:    legacyAcc.IsInvalid,
			InvalidReason: legacyAcc.InvalidReason,
			QuotaThreshold: legacyAcc.QuotaThreshold,
		}

		// Convert subscription
		if legacyAcc.Subscription != nil {
			acc.Subscription = &redis.SubscriptionInfo{
				Tier:       legacyAcc.Subscription.Tier,
				ProjectID:  legacyAcc.Subscription.ProjectID,
				DetectedAt: legacyAcc.Subscription.DetectedAt,
			}
		}

		// Convert quota
		if legacyAcc.Quota != nil {
			acc.Quota = &redis.QuotaInfo{
				Models:      make(map[string]*redis.ModelQuotaInfo),
				LastChecked: legacyAcc.Quota.LastChecked,
			}
			for modelID, q := range legacyAcc.Quota.Models {
				// Convert reset time from int64 to string
				resetTimeStr := ""
				if q.ResetTime > 0 {
					resetTimeStr = time.UnixMilli(q.ResetTime).Format(time.RFC3339)
				}
				acc.Quota.Models[modelID] = &redis.ModelQuotaInfo{
					RemainingFraction: q.RemainingFraction,
					ResetTime:         resetTimeStr,
				}
			}
		}

		// Save account
		if err := accountStore.SetAccount(ctx, acc); err != nil {
			fmt.Printf("    ✗ Failed to migrate %s: %v\n", acc.Email, err)
			continue
		}

		// Migrate rate limits
		if legacyAcc.ModelRateLimits != nil {
			for modelID, limit := range legacyAcc.ModelRateLimits {
				if limit.IsRateLimited && limit.ResetTime > time.Now().UnixMilli() {
					info := &redis.RateLimitInfo{
						IsRateLimited: limit.IsRateLimited,
						ResetTime:     limit.ResetTime,
						ActualResetMs: limit.ActualResetMs,
					}
					if err := accountStore.SetRateLimit(ctx, acc.Email, modelID, info); err != nil {
						fmt.Printf("    ⚠ Failed to migrate rate limit for %s:%s\n", acc.Email, modelID)
					}
				}
			}
		}

		fmt.Printf("    ✓ Migrated %s\n", acc.Email)
	}

	return nil
}

func migrateUsageStats(path string, redisClient *redis.Client, dryRun bool) error {
	fmt.Printf("\n📊 Migrating usage stats from %s\n", path)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("  ℹ No usage-history.json found, skipping...")
			return nil
		}
		return fmt.Errorf("failed to read usage file: %w", err)
	}

	var stats LegacyUsageStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return fmt.Errorf("failed to parse usage-history.json: %w", err)
	}

	fmt.Printf("  Found %d hourly entries\n", len(stats))

	if dryRun {
		// Show sample entries
		count := 0
		for hourKey := range stats {
			if count >= 5 {
				fmt.Printf("    ... and %d more\n", len(stats)-5)
				break
			}
			fmt.Printf("    • %s\n", hourKey)
			count++
		}
		return nil
	}

	// Migrate each hour's stats
	ctx := context.Background()
	statsStore := redis.NewStatsStore(redisClient)

	migrated := 0
	for hourKey, hourData := range stats {
		// Parse ISO timestamp to get the hour key in Redis format
		t, err := time.Parse("2006-01-02T15:04:05.000Z", hourKey)
		if err != nil {
			// Try alternate format
			t, err = time.Parse(time.RFC3339, hourKey)
			if err != nil {
				fmt.Printf("    ⚠ Skipping invalid timestamp: %s\n", hourKey)
				continue
			}
		}

		redisHourKey := t.Format("2006-01-02T15")

		// Build batch request
		requests := make(map[string]map[string]int64)

		for key, value := range hourData {
			if key == "_total" {
				continue // Will be calculated
			}

			// key is the family name (claude, gemini, other)
			familyData, ok := value.(map[string]interface{})
			if !ok {
				continue
			}

			requests[key] = make(map[string]int64)
			for modelKey, countVal := range familyData {
				if modelKey == "_subtotal" {
					continue // Will be calculated
				}
				switch v := countVal.(type) {
				case float64:
					requests[key][modelKey] = int64(v)
				case int64:
					requests[key][modelKey] = v
				case int:
					requests[key][modelKey] = int64(v)
				}
			}
		}

		// Use direct Redis operations to set the stats
		// Since RecordRequestBatch adds to current hour, we need to set directly
		redisKey := redis.PrefixStats + redisHourKey
		raw := redisClient.Raw()

		// Set each field
		for family, models := range requests {
			var familyTotal int64
			for model, count := range models {
				field := family + ":" + model
				raw.HSet(ctx, redisKey, field, count)
				familyTotal += count
			}
			// Set family subtotal
			raw.HSet(ctx, redisKey, family+":_subtotal", familyTotal)
		}

		// Set total
		if total, ok := hourData["_total"].(float64); ok {
			raw.HSet(ctx, redisKey, "_total", int64(total))
		}

		// Set TTL (30 days)
		raw.Expire(ctx, redisKey, 30*24*time.Hour)

		migrated++
	}

	// Use statsStore to avoid unused variable warning
	_ = statsStore

	fmt.Printf("  ✓ Migrated %d hourly entries\n", migrated)
	return nil
}

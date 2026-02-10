// Package modules provides feature modules for the proxy server.
// This file corresponds to src/modules/usage-stats.js in the Node.js version.
package modules

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

// UsageStats provides request tracking and usage statistics
type UsageStats struct {
	statsStore  *redis.StatsStore
	mu          sync.RWMutex
	initialized bool
	stopChan    chan struct{}
}

// NewUsageStats creates a new UsageStats instance
func NewUsageStats(redisClient *redis.Client) *UsageStats {
	var store *redis.StatsStore
	if redisClient != nil {
		store = redis.NewStatsStore(redisClient)
	}

	return &UsageStats{
		statsStore: store,
		stopChan:   make(chan struct{}),
	}
}

// Initialize starts the usage stats module
func (u *UsageStats) Initialize() {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.initialized {
		return
	}

	// Start background pruning
	go u.backgroundPrune()

	u.initialized = true
	utils.Info("[UsageStats] Module initialized")
}

// Shutdown stops the usage stats module
func (u *UsageStats) Shutdown() {
	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.initialized {
		return
	}

	close(u.stopChan)
	u.initialized = false
	utils.Info("[UsageStats] Module shutdown")
}

// backgroundPrune periodically prunes old statistics
func (u *UsageStats) backgroundPrune() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-u.stopChan:
			return
		case <-ticker.C:
			if u.statsStore != nil {
				ctx := context.Background()
				pruned, err := u.statsStore.PruneOldStats(ctx, 30)
				if err != nil {
					utils.Warn("[UsageStats] Failed to prune old stats: %v", err)
				} else if pruned > 0 {
					utils.Debug("[UsageStats] Pruned %d old entries", pruned)
				}
			}
		}
	}
}

// Track records a request for a specific model
func (u *UsageStats) Track(modelID string) {
	if u.statsStore == nil {
		return
	}

	family := GetFamily(modelID)
	shortName := GetShortName(modelID, family)

	ctx := context.Background()
	if err := u.statsStore.RecordRequest(ctx, family, shortName); err != nil {
		utils.Debug("[UsageStats] Failed to record request: %v", err)
	}
}

// GetFamily extracts model family from model ID
func GetFamily(modelID string) string {
	lower := strings.ToLower(modelID)
	if strings.Contains(lower, "claude") {
		return "claude"
	}
	if strings.Contains(lower, "gemini") {
		return "gemini"
	}
	return "other"
}

// GetShortName extracts short model name (without family prefix)
func GetShortName(modelID, family string) string {
	if family == "other" {
		return modelID
	}
	// Remove family prefix (e.g., "claude-opus-4-5" -> "opus-4-5")
	prefix := family + "-"
	lower := strings.ToLower(modelID)
	if strings.HasPrefix(lower, prefix) {
		return modelID[len(prefix):]
	}
	return modelID
}

// GetHistory returns usage history in the format expected by the WebUI
// Returns map[hourKey]map[field]value in Node.js compatible format
func (u *UsageStats) GetHistory(ctx context.Context) (map[string]interface{}, error) {
	if u.statsStore == nil {
		return make(map[string]interface{}), nil
	}

	history, err := u.statsStore.GetHistory(ctx, 30)
	if err != nil {
		return nil, err
	}

	// Convert to the Node.js format expected by the frontend
	// Format: { "2024-01-01T00:00:00.000Z": { "_total": 10, "claude": { "_subtotal": 5, "opus-4-5": 5 } } }
	result := make(map[string]interface{})

	for hourKey, stats := range history {
		// Convert hour key to ISO format
		t, err := time.Parse("2006-01-02T15", hourKey)
		if err != nil {
			continue
		}
		isoKey := t.Format("2006-01-02T15:04:05.000Z")

		hourData := make(map[string]interface{})
		hourData["_total"] = stats.Total

		for family, familyStats := range stats.Families {
			familyData := make(map[string]interface{})
			familyData["_subtotal"] = familyStats.Subtotal
			for model, count := range familyStats.Models {
				familyData[model] = count
			}
			hourData[family] = familyData
		}

		result[isoKey] = hourData
	}

	return result, nil
}

// GetSortedHistory returns history sorted chronologically
func (u *UsageStats) GetSortedHistory(ctx context.Context) (map[string]interface{}, error) {
	history, err := u.GetHistory(ctx)
	if err != nil {
		return nil, err
	}

	// Extract and sort keys
	keys := make([]string, 0, len(history))
	for k := range history {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build sorted result (Go maps maintain insertion order in practice but
	// we return with sorted keys for frontend consistency)
	result := make(map[string]interface{})
	for _, k := range keys {
		result[k] = history[k]
	}

	return result, nil
}

// Middleware creates a Gin middleware for tracking requests
func (u *UsageStats) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Track POST requests to messages endpoints
		if c.Request.Method == "POST" {
			path := c.Request.URL.Path
			if path == "/v1/messages" || path == "/v1/chat/completions" {
				// We need to track after the request body is parsed
				// Store the model tracking function for later use
				c.Set("trackUsage", func(model string) {
					u.Track(model)
				})
			}
		}
		c.Next()
	}
}

// TrackFromContext tracks a request if the middleware was set up
func TrackFromContext(c *gin.Context, model string) {
	if trackFn, exists := c.Get("trackUsage"); exists {
		if fn, ok := trackFn.(func(string)); ok {
			fn(model)
		}
	}
}

// SetupRoutes adds the stats API routes to an engine
func (u *UsageStats) SetupRoutes(router *gin.RouterGroup) {
	router.GET("/stats/history", u.handleGetHistory)
}

// handleGetHistory handles GET /api/stats/history
func (u *UsageStats) handleGetHistory(c *gin.Context) {
	ctx := c.Request.Context()
	history, err := u.GetSortedHistory(ctx)
	if err != nil {
		utils.Error("[UsageStats] Failed to get history: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, history)
}

// GetStatsStore returns the underlying stats store for advanced operations
func (u *UsageStats) GetStatsStore() *redis.StatsStore {
	return u.statsStore
}

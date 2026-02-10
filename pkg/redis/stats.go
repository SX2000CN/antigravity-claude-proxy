// Package redis provides Redis operations for usage statistics.
// This file corresponds to src/modules/usage-stats.js in the Node.js version.
package redis

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// StatsStore provides usage statistics operations
type StatsStore struct {
	client *Client
}

// NewStatsStore creates a new StatsStore
func NewStatsStore(client *Client) *StatsStore {
	return &StatsStore{client: client}
}

// StatsTTL is the TTL for usage statistics (30 days)
const StatsTTL = 30 * 24 * time.Hour

// HourlyStats represents usage statistics for a single hour
type HourlyStats struct {
	Hour     string                       `json:"hour"`      // Format: "2024-02-08T14"
	Total    int64                        `json:"total"`
	Families map[string]*FamilyStats      `json:"families"`
}

// FamilyStats represents statistics for a model family
type FamilyStats struct {
	Subtotal int64          `json:"subtotal"`
	Models   map[string]int64 `json:"models"`
}

// ============================================================
// Recording Operations
// ============================================================

// RecordRequest records a single request for statistics
func (s *StatsStore) RecordRequest(ctx context.Context, modelFamily, modelShortName string) error {
	hourKey := getCurrentHourKey()
	key := PrefixStats + hourKey

	// Increment total
	if _, err := s.client.HIncrBy(ctx, key, "_total", 1); err != nil {
		return err
	}

	// Increment family subtotal
	familyField := modelFamily + ":_subtotal"
	if _, err := s.client.HIncrBy(ctx, key, familyField, 1); err != nil {
		return err
	}

	// Increment specific model
	modelField := modelFamily + ":" + modelShortName
	if _, err := s.client.HIncrBy(ctx, key, modelField, 1); err != nil {
		return err
	}

	// Set TTL (only if key is new)
	return s.client.Expire(ctx, key, StatsTTL)
}

// RecordRequestBatch records multiple requests efficiently
func (s *StatsStore) RecordRequestBatch(ctx context.Context, requests map[string]map[string]int64) error {
	hourKey := getCurrentHourKey()
	key := PrefixStats + hourKey

	pipe := s.client.Pipeline()

	var total int64
	for family, models := range requests {
		var familyTotal int64
		for model, count := range models {
			// Increment specific model
			modelField := family + ":" + model
			pipe.HIncrBy(ctx, key, modelField, count)
			familyTotal += count
		}

		// Increment family subtotal
		familyField := family + ":_subtotal"
		pipe.HIncrBy(ctx, key, familyField, familyTotal)
		total += familyTotal
	}

	// Increment total
	pipe.HIncrBy(ctx, key, "_total", total)

	// Set TTL
	pipe.Expire(ctx, key, StatsTTL)

	_, err := pipe.Exec(ctx)
	return err
}

// ============================================================
// Retrieval Operations
// ============================================================

// GetHourlyStats retrieves statistics for a specific hour
func (s *StatsStore) GetHourlyStats(ctx context.Context, hourKey string) (*HourlyStats, error) {
	key := PrefixStats + hourKey

	data, err := s.client.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	stats := &HourlyStats{
		Hour:     hourKey,
		Families: make(map[string]*FamilyStats),
	}

	for field, value := range data {
		count, _ := strconv.ParseInt(value, 10, 64)

		if field == "_total" {
			stats.Total = count
			continue
		}

		// Parse family:model format
		family, model := parseStatsField(field)
		if family == "" {
			continue
		}

		// Ensure family exists
		if _, ok := stats.Families[family]; !ok {
			stats.Families[family] = &FamilyStats{
				Models: make(map[string]int64),
			}
		}

		if model == "_subtotal" {
			stats.Families[family].Subtotal = count
		} else {
			stats.Families[family].Models[model] = count
		}
	}

	return stats, nil
}

// GetHistory retrieves historical statistics for the specified number of days
func (s *StatsStore) GetHistory(ctx context.Context, days int) (map[string]*HourlyStats, error) {
	if days <= 0 {
		days = 30
	}

	// Get all stat keys
	keys, err := s.client.ScanAll(ctx, PrefixStats+"*")
	if err != nil {
		return nil, err
	}

	// Calculate cutoff time
	cutoff := time.Now().AddDate(0, 0, -days)

	history := make(map[string]*HourlyStats)

	for _, key := range keys {
		hourKey := key[len(PrefixStats):]

		// Parse the hour key to check if it's within range
		t, err := time.Parse("2006-01-02T15", hourKey)
		if err != nil {
			continue
		}

		if t.Before(cutoff) {
			continue
		}

		stats, err := s.GetHourlyStats(ctx, hourKey)
		if err != nil {
			continue
		}
		if stats != nil {
			history[hourKey] = stats
		}
	}

	return history, nil
}

// GetSortedHistory returns history sorted chronologically
func (s *StatsStore) GetSortedHistory(ctx context.Context, days int) ([]*HourlyStats, error) {
	history, err := s.GetHistory(ctx, days)
	if err != nil {
		return nil, err
	}

	// Extract and sort keys
	keys := make([]string, 0, len(history))
	for k := range history {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build sorted result
	result := make([]*HourlyStats, len(keys))
	for i, k := range keys {
		result[i] = history[k]
	}

	return result, nil
}

// GetRecentStats retrieves statistics for the last N hours
func (s *StatsStore) GetRecentStats(ctx context.Context, hours int) ([]*HourlyStats, error) {
	if hours <= 0 {
		hours = 24
	}

	now := time.Now().UTC()
	result := make([]*HourlyStats, 0, hours)

	for i := 0; i < hours; i++ {
		t := now.Add(-time.Duration(i) * time.Hour)
		hourKey := t.Format("2006-01-02T15")

		stats, err := s.GetHourlyStats(ctx, hourKey)
		if err != nil {
			continue
		}
		if stats != nil {
			result = append(result, stats)
		}
	}

	return result, nil
}

// ============================================================
// Aggregation Operations
// ============================================================

// GetTotalsByFamily returns total requests by family for a time range
func (s *StatsStore) GetTotalsByFamily(ctx context.Context, hours int) (map[string]int64, error) {
	stats, err := s.GetRecentStats(ctx, hours)
	if err != nil {
		return nil, err
	}

	totals := make(map[string]int64)
	for _, hourStats := range stats {
		for family, familyStats := range hourStats.Families {
			totals[family] += familyStats.Subtotal
		}
	}

	return totals, nil
}

// GetTotalsByModel returns total requests by model for a time range
func (s *StatsStore) GetTotalsByModel(ctx context.Context, hours int) (map[string]int64, error) {
	stats, err := s.GetRecentStats(ctx, hours)
	if err != nil {
		return nil, err
	}

	totals := make(map[string]int64)
	for _, hourStats := range stats {
		for family, familyStats := range hourStats.Families {
			for model, count := range familyStats.Models {
				key := family + ":" + model
				totals[key] += count
			}
		}
	}

	return totals, nil
}

// GetGrandTotal returns the total request count for a time range
func (s *StatsStore) GetGrandTotal(ctx context.Context, hours int) (int64, error) {
	stats, err := s.GetRecentStats(ctx, hours)
	if err != nil {
		return 0, err
	}

	var total int64
	for _, hourStats := range stats {
		total += hourStats.Total
	}

	return total, nil
}

// ============================================================
// Cleanup Operations
// ============================================================

// PruneOldStats removes statistics older than the specified number of days
func (s *StatsStore) PruneOldStats(ctx context.Context, days int) (int, error) {
	if days <= 0 {
		days = 30
	}

	cutoff := time.Now().AddDate(0, 0, -days)

	keys, err := s.client.ScanAll(ctx, PrefixStats+"*")
	if err != nil {
		return 0, err
	}

	var pruned int
	for _, key := range keys {
		hourKey := key[len(PrefixStats):]

		t, err := time.Parse("2006-01-02T15", hourKey)
		if err != nil {
			continue
		}

		if t.Before(cutoff) {
			if err := s.client.Delete(ctx, key); err == nil {
				pruned++
			}
		}
	}

	return pruned, nil
}

// ClearAllStats removes all usage statistics
func (s *StatsStore) ClearAllStats(ctx context.Context) error {
	keys, err := s.client.ScanAll(ctx, PrefixStats+"*")
	if err != nil {
		return err
	}

	if len(keys) > 0 {
		return s.client.Delete(ctx, keys...)
	}

	return nil
}

// ============================================================
// Helper Functions
// ============================================================

// getCurrentHourKey returns the current hour in the format used for stats keys
func getCurrentHourKey() string {
	return time.Now().UTC().Format("2006-01-02T15")
}

// parseStatsField parses a stats field into family and model components
func parseStatsField(field string) (family, model string) {
	for i := 0; i < len(field); i++ {
		if field[i] == ':' {
			return field[:i], field[i+1:]
		}
	}
	return "", ""
}

// GetModelShortName extracts a short name from a full model name
func GetModelShortName(modelName string) string {
	// Example: "claude-opus-4-5-thinking" -> "opus-4-5-thinking"
	// Example: "gemini-3-flash" -> "3-flash"

	// For Claude models, remove "claude-" prefix
	if len(modelName) > 7 && modelName[:7] == "claude-" {
		return modelName[7:]
	}

	// For Gemini models, remove "gemini-" prefix
	if len(modelName) > 7 && modelName[:7] == "gemini-" {
		return modelName[7:]
	}

	return modelName
}

// FormatStatsKey formats a stats key from components
func FormatStatsKey(hourKey string) string {
	return fmt.Sprintf("%s%s", PrefixStats, hourKey)
}

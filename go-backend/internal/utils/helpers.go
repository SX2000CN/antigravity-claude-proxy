// Package utils provides utility functions for the Antigravity proxy.
// This file corresponds to src/utils/helpers.js in the Node.js version.
package utils

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Version is the package version
const Version = "1.0.0"

// GetPackageVersion returns the package version
// In Go, this is typically set at compile time or from a constants file
func GetPackageVersion() string {
	return Version
}

// FormatDuration formats duration in milliseconds to human-readable string
// Examples: "1h23m45s", "5m30s", "45s"
func FormatDuration(ms int64) string {
	seconds := ms / 1000
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm%ds", hours, minutes, secs)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, secs)
	}
	return fmt.Sprintf("%ds", secs)
}

// FormatDurationFromTime formats a time.Duration to human-readable string
func FormatDurationFromTime(d time.Duration) string {
	return FormatDuration(d.Milliseconds())
}

// Sleep pauses execution for the specified duration in milliseconds
func Sleep(ctx context.Context, ms int64) error {
	select {
	case <-time.After(time.Duration(ms) * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SleepMs is a simple sleep without context
func SleepMs(ms int64) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// IsNetworkError checks if an error is a network error (transient)
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fetch failed") ||
		strings.Contains(msg, "network error") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "eof")
}

// GenerateJitter generates random jitter for backoff timing (Thundering Herd Prevention)
// Prevents all clients from retrying at the exact same moment after errors.
// Returns a value between -maxJitterMs/2 and +maxJitterMs/2
func GenerateJitter(maxJitterMs int64) int64 {
	return int64(rand.Float64()*float64(maxJitterMs)) - (maxJitterMs / 2)
}

// GenerateJitterPositive generates positive jitter (0 to maxJitterMs)
func GenerateJitterPositive(maxJitterMs int64) int64 {
	return int64(rand.Float64() * float64(maxJitterMs))
}

// Min returns the minimum of two int64 values
func Min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// Max returns the maximum of two int64 values
func Max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// MinInt returns the minimum of two int values
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MaxInt returns the maximum of two int values
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Clamp restricts a value to be within a range
func Clamp(value, min, max int64) int64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ClampFloat restricts a float64 value to be within a range
func ClampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// GetHomeDir returns the user's home directory
func GetHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// EnsureDir creates a directory if it doesn't exist
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// EnsureParentDir creates the parent directory of a file path if it doesn't exist
func EnsureParentDir(filePath string) error {
	dir := filepath.Dir(filePath)
	return EnsureDir(dir)
}

// FileExists checks if a file exists
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DirExists checks if a directory exists
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// NowMs returns the current time in milliseconds since epoch
func NowMs() int64 {
	return time.Now().UnixMilli()
}

// NowISO returns the current time as an ISO8601 string
func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// ParseISO parses an ISO8601 string to time.Time
func ParseISO(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

// SafeString returns the string value or empty string if nil
func SafeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// SafeInt64 returns the int64 value or 0 if nil
func SafeInt64(n *int64) int64 {
	if n == nil {
		return 0
	}
	return *n
}

// SafeFloat64 returns the float64 value or 0 if nil
func SafeFloat64(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// SafeBool returns the bool value or false if nil
func SafeBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

// Ptr returns a pointer to the value
func Ptr[T any](v T) *T {
	return &v
}

// StringPtr returns a pointer to a string
func StringPtr(s string) *string {
	return &s
}

// Int64Ptr returns a pointer to an int64
func Int64Ptr(n int64) *int64 {
	return &n
}

// Float64Ptr returns a pointer to a float64
func Float64Ptr(f float64) *float64 {
	return &f
}

// BoolPtr returns a pointer to a bool
func BoolPtr(b bool) *bool {
	return &b
}

// CoalesceString returns the first non-empty string
func CoalesceString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// TruncateString truncates a string to maxLen characters
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ToLower converts a string to lowercase
func ToLower(s string) string {
	return strings.ToLower(s)
}

// ContainsAny checks if a string contains any of the given substrings
func ContainsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// MaskEmail masks an email address for privacy (e.g., "j***@example.com")
func MaskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***"
	}
	local := parts[0]
	if len(local) <= 1 {
		return local + "***@" + parts[1]
	}
	return string(local[0]) + "***@" + parts[1]
}

// init initializes the random seed
func init() {
	rand.Seed(time.Now().UnixNano())
}

// FormatPercent formats a fraction as a percentage string (e.g., 0.75 -> "75%")
func FormatPercent(fraction float64) string {
	return fmt.Sprintf("%d%%", int(fraction*100))
}

// Package cloudcode provides Cloud Code API client implementation.
// This file corresponds to src/cloudcode/rate-limit-parser.js in the Node.js version.
package cloudcode

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// RateLimitReason represents the type of rate limit encountered
type RateLimitReason string

const (
	RateLimitReasonRateLimitExceeded     RateLimitReason = "RATE_LIMIT_EXCEEDED"
	RateLimitReasonQuotaExhausted        RateLimitReason = "QUOTA_EXHAUSTED"
	RateLimitReasonModelCapacityExhausted RateLimitReason = "MODEL_CAPACITY_EXHAUSTED"
	RateLimitReasonServerError           RateLimitReason = "SERVER_ERROR"
	RateLimitReasonUnknown               RateLimitReason = "UNKNOWN"
)

var (
	quotaDelayRegex     = regexp.MustCompile(`(?i)quotaResetDelay[:\s"]+(\d+(?:\.\d+)?)(ms|s)`)
	quotaTimestampRegex = regexp.MustCompile(`(?i)quotaResetTimeStamp[:\s"]+(\d{4}-\d{2}-\d{2}T[\d:.]+Z?)`)
	retrySecondsRegex   = regexp.MustCompile(`(?i)(?:retry[-_]?after[-_]?ms|retryDelay)[:\s"]+([\d.]+)(?:s\b|s")`)
	// Note: Go doesn't support negative lookahead (?!), using simpler pattern
	retryMsRegex        = regexp.MustCompile(`(?i)(?:retry[-_]?after[-_]?ms|retryDelay)[:\s"]+(\d+)(?:\s*ms)?(?:\s|$|[,;}\]])`)
	retryAfterSecRegex  = regexp.MustCompile(`(?i)retry\s+(?:after\s+)?(\d+)\s*(?:sec|s\b)`)
	durationRegex       = regexp.MustCompile(`(?i)(\d+)h(\d+)m(\d+)s|(\d+)m(\d+)s|(\d+)s`)
	isoTimestampRegex   = regexp.MustCompile(`(?i)reset[:\s"]+(\d{4}-\d{2}-\d{2}T[\d:.]+Z?)`)
)

// ParseResetTime parses reset time from HTTP headers or error message.
// Returns milliseconds or -1 if not found.
func ParseResetTime(headers http.Header, errorText string) int64 {
	var resetMs int64 = -1

	// Check headers first
	if retryAfter := headers.Get("retry-after"); retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			resetMs = int64(seconds) * 1000
			utils.Debug("[CloudCode] Retry-After header: %ds", seconds)
		} else {
			// Try parsing as HTTP date
			if t, err := time.Parse(time.RFC1123, retryAfter); err == nil {
				resetMs = t.Sub(time.Now()).Milliseconds()
				if resetMs > 0 {
					utils.Debug("[CloudCode] Retry-After date: %s", retryAfter)
				} else {
					resetMs = -1
				}
			}
		}
	}

	// x-ratelimit-reset (Unix timestamp in seconds)
	if resetMs < 0 {
		if ratelimitReset := headers.Get("x-ratelimit-reset"); ratelimitReset != "" {
			if ts, err := strconv.ParseInt(ratelimitReset, 10, 64); err == nil {
				resetMs = ts*1000 - time.Now().UnixMilli()
				if resetMs > 0 {
					utils.Debug("[CloudCode] x-ratelimit-reset: %s", time.UnixMilli(ts*1000).Format(time.RFC3339))
				} else {
					resetMs = -1
				}
			}
		}
	}

	// x-ratelimit-reset-after (seconds)
	if resetMs < 0 {
		if resetAfter := headers.Get("x-ratelimit-reset-after"); resetAfter != "" {
			if seconds, err := strconv.Atoi(resetAfter); err == nil && seconds > 0 {
				resetMs = int64(seconds) * 1000
				utils.Debug("[CloudCode] x-ratelimit-reset-after: %ds", seconds)
			}
		}
	}

	// Parse from error message body
	if resetMs < 0 && errorText != "" {
		resetMs = parseResetTimeFromBody(errorText)
	}

	// Sanity check: handle very small or negative reset times
	if resetMs >= 0 {
		if resetMs <= 0 {
			utils.Debug("[CloudCode] Reset time invalid (%dms), using 500ms default", resetMs)
			resetMs = 500
		} else if resetMs < 500 {
			// Very short reset - add 200ms buffer for network latency
			utils.Debug("[CloudCode] Short reset time (%dms), adding 200ms buffer", resetMs)
			resetMs = resetMs + 200
		}
	}

	return resetMs
}

// parseResetTimeFromBody parses reset time from error body text
func parseResetTimeFromBody(msg string) int64 {
	var resetMs int64 = -1

	// Try to extract "quotaResetDelay" first (e.g. "754.431528ms" or "1.5s")
	if match := quotaDelayRegex.FindStringSubmatch(msg); match != nil {
		value, _ := strconv.ParseFloat(match[1], 64)
		unit := strings.ToLower(match[2])
		if unit == "s" {
			resetMs = int64(value * 1000)
		} else {
			resetMs = int64(value)
		}
		utils.Debug("[CloudCode] Parsed quotaResetDelay from body: %dms", resetMs)
		return resetMs
	}

	// Try to extract "quotaResetTimeStamp" (ISO format)
	if match := quotaTimestampRegex.FindStringSubmatch(msg); match != nil {
		if t, err := time.Parse(time.RFC3339, match[1]); err == nil {
			resetMs = t.Sub(time.Now()).Milliseconds()
			utils.Debug("[CloudCode] Parsed quotaResetTimeStamp: %s (Delta: %dms)", match[1], resetMs)
			return resetMs
		}
	}

	// Try to extract "retry-after-ms" or "retryDelay" - check seconds format first
	if match := retrySecondsRegex.FindStringSubmatch(msg); match != nil {
		value, _ := strconv.ParseFloat(match[1], 64)
		resetMs = int64(value * 1000)
		utils.Debug("[CloudCode] Parsed retry seconds from body (precise): %dms", resetMs)
		return resetMs
	}

	// Check for ms (explicit "ms" suffix or implicit if no suffix)
	if match := retryMsRegex.FindStringSubmatch(msg); match != nil {
		resetMs, _ = strconv.ParseInt(match[1], 10, 64)
		utils.Debug("[CloudCode] Parsed retry-after-ms from body: %dms", resetMs)
		return resetMs
	}

	// Try to extract seconds value like "retry after 60 seconds"
	if match := retryAfterSecRegex.FindStringSubmatch(msg); match != nil {
		seconds, _ := strconv.ParseInt(match[1], 10, 64)
		resetMs = seconds * 1000
		utils.Debug("[CloudCode] Parsed retry seconds from body: %ds", seconds)
		return resetMs
	}

	// Try to extract duration like "1h23m45s" or "23m45s" or "45s"
	if match := durationRegex.FindStringSubmatch(msg); match != nil {
		if match[1] != "" {
			hours, _ := strconv.Atoi(match[1])
			minutes, _ := strconv.Atoi(match[2])
			seconds, _ := strconv.Atoi(match[3])
			resetMs = int64((hours*3600 + minutes*60 + seconds) * 1000)
		} else if match[4] != "" {
			minutes, _ := strconv.Atoi(match[4])
			seconds, _ := strconv.Atoi(match[5])
			resetMs = int64((minutes*60 + seconds) * 1000)
		} else if match[6] != "" {
			seconds, _ := strconv.Atoi(match[6])
			resetMs = int64(seconds * 1000)
		}
		if resetMs > 0 {
			utils.Debug("[CloudCode] Parsed duration from body: %s", utils.FormatDuration(resetMs))
		}
		return resetMs
	}

	// Try to extract ISO timestamp
	if match := isoTimestampRegex.FindStringSubmatch(msg); match != nil {
		if t, err := time.Parse(time.RFC3339, match[1]); err == nil {
			resetMs = t.Sub(time.Now()).Milliseconds()
			if resetMs > 0 {
				utils.Debug("[CloudCode] Parsed ISO reset time: %s", match[1])
				return resetMs
			}
		}
	}

	return -1
}

// ParseRateLimitReason parses the rate limit reason from error text
func ParseRateLimitReason(errorText string, status int) RateLimitReason {
	// Status code checks FIRST
	// 529 = Site Overloaded, 503 = Service Unavailable → Capacity issues
	if status == 529 || status == 503 {
		return RateLimitReasonModelCapacityExhausted
	}
	// 500 = Internal Server Error → Treat as Server Error (soft wait)
	if status == 500 {
		return RateLimitReasonServerError
	}

	lower := strings.ToLower(errorText)

	// Check for quota exhaustion (daily/hourly limits)
	if strings.Contains(lower, "quota_exhausted") ||
		strings.Contains(lower, "quotaresetdelay") ||
		strings.Contains(lower, "quotaresettimestamp") ||
		strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "daily limit") ||
		strings.Contains(lower, "quota exceeded") {
		return RateLimitReasonQuotaExhausted
	}

	// Check for model capacity issues (temporary, retry quickly)
	if strings.Contains(lower, "model_capacity_exhausted") ||
		strings.Contains(lower, "capacity_exhausted") ||
		strings.Contains(lower, "model is currently overloaded") ||
		strings.Contains(lower, "service temporarily unavailable") {
		return RateLimitReasonModelCapacityExhausted
	}

	// Check for rate limiting (per-minute limits)
	if strings.Contains(lower, "rate_limit_exceeded") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "throttl") {
		return RateLimitReasonRateLimitExceeded
	}

	// Check for server errors
	if strings.Contains(lower, "internal server error") ||
		strings.Contains(lower, "server error") ||
		strings.Contains(lower, "503") ||
		strings.Contains(lower, "502") ||
		strings.Contains(lower, "504") {
		return RateLimitReasonServerError
	}

	return RateLimitReasonUnknown
}

// Package server provides the HTTP server implementation.
// This file corresponds to the middleware in src/server.js.
package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// CORSMiddleware handles CORS headers
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// APIKeyAuthMiddleware validates API key for /v1/* endpoints
func APIKeyAuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip validation if apiKey is not configured
		if cfg.APIKey == "" {
			c.Next()
			return
		}

		// Get API key from Authorization header or X-API-Key header
		var providedKey string
		authHeader := c.GetHeader("Authorization")
		xAPIKey := c.GetHeader("X-API-Key")

		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			providedKey = strings.TrimPrefix(authHeader, "Bearer ")
		} else if xAPIKey != "" {
			providedKey = xAPIKey
		}

		if providedKey == "" || providedKey != cfg.APIKey {
			utils.Warn("[API] Unauthorized request from %s, invalid API key", c.ClientIP())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"type": "error",
				"error": gin.H{
					"type":    "authentication_error",
					"message": "Invalid or missing API key",
				},
			})
			return
		}

		c.Next()
	}
}

// RequestLoggingMiddleware logs all requests
func RequestLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		duration := time.Since(start)
		status := c.Writer.Status()
		logMsg := "[%s] %s %d (%dms)"

		// Skip logging for certain paths unless debug mode
		if path == "/api/event_logging/batch" ||
			strings.HasPrefix(path, "/v1/messages/count_tokens") ||
			strings.HasPrefix(path, "/.well-known/") {
			if utils.IsDebug() {
				utils.Debug(logMsg, c.Request.Method, c.Request.URL.Path, status, duration.Milliseconds())
			}
			return
		}

		// Colorize status code
		if status >= 500 {
			utils.Error(logMsg, c.Request.Method, c.Request.URL.Path, status, duration.Milliseconds())
		} else if status >= 400 {
			utils.Warn(logMsg, c.Request.Method, c.Request.URL.Path, status, duration.Milliseconds())
		} else {
			utils.Info(logMsg, c.Request.Method, c.Request.URL.Path, status, duration.Milliseconds())
		}
	}
}

// SilentHandlerMiddleware handles Claude Code CLI silent endpoints
func SilentHandlerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Handle Claude Code event logging requests silently
		if c.Request.Method == "POST" && c.Request.URL.Path == "/api/event_logging/batch" {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			c.Abort()
			return
		}
		// Handle Claude Code root POST requests silently
		if c.Request.Method == "POST" && c.Request.URL.Path == "/" {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			c.Abort()
			return
		}

		c.Next()
	}
}

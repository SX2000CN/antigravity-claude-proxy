// Package webui provides the web management interface.
// This file corresponds to the auth middleware in src/webui/index.js in the Node.js version.
package webui

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
)

// AuthMiddleware creates an authentication middleware for WebUI
// Password can be set via WEBUI_PASSWORD env var or config.json
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		password := cfg.WebUIPassword
		if password == "" {
			c.Next()
			return
		}

		// Determine if this path should be protected
		path := c.Request.URL.Path
		method := c.Request.Method

		isAPIRoute := len(path) >= 5 && path[:5] == "/api/"
		isAuthURL := path == "/api/auth/url"
		isConfigGet := path == "/api/config" && method == "GET"
		isProtected := (isAPIRoute && !isAuthURL && !isConfigGet) || path == "/account-limits" || path == "/health"

		if isProtected {
			providedPassword := c.GetHeader("X-WebUI-Password")
			if providedPassword == "" {
				providedPassword = c.Query("password")
			}

			if providedPassword != password {
				c.JSON(http.StatusUnauthorized, gin.H{
					"status": "error",
					"error":  "Unauthorized: Password required",
				})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

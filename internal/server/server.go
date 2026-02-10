// Package server provides the main HTTP server implementation.
// This file corresponds to src/server.js in the Node.js version.
package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/cloudcode"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/format"
	"github.com/poemonsense/antigravity-proxy-go/internal/server/handlers"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// Server represents the main HTTP server
type Server struct {
	engine          *gin.Engine
	accountManager  *account.Manager
	cloudCodeClient *cloudcode.Client
	cfg             *config.Config
	fallbackEnabled bool
	strategyOverride string

	// Initialization state
	initOnce    sync.Once
	initError   error
	initialized bool
}

// Options holds server configuration options
type Options struct {
	FallbackEnabled  bool
	StrategyOverride string
	Debug            bool
}

// New creates a new Server instance
func New(cfg *config.Config, accountManager *account.Manager, opts Options) *Server {
	// Set gin mode based on debug flag
	if opts.Debug || cfg.DevMode {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create Gin engine with defaults
	engine := gin.New()

	// Disable x-powered-by equivalent (not needed in Gin, but disable trusted proxies warning)
	engine.SetTrustedProxies(nil)

	// Recovery middleware
	engine.Use(gin.Recovery())

	s := &Server{
		engine:           engine,
		accountManager:   accountManager,
		cfg:              cfg,
		fallbackEnabled:  opts.FallbackEnabled,
		strategyOverride: opts.StrategyOverride,
	}

	return s
}

// Initialize initializes the server and account manager
func (s *Server) Initialize(ctx context.Context) error {
	s.initOnce.Do(func() {
		// Initialize account manager
		if err := s.accountManager.Initialize(ctx, s.strategyOverride); err != nil {
			s.initError = err
			utils.Error("[Server] Failed to initialize account manager: %v", err)
			return
		}

		// Create cloud code client
		s.cloudCodeClient = cloudcode.NewClient(s.accountManager, s.cfg)

		// Log success
		status := s.accountManager.GetStatus()
		utils.Success("[Server] Account pool initialized: %s", status.Summary)

		s.initialized = true
	})

	return s.initError
}

// ensureInitialized ensures the server is initialized
func (s *Server) ensureInitialized(c *gin.Context) bool {
	if s.initialized {
		return true
	}

	if err := s.Initialize(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "api_error",
				"message": "Server not initialized: " + err.Error(),
			},
		})
		return false
	}

	return true
}

// SetupRoutes sets up all HTTP routes
func (s *Server) SetupRoutes() {
	// Apply global middleware
	s.engine.Use(CORSMiddleware())
	s.engine.Use(SilentHandlerMiddleware())
	s.engine.Use(RequestLoggingMiddleware())

	// Request body limit (10MB)
	s.engine.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.RequestBodyLimit)
		c.Next()
	})

	// Create handlers
	healthHandler := handlers.NewHealthHandler(s.accountManager)
	modelsHandler := handlers.NewModelsHandler(s.accountManager)
	accountsHandler := handlers.NewAccountsHandler(s.accountManager, s.cfg)
	messagesHandler := handlers.NewMessagesHandler(
		s.accountManager,
		s.cloudCodeClient,
		s.cfg,
		s.fallbackEnabled,
	)
	refreshHandler := handlers.NewRefreshTokenHandler(s.accountManager)

	// Silent handler for Claude Code root POST
	s.engine.POST("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Test endpoint - Clear thinking signature cache
	s.engine.POST("/test/clear-signature-cache", func(c *gin.Context) {
		format.ClearThinkingSignatureCache()
		utils.Debug("[Test] Cleared thinking signature cache")
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Thinking signature cache cleared",
		})
	})

	// Health check endpoint
	s.engine.GET("/health", func(c *gin.Context) {
		if !s.ensureInitialized(c) {
			return
		}
		healthHandler.Health(c)
	})

	// Account limits endpoint
	s.engine.GET("/account-limits", func(c *gin.Context) {
		if !s.ensureInitialized(c) {
			return
		}
		accountsHandler.AccountLimits(c)
	})

	// Refresh token endpoint
	s.engine.POST("/refresh-token", func(c *gin.Context) {
		if !s.ensureInitialized(c) {
			return
		}
		refreshHandler.RefreshToken(c)
	})

	// API v1 routes with authentication
	v1 := s.engine.Group("/v1")
	v1.Use(APIKeyAuthMiddleware(s.cfg))
	{
		// Models endpoint
		v1.GET("/models", func(c *gin.Context) {
			if !s.ensureInitialized(c) {
				return
			}
			modelsHandler.ListModels(c)
		})

		// Token counting (not implemented)
		v1.POST("/messages/count_tokens", messagesHandler.CountTokens)

		// Main messages endpoint
		v1.POST("/messages", func(c *gin.Context) {
			if !s.ensureInitialized(c) {
				return
			}
			messagesHandler.Messages(c)
		})
	}

	// Catch-all for unsupported endpoints
	s.engine.NoRoute(func(c *gin.Context) {
		if utils.IsDebug() {
			utils.Debug("[API] 404 Not Found: %s %s", c.Request.Method, c.Request.URL.Path)
		}
		c.JSON(http.StatusNotFound, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "not_found_error",
				"message": fmt.Sprintf("Endpoint %s %s not found", c.Request.Method, c.Request.URL.Path),
			},
		})
	})
}

// Run starts the HTTP server
func (s *Server) Run(addr string) error {
	s.SetupRoutes()

	utils.Info("[Server] Starting on %s", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      s.engine,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Minute, // Long timeout for AI responses
		IdleTimeout:  120 * time.Second,
	}

	return srv.ListenAndServe()
}

// Engine returns the Gin engine for testing or custom configuration
func (s *Server) Engine() *gin.Engine {
	return s.engine
}

// GetAccountManager returns the account manager
func (s *Server) GetAccountManager() *account.Manager {
	return s.accountManager
}

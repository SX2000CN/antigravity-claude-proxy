// Package main provides the Antigravity Claude Proxy server.
// This file corresponds to src/index.js in the Node.js version.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/account/strategies"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/format"
	"github.com/poemonsense/antigravity-proxy-go/internal/modules"
	"github.com/poemonsense/antigravity-proxy-go/internal/server"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
	"github.com/poemonsense/antigravity-proxy-go/internal/webui"
	"github.com/poemonsense/antigravity-proxy-go/pkg/redis"
)

const version = "1.0.0"

func main() {
	// Parse command line flags
	var (
		debugMode    bool
		devMode      bool
		fallback     bool
		strategyName string
		port         int
		host         string
	)

	flag.BoolVar(&debugMode, "debug", false, "Enable debug mode (legacy alias for dev-mode)")
	flag.BoolVar(&devMode, "dev-mode", false, "Enable developer mode")
	flag.BoolVar(&fallback, "fallback", false, "Enable model fallback on quota exhaust")
	flag.StringVar(&strategyName, "strategy", "", "Account selection strategy (sticky/round-robin/hybrid)")
	flag.IntVar(&port, "port", 0, "Server port (default: 8080)")
	flag.StringVar(&host, "host", "", "Bind address (default: 0.0.0.0)")
	flag.Parse()

	// Environment variable overrides
	if os.Getenv("DEBUG") == "true" || os.Getenv("DEV_MODE") == "true" {
		devMode = true
	}
	if os.Getenv("FALLBACK") == "true" {
		fallback = true
	}
	if debugMode {
		devMode = true
	}

	// Port from environment
	if port == 0 {
		if envPort := os.Getenv("PORT"); envPort != "" {
			fmt.Sscanf(envPort, "%d", &port)
		}
	}
	if port == 0 {
		port = config.DefaultPort
	}

	// Host from environment
	if host == "" {
		host = os.Getenv("HOST")
	}
	if host == "" {
		host = "0.0.0.0"
	}

	// Validate strategy
	if strategyName != "" {
		validStrategies := []string{strategies.StrategySticky, strategies.StrategyRoundRobin, strategies.StrategyHybrid}
		valid := false
		for _, s := range validStrategies {
			if strings.ToLower(strategyName) == s {
				valid = true
				strategyName = s
				break
			}
		}
		if !valid {
			utils.Warn("[Startup] Invalid strategy \"%s\". Valid options: %s. Using default.",
				strategyName, strings.Join(validStrategies, ", "))
			strategyName = ""
		}
	}

	// Initialize logging
	utils.SetDebug(devMode)

	// Create runtime config
	cfg := config.DefaultConfig()
	if err := cfg.Load(); err != nil {
		utils.Warn("[Startup] Failed to load config: %v", err)
	}
	cfg.DevMode = devMode
	if devMode {
		utils.Debug("Developer mode enabled")
	}
	if fallback {
		utils.Info("Model fallback mode enabled")
	}

	// Initialize Redis client
	redisClient, err := redis.NewClient(redis.Config{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err != nil {
		utils.Error("[Startup] Failed to connect to Redis: %v", err)
		utils.Warn("[Startup] Starting without Redis - using in-memory storage")
		redisClient = nil
	}

	// Initialize signature cache
	format.InitGlobalSignatureCache(redisClient)

	// Initialize account manager
	accountManager := account.NewManager(redisClient, cfg)

	// Initialize usage stats
	usageStats := modules.NewUsageStats(redisClient)
	usageStats.Initialize()

	// Create HTTP server
	srv := server.New(cfg, accountManager, server.Options{
		FallbackEnabled:  fallback,
		StrategyOverride: strategyName,
		Debug:            devMode,
	})

	// Initialize server (and account manager)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := srv.Initialize(ctx); err != nil {
		utils.Error("[Startup] Failed to initialize server: %v", err)
		cancel()
		os.Exit(1)
	}
	cancel()

	// Set up routes
	srv.SetupRoutes()

	// Add usage stats middleware and routes
	engine := srv.Engine()
	engine.Use(usageStats.Middleware())

	// Mount WebUI
	webuiRouter := webui.NewRouter(accountManager, cfg, usageStats)
	publicDir := getPublicDir()
	webuiRouter.Mount(engine, publicDir)

	// Add stats API route to WebUI
	apiGroup := engine.Group("/api")
	usageStats.SetupRoutes(apiGroup)

	// Print startup banner
	printBanner(port, host, strategyName, devMode, fallback, accountManager, cfg)

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", host, port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      engine,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Minute, // Long timeout for AI responses
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		utils.Info("[Server] Starting on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			utils.Error("[Server] Failed to start: %v", err)
			os.Exit(1)
		}
	}()

	utils.Success("Server started successfully on port %d", port)
	if devMode {
		utils.Warn("Running in DEVELOPER mode - verbose logs enabled")
	}

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Graceful shutdown
	utils.Info("Shutting down server...")

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown usage stats
	usageStats.Shutdown()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(ctx); err != nil {
		utils.Error("Server forced to shutdown: %v", err)
		os.Exit(1)
	}

	// Close Redis connection
	if redisClient != nil {
		redisClient.Close()
	}

	utils.Success("Server stopped")
}

// getPublicDir returns the path to the public directory
func getPublicDir() string {
	// Try relative to executable
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Join(filepath.Dir(exe), "..", "public")
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}

	// Try relative to working directory
	cwd, err := os.Getwd()
	if err == nil {
		dir := filepath.Join(cwd, "public")
		if _, err := os.Stat(dir); err == nil {
			return dir
		}

		// Try parent directory (for when running from go-backend)
		dir = filepath.Join(cwd, "..", "public")
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}

	// Default
	return "public"
}

// printBanner prints the startup banner
func printBanner(port int, host, strategy string, devMode, fallback bool, am *account.Manager, cfg *config.Config) {
	// Clear console
	fmt.Print("\033[H\033[2J")

	status := am.GetStatus()
	strategyLabel := strategies.GetStrategyLabel(am.GetStrategyName())

	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".antigravity-claude-proxy")

	displayHost := host
	if host == "0.0.0.0" {
		displayHost = "localhost"
	}

	// Build status lines
	statusLines := []string{
		fmt.Sprintf("    ✓ Strategy: %s", strategyLabel),
		fmt.Sprintf("    ✓ Accounts: %s", status.Summary),
	}
	if devMode {
		statusLines = append(statusLines, "    ✓ Developer mode enabled")
	}
	if fallback {
		statusLines = append(statusLines, "    ✓ Model fallback enabled")
	}

	// Build control lines
	controlLines := []string{
		"    --strategy=<s>     Set account selection strategy",
		"                       (sticky/round-robin/hybrid)",
	}
	if !devMode {
		controlLines = append(controlLines, "    --dev-mode         Enable developer mode")
	}
	if !fallback {
		controlLines = append(controlLines, "    --fallback         Enable model fallback on quota exhaust")
	}
	controlLines = append(controlLines, "    Ctrl+C             Stop server")

	fmt.Println(`
╔══════════════════════════════════════════════════════════════╗
║            Antigravity Claude Proxy Server v` + version + `            ║
╠══════════════════════════════════════════════════════════════╣
║                                                              ║`)
	fmt.Printf("║  Server and WebUI running at: http://%s:%-15d ║\n", displayHost, port)
	fmt.Printf("║  Bound to: %s:%-42d ║\n", host, port)
	fmt.Println("║                                                              ║")
	fmt.Println("║  Active Modes:                                               ║")
	for _, line := range statusLines {
		fmt.Printf("║  %-60s ║\n", line)
	}
	fmt.Println("║                                                              ║")
	fmt.Println("║  Control:                                                    ║")
	for _, line := range controlLines {
		fmt.Printf("║  %-60s ║\n", line)
	}
	fmt.Println("║                                                              ║")
	fmt.Println("║  Endpoints:                                                  ║")
	fmt.Println("║    POST /v1/messages         - Anthropic Messages API        ║")
	fmt.Println("║    GET  /v1/models           - List available models         ║")
	fmt.Println("║    GET  /health              - Health check                  ║")
	fmt.Println("║    GET  /account-limits      - Account status & quotas       ║")
	fmt.Println("║    POST /refresh-token       - Force token refresh           ║")
	fmt.Println("║                                                              ║")
	fmt.Println("║  Configuration:                                              ║")
	fmt.Printf("║    Storage: %-50s ║\n", configDir)
	fmt.Println("║                                                              ║")
	fmt.Println("║  Usage with Claude Code:                                     ║")
	fmt.Printf("║    export ANTHROPIC_BASE_URL=http://localhost:%-15d ║\n", port)
	fmt.Printf("║    export ANTHROPIC_API_KEY=%-33s ║\n", cfg.APIKey)
	fmt.Println("║    claude                                                    ║")
	fmt.Println("║                                                              ║")
	fmt.Println("║  Add Google accounts:                                        ║")
	fmt.Println("║    antigravity-accounts add                                  ║")
	fmt.Println("║                                                              ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}

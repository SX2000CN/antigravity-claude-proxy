// Package auth provides SQLite database access for Antigravity state.
// This file corresponds to src/auth/database.js in the Node.js version.
//
// Uses modernc.org/sqlite for:
// - Windows compatibility (no CGO dependency)
// - Pure Go implementation
// - Cross-platform support
package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"

	_ "modernc.org/sqlite" // SQLite driver
)

// AuthStatusData represents the auth status data from the database
type AuthStatusData struct {
	APIKey string `json:"apiKey"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

// GetAuthStatus queries Antigravity database for authentication status
func GetAuthStatus(dbPath string) (*AuthStatusData, error) {
	if dbPath == "" {
		dbPath = config.AntigravityDBPath
	}

	// Check if database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("database not found at %s; make sure Antigravity is installed and you are logged in", dbPath)
	}

	// Open database in read-only mode
	// Use the ?mode=ro query parameter for read-only access
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Query for auth status
	var value string
	err = db.QueryRow("SELECT value FROM ItemTable WHERE key = 'antigravityAuthStatus'").Scan(&value)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no auth status found in database")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query database: %w", err)
	}

	// Parse JSON value
	var authData AuthStatusData
	if err := json.Unmarshal([]byte(value), &authData); err != nil {
		return nil, fmt.Errorf("failed to parse auth data: %w", err)
	}

	if authData.APIKey == "" {
		return nil, fmt.Errorf("auth data missing apiKey field")
	}

	return &authData, nil
}

// IsDatabaseAccessible checks if database exists and is accessible
func IsDatabaseAccessible(dbPath string) bool {
	if dbPath == "" {
		dbPath = config.AntigravityDBPath
	}

	// Check if file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return false
	}

	// Try to open the database
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		utils.Debug("[Database] Failed to open: %v", err)
		return false
	}
	defer db.Close()

	// Try to ping
	if err := db.Ping(); err != nil {
		utils.Debug("[Database] Failed to ping: %v", err)
		return false
	}

	return true
}

// Package utils provides utility functions for the Antigravity proxy.
// This file corresponds to src/utils/logger.js in the Node.js version.
package utils

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// ANSI color codes
const (
	colorReset   = "\033[0m"
	colorBright  = "\033[1m"
	colorDim     = "\033[2m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorWhite   = "\033[37m"
	colorGray    = "\033[90m"
)

// LogLevel represents the log level
type LogLevel string

const (
	LogLevelInfo    LogLevel = "INFO"
	LogLevelSuccess LogLevel = "SUCCESS"
	LogLevelWarn    LogLevel = "WARN"
	LogLevelError   LogLevel = "ERROR"
	LogLevelDebug   LogLevel = "DEBUG"
)

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp string   `json:"timestamp"`
	Level     LogLevel `json:"level"`
	Message   string   `json:"message"`
}

// LogListener is a function that receives log entries
type LogListener func(entry LogEntry)

// Logger provides structured logging with colors and debug support
type Logger struct {
	mu             sync.RWMutex
	isDebugEnabled bool
	history        []LogEntry
	maxHistory     int
	listeners      []LogListener
}

// NewLogger creates a new Logger instance
func NewLogger() *Logger {
	return &Logger{
		isDebugEnabled: false,
		history:        make([]LogEntry, 0),
		maxHistory:     1000,
		listeners:      make([]LogListener, 0),
	}
}

// SetDebug enables or disables debug mode
func (l *Logger) SetDebug(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.isDebugEnabled = enabled
}

// IsDebugEnabled returns whether debug mode is enabled
func (l *Logger) IsDebugEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.isDebugEnabled
}

// AddListener adds a log listener
func (l *Logger) AddListener(listener LogListener) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.listeners = append(l.listeners, listener)
}

// GetHistory returns the log history
func (l *Logger) GetHistory() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make([]LogEntry, len(l.history))
	copy(result, l.history)
	return result
}

// getTimestamp returns the current timestamp as an ISO8601 string
func (l *Logger) getTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// print formats and prints a log message
func (l *Logger) print(level LogLevel, color string, message string, args ...interface{}) {
	timestampStr := l.getTimestamp()
	timestamp := fmt.Sprintf("%s[%s]%s", colorGray, timestampStr, colorReset)
	levelTag := fmt.Sprintf("%s[%s]%s", color, level, colorReset)

	// Format the message with args
	formattedMessage := fmt.Sprintf(message, args...)

	fmt.Fprintf(os.Stdout, "%s %s %s\n", timestamp, levelTag, formattedMessage)

	// Store structured log
	entry := LogEntry{
		Timestamp: timestampStr,
		Level:     level,
		Message:   formattedMessage,
	}

	l.mu.Lock()
	l.history = append(l.history, entry)
	if len(l.history) > l.maxHistory {
		l.history = l.history[1:]
	}
	listeners := make([]LogListener, len(l.listeners))
	copy(listeners, l.listeners)
	l.mu.Unlock()

	// Notify listeners (outside the lock)
	for _, listener := range listeners {
		listener(entry)
	}
}

// Info logs a standard info message
func (l *Logger) Info(message string, args ...interface{}) {
	l.print(LogLevelInfo, colorBlue, message, args...)
}

// Success logs a success message
func (l *Logger) Success(message string, args ...interface{}) {
	l.print(LogLevelSuccess, colorGreen, message, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(message string, args ...interface{}) {
	l.print(LogLevelWarn, colorYellow, message, args...)
}

// Error logs an error message
func (l *Logger) Error(message string, args ...interface{}) {
	l.print(LogLevelError, colorRed, message, args...)
}

// Debug logs a debug message (only if debug mode is enabled)
func (l *Logger) Debug(message string, args ...interface{}) {
	if l.IsDebugEnabled() {
		l.print(LogLevelDebug, colorMagenta, message, args...)
	}
}

// Log prints a raw message without formatting (proxied to stdout)
func (l *Logger) Log(message string, args ...interface{}) {
	fmt.Printf(message, args...)
	fmt.Println()
}

// Header prints a section header
func (l *Logger) Header(title string) {
	fmt.Printf("\n%s%s=== %s ===%s\n\n", colorBright, colorCyan, title, colorReset)
}

// Global logger instance (singleton pattern matching Node.js version)
var (
	globalLogger     *Logger
	globalLoggerOnce sync.Once
)

// GetLogger returns the global logger instance
func GetLogger() *Logger {
	globalLoggerOnce.Do(func() {
		globalLogger = NewLogger()
	})
	return globalLogger
}

// Convenience functions using the global logger

// Info logs a standard info message using the global logger
func Info(message string, args ...interface{}) {
	GetLogger().Info(message, args...)
}

// Success logs a success message using the global logger
func Success(message string, args ...interface{}) {
	GetLogger().Success(message, args...)
}

// Warn logs a warning message using the global logger
func Warn(message string, args ...interface{}) {
	GetLogger().Warn(message, args...)
}

// Error logs an error message using the global logger
func Error(message string, args ...interface{}) {
	GetLogger().Error(message, args...)
}

// Debug logs a debug message using the global logger
func Debug(message string, args ...interface{}) {
	GetLogger().Debug(message, args...)
}

// SetDebug enables or disables debug mode on the global logger
func SetDebug(enabled bool) {
	GetLogger().SetDebug(enabled)
}

// IsDebug returns whether debug mode is enabled on the global logger
func IsDebug() bool {
	return GetLogger().IsDebugEnabled()
}

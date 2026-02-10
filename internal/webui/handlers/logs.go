// Package handlers provides HTTP handlers for the WebUI.
// This file corresponds to log-related handlers in src/webui/index.js in the Node.js version.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// LogsHandler handles log-related API endpoints
type LogsHandler struct{}

// NewLogsHandler creates a new LogsHandler
func NewLogsHandler() *LogsHandler {
	return &LogsHandler{}
}

// GetLogs handles GET /api/logs
func (h *LogsHandler) GetLogs(c *gin.Context) {
	logger := utils.GetLogger()
	history := logger.GetHistory()

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"logs":   history,
	})
}

// StreamLogs handles GET /api/logs/stream
func (h *LogsHandler) StreamLogs(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	logger := utils.GetLogger()

	// Send recent history if requested
	if c.Query("history") == "true" {
		history := logger.GetHistory()
		for _, log := range history {
			data, err := json.Marshal(log)
			if err == nil {
				c.Writer.Write([]byte("data: " + string(data) + "\n\n"))
			}
		}
		c.Writer.Flush()
	}

	// Create a channel for log events
	logChan := make(chan utils.LogEntry, 100)

	// Subscribe to log events
	listener := func(entry utils.LogEntry) {
		select {
		case logChan <- entry:
		default:
			// Channel full, skip this log
		}
	}
	logger.AddListener(listener)

	// Stream logs until client disconnects
	clientGone := c.Request.Context().Done()
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "Streaming not supported",
		})
		return
	}

	for {
		select {
		case <-clientGone:
			// Client disconnected
			return
		case log := <-logChan:
			data, err := json.Marshal(log)
			if err == nil {
				c.Writer.Write([]byte("data: " + string(data) + "\n\n"))
				flusher.Flush()
			}
		}
	}
}

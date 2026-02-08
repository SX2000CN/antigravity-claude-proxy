// Package sse provides Server-Sent Events (SSE) response writing utilities.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Writer wraps an http.ResponseWriter for SSE streaming
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter creates a new SSE writer
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
	}

	return &Writer{
		w:       w,
		flusher: flusher,
	}, nil
}

// SetHeaders sets the SSE response headers
func (sw *Writer) SetHeaders() {
	sw.w.Header().Set("Content-Type", "text/event-stream")
	sw.w.Header().Set("Cache-Control", "no-cache")
	sw.w.Header().Set("Connection", "keep-alive")
	sw.w.Header().Set("X-Accel-Buffering", "no")
}

// WriteEvent writes an SSE event with the given type and data
func (sw *Writer) WriteEvent(eventType string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", eventType, jsonData)
	if err != nil {
		return err
	}

	sw.flusher.Flush()
	return nil
}

// WriteRaw writes raw data as an SSE event
func (sw *Writer) WriteRaw(eventType string, jsonData []byte) error {
	_, err := fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", eventType, jsonData)
	if err != nil {
		return err
	}

	sw.flusher.Flush()
	return nil
}

// WriteError writes an error event
func (sw *Writer) WriteError(errorType, message string) error {
	errorData := map[string]interface{}{
		"type": "error",
		"error": map[string]string{
			"type":    errorType,
			"message": message,
		},
	}
	return sw.WriteEvent("error", errorData)
}

// Flush flushes any buffered data
func (sw *Writer) Flush() {
	sw.flusher.Flush()
}

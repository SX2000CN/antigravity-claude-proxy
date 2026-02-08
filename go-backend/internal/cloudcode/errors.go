// Package cloudcode provides Cloud Code API client implementation.
// This file contains error types for the cloudcode package.
package cloudcode

// EmptyResponseError represents an error when no content was received from the API
type EmptyResponseError struct {
	Message string
}

// NewEmptyResponseError creates a new EmptyResponseError
func NewEmptyResponseError(message string) *EmptyResponseError {
	return &EmptyResponseError{Message: message}
}

// Error implements the error interface
func (e *EmptyResponseError) Error() string {
	return e.Message
}

// IsEmptyResponseError checks if an error is an EmptyResponseError
func IsEmptyResponseError(err error) bool {
	_, ok := err.(*EmptyResponseError)
	return ok
}

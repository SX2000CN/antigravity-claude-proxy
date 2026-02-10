// Package errors provides custom error types for the Antigravity proxy.
// This file corresponds to src/errors.js in the Node.js version.
package errors

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AntigravityError is the base error class for Antigravity proxy errors
type AntigravityError struct {
	Message   string                 `json:"message"`
	Code      string                 `json:"code"`
	Retryable bool                   `json:"retryable"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

func (e *AntigravityError) Error() string {
	return e.Message
}

// ToJSON converts the error to JSON for API responses
func (e *AntigravityError) ToJSON() map[string]interface{} {
	result := map[string]interface{}{
		"name":      "AntigravityError",
		"code":      e.Code,
		"message":   e.Message,
		"retryable": e.Retryable,
	}
	for k, v := range e.Metadata {
		result[k] = v
	}
	return result
}

// MarshalJSON implements json.Marshaler
func (e *AntigravityError) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.ToJSON())
}

// NewAntigravityError creates a new AntigravityError
func NewAntigravityError(message, code string, retryable bool, metadata map[string]interface{}) *AntigravityError {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	return &AntigravityError{
		Message:   message,
		Code:      code,
		Retryable: retryable,
		Metadata:  metadata,
	}
}

// RateLimitError represents a rate limit error (429 / RESOURCE_EXHAUSTED)
type RateLimitError struct {
	*AntigravityError
	ResetMs      *int64  `json:"resetMs,omitempty"`
	AccountEmail string  `json:"accountEmail,omitempty"`
}

// NewRateLimitError creates a new RateLimitError
func NewRateLimitError(message string, resetMs *int64, accountEmail string) *RateLimitError {
	metadata := map[string]interface{}{}
	if resetMs != nil {
		metadata["resetMs"] = *resetMs
	}
	if accountEmail != "" {
		metadata["accountEmail"] = accountEmail
	}
	return &RateLimitError{
		AntigravityError: &AntigravityError{
			Message:   message,
			Code:      "RATE_LIMITED",
			Retryable: true,
			Metadata:  metadata,
		},
		ResetMs:      resetMs,
		AccountEmail: accountEmail,
	}
}

// AuthError represents an authentication error
type AuthError struct {
	*AntigravityError
	AccountEmail string `json:"accountEmail,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// NewAuthError creates a new AuthError
func NewAuthError(message, accountEmail, reason string) *AuthError {
	metadata := map[string]interface{}{}
	if accountEmail != "" {
		metadata["accountEmail"] = accountEmail
	}
	if reason != "" {
		metadata["reason"] = reason
	}
	return &AuthError{
		AntigravityError: &AntigravityError{
			Message:   message,
			Code:      "AUTH_INVALID",
			Retryable: false,
			Metadata:  metadata,
		},
		AccountEmail: accountEmail,
		Reason:       reason,
	}
}

// NoAccountsError represents no accounts available error
type NoAccountsError struct {
	*AntigravityError
	AllRateLimited bool `json:"allRateLimited"`
}

// NewNoAccountsError creates a new NoAccountsError
func NewNoAccountsError(message string, allRateLimited bool) *NoAccountsError {
	if message == "" {
		message = "No accounts available"
	}
	return &NoAccountsError{
		AntigravityError: &AntigravityError{
			Message:   message,
			Code:      "NO_ACCOUNTS",
			Retryable: allRateLimited,
			Metadata: map[string]interface{}{
				"allRateLimited": allRateLimited,
			},
		},
		AllRateLimited: allRateLimited,
	}
}

// MaxRetriesError represents max retries exceeded error
type MaxRetriesError struct {
	*AntigravityError
	Attempts int `json:"attempts"`
}

// NewMaxRetriesError creates a new MaxRetriesError
func NewMaxRetriesError(message string, attempts int) *MaxRetriesError {
	if message == "" {
		message = "Max retries exceeded"
	}
	return &MaxRetriesError{
		AntigravityError: &AntigravityError{
			Message:   message,
			Code:      "MAX_RETRIES",
			Retryable: false,
			Metadata: map[string]interface{}{
				"attempts": attempts,
			},
		},
		Attempts: attempts,
	}
}

// ApiError represents an API error from upstream service
type ApiError struct {
	*AntigravityError
	StatusCode int    `json:"statusCode"`
	ErrorType  string `json:"errorType"`
}

// NewApiError creates a new ApiError
func NewApiError(message string, statusCode int, errorType string) *ApiError {
	if errorType == "" {
		errorType = "api_error"
	}
	return &ApiError{
		AntigravityError: &AntigravityError{
			Message:   message,
			Code:      strings.ToUpper(errorType),
			Retryable: statusCode >= 500,
			Metadata: map[string]interface{}{
				"statusCode": statusCode,
				"errorType":  errorType,
			},
		},
		StatusCode: statusCode,
		ErrorType:  errorType,
	}
}

// NativeModuleError represents a native module error
type NativeModuleError struct {
	*AntigravityError
	RebuildSucceeded bool `json:"rebuildSucceeded"`
	RestartRequired  bool `json:"restartRequired"`
}

// NewNativeModuleError creates a new NativeModuleError
func NewNativeModuleError(message string, rebuildSucceeded, restartRequired bool) *NativeModuleError {
	return &NativeModuleError{
		AntigravityError: &AntigravityError{
			Message:   message,
			Code:      "NATIVE_MODULE_ERROR",
			Retryable: false,
			Metadata: map[string]interface{}{
				"rebuildSucceeded": rebuildSucceeded,
				"restartRequired":  restartRequired,
			},
		},
		RebuildSucceeded: rebuildSucceeded,
		RestartRequired:  restartRequired,
	}
}

// EmptyResponseError represents an empty response error
type EmptyResponseError struct {
	*AntigravityError
}

// NewEmptyResponseError creates a new EmptyResponseError
func NewEmptyResponseError(message string) *EmptyResponseError {
	if message == "" {
		message = "No content received from API"
	}
	return &EmptyResponseError{
		AntigravityError: &AntigravityError{
			Message:   message,
			Code:      "EMPTY_RESPONSE",
			Retryable: true,
			Metadata:  make(map[string]interface{}),
		},
	}
}

// CapacityExhaustedError represents a capacity exhausted error
type CapacityExhaustedError struct {
	*AntigravityError
	RetryAfterMs *int64 `json:"retryAfterMs,omitempty"`
}

// NewCapacityExhaustedError creates a new CapacityExhaustedError
func NewCapacityExhaustedError(message string, retryAfterMs *int64) *CapacityExhaustedError {
	if message == "" {
		message = "Model capacity exhausted"
	}
	metadata := map[string]interface{}{}
	if retryAfterMs != nil {
		metadata["retryAfterMs"] = *retryAfterMs
	}
	return &CapacityExhaustedError{
		AntigravityError: &AntigravityError{
			Message:   message,
			Code:      "CAPACITY_EXHAUSTED",
			Retryable: true,
			Metadata:  metadata,
		},
		RetryAfterMs: retryAfterMs,
	}
}

// Error checking functions

// IsRateLimitError checks if an error is a rate limit error
func IsRateLimitError(err error) bool {
	if _, ok := err.(*RateLimitError); ok {
		return true
	}
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "quota_exhausted") ||
		strings.Contains(msg, "rate limit")
}

// IsAuthError checks if an error is an authentication error
func IsAuthError(err error) bool {
	if _, ok := err.(*AuthError); ok {
		return true
	}
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "AUTH_INVALID") ||
		strings.Contains(msg, "INVALID_GRANT") ||
		strings.Contains(msg, "TOKEN REFRESH FAILED")
}

// IsEmptyResponseError checks if an error is an empty response error
func IsEmptyResponseError(err error) bool {
	if _, ok := err.(*EmptyResponseError); ok {
		return true
	}
	if ag, ok := err.(*AntigravityError); ok {
		return ag.Code == "EMPTY_RESPONSE"
	}
	return false
}

// IsCapacityExhaustedError checks if an error is a capacity exhausted error
func IsCapacityExhaustedError(err error) bool {
	if _, ok := err.(*CapacityExhaustedError); ok {
		return true
	}
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "model_capacity_exhausted") ||
		strings.Contains(msg, "capacity_exhausted") ||
		strings.Contains(msg, "model is currently overloaded") ||
		strings.Contains(msg, "service temporarily unavailable")
}

// WrapError wraps a standard error with AntigravityError
func WrapError(err error, code string, retryable bool) *AntigravityError {
	if err == nil {
		return nil
	}
	return NewAntigravityError(err.Error(), code, retryable, nil)
}

// FormatAPIError formats an error for API response
func FormatAPIError(err error) map[string]interface{} {
	if ae, ok := err.(*AntigravityError); ok {
		return ae.ToJSON()
	}
	if re, ok := err.(*RateLimitError); ok {
		return re.ToJSON()
	}
	if au, ok := err.(*AuthError); ok {
		return au.ToJSON()
	}
	if na, ok := err.(*NoAccountsError); ok {
		return na.ToJSON()
	}
	if mr, ok := err.(*MaxRetriesError); ok {
		return mr.ToJSON()
	}
	if ap, ok := err.(*ApiError); ok {
		return ap.ToJSON()
	}
	if er, ok := err.(*EmptyResponseError); ok {
		return er.ToJSON()
	}
	if ce, ok := err.(*CapacityExhaustedError); ok {
		return ce.ToJSON()
	}

	// Generic error
	return map[string]interface{}{
		"type":    "error",
		"error":   map[string]interface{}{
			"type":    "internal_error",
			"message": err.Error(),
		},
	}
}

// HTTPStatusFromError returns the appropriate HTTP status code for an error
func HTTPStatusFromError(err error) int {
	switch e := err.(type) {
	case *RateLimitError:
		return 429
	case *AuthError:
		return 401
	case *NoAccountsError:
		if e.AllRateLimited {
			return 429
		}
		return 503
	case *MaxRetriesError:
		return 503
	case *ApiError:
		return e.StatusCode
	case *EmptyResponseError:
		return 502
	case *CapacityExhaustedError:
		return 503
	default:
		return 500
	}
}

// ErrorWithContext adds context to an error
func ErrorWithContext(err error, context string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", context, err)
}

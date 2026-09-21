package typesafe

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

var (
	// ErrAPI matches APIError.
	ErrAPI = errors.New("typesafe API error")
	// ErrProtocol matches ProtocolError.
	ErrProtocol = errors.New("typesafe protocol error")
	// ErrResponseTooLarge matches ResponseTooLargeError.
	ErrResponseTooLarge = errors.New("typesafe response too large")
	// ErrConnection matches ConnectionError.
	ErrConnection = errors.New("typesafe connection error")
	// ErrAttemptTimeout matches AttemptTimeoutError.
	ErrAttemptTimeout = errors.New("typesafe attempt timeout")
)

// ValidationError reports a payload-free constructor or request validation failure.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return "typesafe: invalid " + safeField(e.Field) + ": " + safeReason(e.Reason)
}

// APIError reports a non-2xx API response. Response bodies are never retained.
type APIError struct {
	StatusCode int
	RequestID  string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if text := http.StatusText(e.StatusCode); text != "" {
		return fmt.Sprintf("typesafe: API returned %d %s", e.StatusCode, text)
	}
	return fmt.Sprintf("typesafe: API returned status %d", e.StatusCode)
}
func (e *APIError) Is(target error) bool { return target == ErrAPI }

// ProtocolError reports a malformed service response. Reason is a fixed SDK
// classification; decoder errors and response bodies are not retained.
type ProtocolError struct {
	RequestID string
	Field     string
	Reason    string
}

func (e *ProtocolError) Error() string {
	if e.Field == "" {
		return "typesafe: invalid service response"
	}
	return "typesafe: invalid service response field " + safeField(e.Field)
}
func (e *ProtocolError) Is(target error) bool { return target == ErrProtocol }

func safeReason(reason string) string {
	switch reason {
	case "blank", "missing", "nil", "unsupported", "invalid JSON", "invalid value", "out of range":
		return reason
	default:
		return "invalid value"
	}
}

func safeField(field string) string {
	for _, r := range field {
		if !(r == '.' || r == '[' || r == ']' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return "response"
		}
	}
	if len(field) > 96 {
		return "response"
	}
	return field
}

// ResponseTooLargeError reports a decompressed body beyond the configured limit.
type ResponseTooLargeError struct{ Limit int64 }

func (e *ResponseTooLargeError) Error() string        { return "typesafe: response exceeds configured limit" }
func (e *ResponseTooLargeError) Is(target error) bool { return target == ErrResponseTooLarge }

// ConnectionError reports a transport or response-body I/O failure.
type ConnectionError struct{ cause error }

func (e *ConnectionError) Error() string        { return "typesafe: connection failed" }
func (e *ConnectionError) Unwrap() error        { return e.cause }
func (e *ConnectionError) Is(target error) bool { return target == ErrConnection }

// AttemptTimeoutError reports expiration of the per-attempt timeout.
type AttemptTimeoutError struct{ cause error }

func (e *AttemptTimeoutError) Error() string        { return "typesafe: attempt timed out" }
func (e *AttemptTimeoutError) Unwrap() error        { return e.cause }
func (e *AttemptTimeoutError) Is(target error) bool { return target == ErrAttemptTimeout }

func formatError(s fmt.State, verb rune, err error) {
	if verb == 'q' {
		fmt.Fprintf(s, "%q", err.Error())
		return
	}
	fmt.Fprint(s, err.Error())
}

func (e *ValidationError) Format(s fmt.State, verb rune)       { formatError(s, verb, e) }
func (e *APIError) Format(s fmt.State, verb rune)              { formatError(s, verb, e) }
func (e *ProtocolError) Format(s fmt.State, verb rune)         { formatError(s, verb, e) }
func (e *ResponseTooLargeError) Format(s fmt.State, verb rune) { formatError(s, verb, e) }
func (e *ConnectionError) Format(s fmt.State, verb rune)       { formatError(s, verb, e) }
func (e *AttemptTimeoutError) Format(s fmt.State, verb rune)   { formatError(s, verb, e) }

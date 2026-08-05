// Package errors defines the platform's typed application error and the standard
// API error envelope. See docs/04-api-spec.md for the canonical code list.
//
// An *Error carries a stable machine-readable Code, a human message, optional
// details, and an optional wrapped cause. Codes map deterministically to HTTP
// statuses so every service responds consistently.
package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
)

// Code is a stable, machine-readable error identifier.
type Code string

const (
	CodeUnauthenticated  Code = "UNAUTHENTICATED"
	CodeForbidden        Code = "FORBIDDEN"
	CodeNotFound         Code = "NOT_FOUND"
	CodeValidationFailed Code = "VALIDATION_FAILED"
	CodeValidation       Code = "VALIDATION_FAILED" // Alias for CodeValidationFailed
	CodeConflict         Code = "CONFLICT"
	CodeRateLimited      Code = "RATE_LIMITED"
	CodeInternal         Code = "INTERNAL"
)

// Error is the platform's typed application error.
type Error struct {
	Code    Code
	Message string
	Details []string
	// status optionally overrides the HTTP status derived from Code. It lets
	// services use domain-specific codes (e.g. "ACCOUNT_LOCKED") while still
	// returning the correct HTTP status. Zero means "derive from Code".
	status int
	cause  error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the wrapped cause for errors.Is/As.
func (e *Error) Unwrap() error { return e.cause }

// WithDetails returns a copy of the error with additional details appended.
func (e *Error) WithDetails(details ...string) *Error {
	cp := *e
	cp.Details = append(append([]string{}, e.Details...), details...)
	return &cp
}

// HTTPStatus returns the HTTP status code for this error: the explicit override
// when set, otherwise the status derived from Code.
func (e *Error) HTTPStatus() int {
	if e.status != 0 {
		return e.status
	}
	return HTTPStatus(e.Code)
}

// New constructs a new application error.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// NewWithStatus constructs an application error with a domain-specific code and
// an explicit HTTP status. Use it when the standard Code->status mapping does
// not apply (e.g. "ACCOUNT_LOCKED" -> 423).
func NewWithStatus(code Code, status int, message string) *Error {
	return &Error{Code: code, Message: message, status: status}
}

// Newf constructs a new application error with a formatted message.
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap annotates an existing error with a code and message.
func Wrap(err error, code Code, message string) *Error {
	return &Error{Code: code, Message: message, cause: err}
}

// From coerces any error into an *Error. If err is already an *Error it is
// returned unchanged; otherwise it becomes an INTERNAL error wrapping the cause.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if stderrors.As(err, &appErr) {
		return appErr
	}
	return &Error{Code: CodeInternal, Message: "internal error", cause: err}
}

// HTTPStatus maps an error Code to its HTTP status.
func HTTPStatus(c Code) int {
	switch c {
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeValidationFailed:
		return http.StatusUnprocessableEntity
	case CodeConflict:
		return http.StatusConflict
	case CodeRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// Envelope is the JSON body returned for any error response.
type Envelope struct {
	Error Detail `json:"error"`
}

// Detail carries the error specifics returned to clients.
type Detail struct {
	Code      Code     `json:"code"`
	Message   string   `json:"message"`
	Details   []string `json:"details,omitempty"`
	RequestID string   `json:"requestId,omitempty"`
}

// Envelope renders the error as an API response body. The cause is intentionally
// omitted so internal details never leak to clients.
func (e *Error) Envelope(requestID string) Envelope {
	return Envelope{Error: Detail{
		Code:      e.Code,
		Message:   e.Message,
		Details:   e.Details,
		RequestID: requestID,
	}}
}

// Convenience constructors for common cases.

func Unauthenticated(message string) *Error { return New(CodeUnauthenticated, message) }
func Unauthorized(message string) *Error    { return New(CodeUnauthenticated, message) } // Alias for Unauthenticated
func Forbidden(message string) *Error       { return New(CodeForbidden, message) }
func NotFound(message string) *Error        { return New(CodeNotFound, message) }
func Validation(message string) *Error      { return New(CodeValidationFailed, message) }
func Conflict(message string) *Error        { return New(CodeConflict, message) }
func RateLimited(message string) *Error     { return New(CodeRateLimited, message) }
func Internal(message string) *Error        { return New(CodeInternal, message) }

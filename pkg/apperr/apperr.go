// Package apperr defines typed business errors mapped to HTTP statuses.
// Error codes are snake_case and double as i18n message keys (errors.<code>).
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

type Error struct {
	Code       string // machine-readable snake_case code, e.g. "insufficient_tokens"
	Message    string // English fallback message; localized via i18n key errors.<code>
	HTTPStatus int
	Meta       map[string]any // optional template data for i18n
	cause      error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// WithCause attaches an underlying error for logs without changing the API response.
func (e *Error) WithCause(err error) *Error {
	clone := *e
	clone.cause = err
	return &clone
}

// WithMeta attaches template data used when localizing the message.
func (e *Error) WithMeta(meta map[string]any) *Error {
	clone := *e
	clone.Meta = meta
	return &clone
}

func New(code, message string, status int) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: status}
}

func NotFound(code, message string) *Error     { return New(code, message, http.StatusNotFound) }
func Unauthorized(code, message string) *Error { return New(code, message, http.StatusUnauthorized) }
func Forbidden(code, message string) *Error    { return New(code, message, http.StatusForbidden) }
func Conflict(code, message string) *Error     { return New(code, message, http.StatusConflict) }
func Validation(message string) *Error {
	return New("validation_failed", message, http.StatusUnprocessableEntity)
}
func PaymentRequired(code, message string) *Error {
	return New(code, message, http.StatusPaymentRequired)
}
func TooManyRequests(message string) *Error {
	return New("rate_limited", message, http.StatusTooManyRequests)
}
func Internal(err error) *Error {
	return New("internal_error", "an internal error occurred", http.StatusInternalServerError).WithCause(err)
}

// From converts any error into an *Error, wrapping unknown errors as internal.
func From(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal(err)
}

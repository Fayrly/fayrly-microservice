// Package apierr defines the API error envelope used by every service.
package apierr

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is an error that maps directly onto an HTTP response.
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func BadRequest(message string) *Error {
	return New(http.StatusBadRequest, "bad_request", message)
}

func Validation(details map[string]string) *Error {
	return &Error{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_failed",
		Message: "the request contains invalid fields",
		Details: details,
	}
}

func Unauthorized(message string) *Error {
	if message == "" {
		message = "authentication is required"
	}
	return New(http.StatusUnauthorized, "unauthorized", message)
}

func Forbidden(message string) *Error {
	if message == "" {
		message = "you do not have permission to perform this action"
	}
	return New(http.StatusForbidden, "forbidden", message)
}

func NotFound(resource string) *Error {
	return New(http.StatusNotFound, "not_found", resource+" was not found")
}

func Conflict(message string) *Error {
	return New(http.StatusConflict, "conflict", message)
}

func TooManyRequests(message string) *Error {
	if message == "" {
		message = "too many requests, please retry later"
	}
	return New(http.StatusTooManyRequests, "rate_limited", message)
}

func ServiceUnavailable(message string) *Error {
	if message == "" {
		message = "a dependency is unavailable, please retry later"
	}
	return New(http.StatusServiceUnavailable, "service_unavailable", message)
}

func Internal(cause error) *Error {
	return &Error{
		Status:  http.StatusInternalServerError,
		Code:    "internal_error",
		Message: "an unexpected error occurred",
		Cause:   cause,
	}
}

// From returns err if it is already an *Error, otherwise wraps it as an internal error.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return Internal(err)
}

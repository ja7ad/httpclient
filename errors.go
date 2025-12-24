package httpclient

import (
	"fmt"
	"net/http"
	"time"
)

type ErrorType string

const (
	ErrorTypeTimeout        ErrorType = "timeout"
	ErrorTypeNetwork        ErrorType = "network"
	ErrorTypeHTTP           ErrorType = "http"
	ErrorTypeRetryExhausted ErrorType = "retry_exhausted"
	ErrorTypeValidation     ErrorType = "validation"
	ErrorTypeUnknown        ErrorType = "unknown"
)

type Error struct {
	Type       ErrorType
	Message    string
	StatusCode int
	URL        string
	Method     string
	RequestID  string
	Err        error
	Timestamp  time.Time
}

func (e *Error) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("[%s] %s: %s (requestID: %s)", e.Type, e.Message, e.URL, e.RequestID)
	}
	return fmt.Sprintf("[%s] %s: %s", e.Type, e.Message, e.URL)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func newError(errType ErrorType, message, url, method, requestID string, err error) *Error {
	return &Error{
		Type:      errType,
		Message:   message,
		URL:       url,
		Method:    method,
		RequestID: requestID,
		Err:       err,
		Timestamp: time.Now(),
	}
}

func newHTTPError(statusCode int, url, method, requestID string) *Error {
	return &Error{
		Type:       ErrorTypeHTTP,
		Message:    fmt.Sprintf("HTTP %d %s", statusCode, http.StatusText(statusCode)),
		StatusCode: statusCode,
		URL:        url,
		Method:     method,
		RequestID:  requestID,
		Timestamp:  time.Now(),
	}
}

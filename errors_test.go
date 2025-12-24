package httpclient

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		expected string
	}{
		{
			name: "with request id",
			err: &Error{
				Type:      ErrorTypeTimeout,
				Message:   "request timeout",
				URL:       "https://api.example.com",
				RequestID: "req-123",
			},
			expected: "[timeout] request timeout: https://api.example.com (requestID: req-123)",
		},
		{
			name: "without request id",
			err: &Error{
				Type:    ErrorTypeNetwork,
				Message: "connection refused",
				URL:     "https://api.example.com",
			},
			expected: "[network] connection refused: https://api.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	originalErr := errors.New("original error")
	err := &Error{
		Type:    ErrorTypeUnknown,
		Message: "wrapped error",
		Err:     originalErr,
	}

	assert.Equal(t, originalErr, err.Unwrap())
}

func TestNewError(t *testing.T) {
	originalErr := errors.New("test error")
	err := newError(
		ErrorTypeNetwork,
		"network failure",
		"https://api.test.com",
		"GET",
		"req-456",
		originalErr,
	)

	require.NotNil(t, err)
	assert.Equal(t, ErrorTypeNetwork, err.Type)
	assert.Equal(t, "network failure", err.Message)
	assert.Equal(t, "https://api.test.com", err.URL)
	assert.Equal(t, "GET", err.Method)
	assert.Equal(t, "req-456", err.RequestID)
	assert.Equal(t, originalErr, err.Err)
	assert.WithinDuration(t, time.Now(), err.Timestamp, time.Second)
}

func TestNewHTTPError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		url        string
		method     string
		requestID  string
	}{
		{
			name:       "404 not found",
			statusCode: 404,
			url:        "https://api.test.com/users/123",
			method:     "GET",
			requestID:  "req-789",
		},
		{
			name:       "500 internal server error",
			statusCode: 500,
			url:        "https://api.test.com/data",
			method:     "POST",
			requestID:  "req-999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newHTTPError(tt.statusCode, tt.url, tt.method, tt.requestID)

			require.NotNil(t, err)
			assert.Equal(t, ErrorTypeHTTP, err.Type)
			assert.Equal(t, tt.statusCode, err.StatusCode)
			assert.Equal(t, tt.url, err.URL)
			assert.Equal(t, tt.method, err.Method)
			assert.Equal(t, tt.requestID, err.RequestID)
			assert.WithinDuration(t, time.Now(), err.Timestamp, time.Second)
		})
	}
}

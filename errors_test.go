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

func TestErrorParsers(t *testing.T) {
	tests := []struct {
		name            string
		parser          ErrorResponseParser
		body            string
		expectedMessage string
		expectedCode    string
		expectedCorr    string
	}{
		{
			name:   "sumsub error",
			parser: &SumsubErrorParser{},
			body: `{
				"description": "Invalid id '12313213'",
				"code": 400,
				"correlationId": "36d4c16cd06b6f673976b30000000000"
			}`,
			expectedMessage: "Invalid id '12313213'",
			expectedCode:    "400",
			expectedCorr:    "36d4c16cd06b6f673976b30000000000",
		},
		{
			name:   "stripe error",
			parser: &StripeErrorParser{},
			body: `{
				"error": {
					"message": "Invalid card number",
					"type": "card_error",
					"code": "incorrect_number",
					"param": "number"
				}
			}`,
			expectedMessage: "Invalid card number",
			expectedCode:    "incorrect_number",
		},
		{
			name:   "rfc7807 error",
			parser: &RFC7807ErrorParser{},
			body: `{
				"type": "https://example.com/probs/out-of-credit",
				"title": "You do not have enough credit",
				"status": 403,
				"detail": "Your current balance is 30, but that costs 50",
				"instance": "/account/12345/msgs/abc"
			}`,
			expectedMessage: "Your current balance is 30, but that costs 50",
			expectedCode:    "403",
		},
		{
			name:   "generic error",
			parser: &GenericErrorParser{},
			body: `{
				"error": "Resource not found",
				"status_code": 404
			}`,
			expectedMessage: "Resource not found",
			expectedCode:    "404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := tt.parser.Parse([]byte(tt.body))
			require.NoError(t, err)
			require.NotNil(t, parsed)

			assert.Equal(t, tt.expectedMessage, parsed.Message)
			assert.Equal(t, tt.expectedCode, parsed.ErrorCode)
			if tt.expectedCorr != "" {
				assert.Equal(t, tt.expectedCorr, parsed.CorrelationID)
			}
		})
	}
}

func TestErrorParserChain(t *testing.T) {
	chain := NewErrorParserChain()

	tests := []struct {
		name            string
		body            string
		expectedMessage string
		expectedCode    string
	}{
		{
			name: "sumsub format",
			body: `{
				"description": "Invalid id",
				"code": 400,
				"correlationId": "abc123"
			}`,
			expectedMessage: "Invalid id",
			expectedCode:    "400",
		},
		{
			name: "stripe format",
			body: `{
				"error": {
					"message": "Card declined",
					"code": "card_declined"
				}
			}`,
			expectedMessage: "Card declined",
			expectedCode:    "card_declined",
		},
		{
			name: "generic format",
			body: `{
				"message": "Something went wrong",
				"status": 500
			}`,
			expectedMessage: "Something went wrong",
			expectedCode:    "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := chain.Parse([]byte(tt.body))
			require.NoError(t, err)
			require.NotNil(t, parsed)

			assert.Equal(t, tt.expectedMessage, parsed.Message)
			if tt.expectedCode != "" {
				assert.Equal(t, tt.expectedCode, parsed.ErrorCode)
			}
		})
	}
}

func TestError_GetDetail(t *testing.T) {
	err := &Error{
		Details: map[string]interface{}{
			"field":  "email",
			"reason": "invalid format",
			"code":   400,
		},
	}

	val, ok := err.GetDetail("field")
	assert.True(t, ok)
	assert.Equal(t, "email", val)

	val, ok = err.GetDetail("reason")
	assert.True(t, ok)
	assert.Equal(t, "invalid format", val)

	_, ok = err.GetDetail("nonexistent")
	assert.False(t, ok)

	assert.True(t, err.HasDetail("field"))
	assert.False(t, err.HasDetail("nonexistent"))
}

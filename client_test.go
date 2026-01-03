package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testResponse struct {
	Message string `json:"message"`
	ID      int    `json:"id"`
}

type testRequest struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		opts    []ClientOption
		asserts func(t *testing.T, c *Client)
	}{
		{
			name: "default client",
			opts: []ClientOption{},
			asserts: func(t *testing.T, c *Client) {
				assert.NotNil(t, c)
				assert.NotNil(t, c.httpClient)
				assert.Equal(t, "", c.baseURL)
				assert.Equal(t, 30*time.Second, c.timeout)
			},
		},
		{
			name: "with base url",
			opts: []ClientOption{
				WithBaseURL("https://api.example.com"),
			},
			asserts: func(t *testing.T, c *Client) {
				assert.Equal(t, "https://api.example.com", c.baseURL)
			},
		},
		{
			name: "with timeout",
			opts: []ClientOption{
				WithTimeout(10 * time.Second),
			},
			asserts: func(t *testing.T, c *Client) {
				assert.Equal(t, 10*time.Second, c.timeout)
			},
		},
		{
			name: "with headers",
			opts: []ClientOption{
				WithHeaders(map[string]string{
					"Authorization": "Bearer token",
					"X-Custom":      "value",
				}),
			},
			asserts: func(t *testing.T, c *Client) {
				assert.Equal(t, "Bearer token", c.headers["Authorization"])
				assert.Equal(t, "value", c.headers["X-Custom"])
			},
		},
		{
			name: "with single header",
			opts: []ClientOption{
				WithHeader("Authorization", "Bearer token"),
			},
			asserts: func(t *testing.T, c *Client) {
				assert.Equal(t, "Bearer token", c.headers["Authorization"])
			},
		},
		{
			name: "with resilience config",
			opts: []ClientOption{
				WithResilienceConfig(DefaultResilienceConfig()),
			},
			asserts: func(t *testing.T, c *Client) {
				assert.NotNil(t, c.httpClient)
			},
		},
		{
			name: "all options combined",
			opts: []ClientOption{
				WithBaseURL("https://api.example.com"),
				WithTimeout(15 * time.Second),
				WithHeader("Authorization", "Bearer token"),
				WithResilienceConfig(DefaultResilienceConfig()),
			},
			asserts: func(t *testing.T, c *Client) {
				assert.Equal(t, "https://api.example.com", c.baseURL)
				assert.Equal(t, 15*time.Second, c.timeout)
				assert.Equal(t, "Bearer token", c.headers["Authorization"])
				assert.NotNil(t, c.httpClient)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.opts...)
			tt.asserts(t, client)
		})
	}
}

func TestNewDefaultClient(t *testing.T) {
	client := NewDefaultClient("https://api.example.com")

	assert.NotNil(t, client)
	assert.Equal(t, "https://api.example.com", client.baseURL)
	assert.Equal(t, "application/json", client.headers["Content-Type"])
	assert.Equal(t, "application/json", client.headers["Accept"])
}

func TestClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/users/123", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(testResponse{Message: "success", ID: 123})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	resp, err := client.Get(context.Background(), "/users/123")

	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_Post(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/users", r.URL.Path)

		var req testRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "John", req.Name)
		assert.Equal(t, 100, req.Value)

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(testResponse{Message: "created", ID: 1})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	body := testRequest{Name: "John", Value: 100}
	resp, err := client.Post(context.Background(), "/users", body)

	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestClient_Put(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	body := testRequest{Name: "Updated", Value: 200}
	resp, err := client.Put(context.Background(), "/users/1", body)

	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_Patch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	body := map[string]interface{}{"name": "Patched"}
	resp, err := client.Patch(context.Background(), "/users/1", body)

	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	resp, err := client.Delete(context.Background(), "/users/1")

	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestClient_GetJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(testResponse{Message: "success", ID: 123})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	var result testResponse
	err := client.GetJSON(context.Background(), "/users/123", &result)

	require.NoError(t, err)
	assert.Equal(t, "success", result.Message)
	assert.Equal(t, 123, result.ID)
}

func TestClient_PostJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req testRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "John", req.Name)

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(testResponse{Message: "created", ID: 1})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	body := testRequest{Name: "John", Value: 100}
	var result testResponse
	err := client.PostJSON(context.Background(), "/users", body, &result)

	require.NoError(t, err)
	assert.Equal(t, "created", result.Message)
	assert.Equal(t, 1, result.ID)
}

func TestClient_PutJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(testResponse{Message: "updated", ID: 1})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	body := testRequest{Name: "Updated", Value: 200}
	var result testResponse
	err := client.PutJSON(context.Background(), "/users/1", body, &result)

	require.NoError(t, err)
	assert.Equal(t, "updated", result.Message)
}

func TestClient_PatchJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(testResponse{Message: "patched", ID: 1})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	body := map[string]interface{}{"name": "Patched"}
	var result testResponse
	err := client.PatchJSON(context.Background(), "/users/1", body, &result)

	require.NoError(t, err)
	assert.Equal(t, "patched", result.Message)
}

func TestClient_DeleteJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(testResponse{Message: "deleted", ID: 1})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	var result testResponse
	err := client.DeleteJSON(context.Background(), "/users/1", &result)

	require.NoError(t, err)
	assert.Equal(t, "deleted", result.Message)
}

func TestClient_BuildURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		path     string
		expected string
	}{
		{
			name:     "no base url",
			baseURL:  "",
			path:     "/users",
			expected: "/users",
		},
		{
			name:     "base url without trailing slash",
			baseURL:  "https://api.example.com",
			path:     "/users",
			expected: "https://api.example.com/users",
		},
		{
			name:     "base url with trailing slash",
			baseURL:  "https://api.example.com/",
			path:     "/users",
			expected: "https://api.example.com/users",
		},
		{
			name:     "path without leading slash",
			baseURL:  "https://api.example.com",
			path:     "users",
			expected: "https://api.example.com/users",
		},
		{
			name:     "full url as path",
			baseURL:  "",
			path:     "https://other-api.com/users",
			expected: "https://other-api.com/users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(WithBaseURL(tt.baseURL))
			result := client.buildURL(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClient_BuildURL_PreservesQueryAndHandlesBasePath(t *testing.T) {
	client := NewClient(WithBaseURL("https://api.example.com/v1"))

	got := client.buildURL("/users?id=1&active=true#frag")
	u, err := url.Parse(got)
	require.NoError(t, err)

	assert.Equal(t, "api.example.com", u.Host)
	assert.Equal(t, "/v1/users", u.Path)
	assert.Equal(t, "id=1&active=true", u.RawQuery)
	assert.Equal(t, "frag", u.Fragment)
}

func TestClient_MarshalBody(t *testing.T) {
	client := NewClient()

	tests := []struct {
		name     string
		body     interface{}
		expected string
	}{
		{
			name:     "byte slice",
			body:     []byte("raw bytes"),
			expected: "raw bytes",
		},
		{
			name:     "string",
			body:     "string body",
			expected: "string body",
		},
		{
			name:     "struct",
			body:     testRequest{Name: "John", Value: 100},
			expected: `{"name":"John","value":100}`,
		},
		{
			name:     "io.Reader",
			body:     strings.NewReader("reader body"),
			expected: "reader body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.marshalBody(tt.body)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

func TestClient_MarshalBody_IOReaderError(t *testing.T) {
	client := NewClient()
	_, err := client.marshalBody(io.NopCloser(failingReader{}))
	require.Error(t, err)
}

func TestClient_WrapError(t *testing.T) {
	client := NewClient()

	tests := []struct {
		name          string
		err           error
		expectedType  ErrorType
		shouldContain string
	}{
		{
			name:          "context deadline exceeded",
			err:           context.DeadlineExceeded,
			expectedType:  ErrorTypeTimeout,
			shouldContain: "timeout",
		},
		{
			name:          "context canceled",
			err:           context.Canceled,
			expectedType:  ErrorTypeTimeout,
			shouldContain: "timeout",
		},
		{
			name:          "retries exceeded",
			err:           errors.New("retries exceeded"),
			expectedType:  ErrorTypeRetryExhausted,
			shouldContain: "retry",
		},
		{
			name:          "network error",
			err:           errors.New("connection refused"),
			expectedType:  ErrorTypeNetwork,
			shouldContain: "network",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrappedErr := client.wrapError(tt.err, "https://api.test.com", "GET", "req-123")
			require.Error(t, wrappedErr)

			var httpErr *Error
			require.True(t, errors.As(wrappedErr, &httpErr))
			assert.Equal(t, tt.expectedType, httpErr.Type)
			assert.Contains(t, httpErr.Error(), tt.shouldContain)
		})
	}
}

func TestClient_HandleHTTPError(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   interface{}
		expectedStatus int
		expectedType   ErrorType
	}{
		{
			name:           "404 not found",
			statusCode:     http.StatusNotFound,
			responseBody:   map[string]string{"error": "User not found"},
			expectedStatus: http.StatusNotFound,
			expectedType:   ErrorTypeHTTP,
		},
		{
			name:           "500 internal server error",
			statusCode:     http.StatusInternalServerError,
			responseBody:   map[string]string{"message": "Internal error"},
			expectedStatus: http.StatusInternalServerError,
			expectedType:   ErrorTypeHTTP,
		},
		{
			name:           "400 bad request",
			statusCode:     http.StatusBadRequest,
			responseBody:   map[string]string{"details": "Invalid input"},
			expectedStatus: http.StatusBadRequest,
			expectedType:   ErrorTypeHTTP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-ID", "test-req-123")
				w.WriteHeader(tt.statusCode)
				json.NewEncoder(w).Encode(tt.responseBody)
			}))
			defer server.Close()

			client := NewClient(WithBaseURL(server.URL))
			resp, err := client.Get(context.Background(), "/test")
			require.NoError(t, err)
			defer resp.Body.Close()

			httpErr := client.handleHTTPError(resp, "/test", http.MethodGet)
			require.Error(t, httpErr)

			var clientErr *Error
			require.True(t, errors.As(httpErr, &clientErr))
			assert.Equal(t, tt.expectedType, clientErr.Type)
			assert.Equal(t, tt.expectedStatus, clientErr.StatusCode)
			assert.Equal(t, "test-req-123", clientErr.RequestID)
		})
	}
}

func TestClient_HeaderManagement(t *testing.T) {
	client := NewClient()

	client.SetHeader("Authorization", "Bearer token1")
	assert.Equal(t, "Bearer token1", client.headers["Authorization"])

	client.SetHeader("Authorization", "Bearer token2")
	assert.Equal(t, "Bearer token2", client.headers["Authorization"])

	client.SetHeaders(map[string]string{
		"X-Custom-1": "value1",
		"X-Custom-2": "value2",
	})
	assert.Equal(t, "value1", client.headers["X-Custom-1"])
	assert.Equal(t, "value2", client.headers["X-Custom-2"])

	client.RemoveHeader("Authorization")
	_, exists := client.headers["Authorization"]
	assert.False(t, exists)
}

func TestClient_BaseURLManagement(t *testing.T) {
	client := NewClient()

	assert.Equal(t, "", client.baseURL)

	client.SetBaseURL("https://api.example.com/")
	assert.Equal(t, "https://api.example.com", client.baseURL)

	client.SetBaseURL("https://api2.example.com")
	assert.Equal(t, "https://api2.example.com", client.baseURL)
}

func TestClient_HTTPClientManagement(t *testing.T) {
	client := NewClient()

	customClient := &http.Client{Timeout: 5 * time.Second}
	client.SetHTTPClient(customClient)

	retrieved := client.GetHTTPClient()
	assert.Equal(t, customClient, retrieved)
	assert.Equal(t, 5*time.Second, retrieved.Timeout)
}

func TestClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.Get(ctx, "/test")
	assert.Error(t, err)

	var clientErr *Error
	require.True(t, errors.As(err, &clientErr))
	assert.Equal(t, ErrorTypeTimeout, clientErr.Type)
}

func TestClient_NoContentResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	var result testResponse
	err := client.DeleteJSON(context.Background(), "/users/1", &result)

	require.NoError(t, err)
	assert.Equal(t, testResponse{}, result)
}

func TestClient_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	var result testResponse
	err := client.GetJSON(context.Background(), "/test", &result)

	require.Error(t, err)
	var clientErr *Error
	require.True(t, errors.As(err, &clientErr))
	assert.Equal(t, ErrorTypeValidation, clientErr.Type)
}

func TestClient_RequestWithCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer custom-token", r.Header.Get("Authorization"))
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom-Header"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithHeader("Authorization", "Bearer custom-token"),
		WithHeader("X-Custom-Header", "custom-value"),
	)

	resp, err := client.Get(context.Background(), "/test")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_IntegrationWithResilience(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(testResponse{Message: "success", ID: 1})
	}))
	defer server.Close()

	cfg := &ResilienceConfig{
		RetryPolicy: &RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    3,
			BackoffInitial: 10 * time.Millisecond,
			BackoffMax:     100 * time.Millisecond,
			RetryableStatus: []int{
				http.StatusInternalServerError,
			},
		},
	}

	client := NewClient(
		WithBaseURL(server.URL),
		WithResilienceConfig(cfg),
	)

	var result testResponse
	err := client.GetJSON(context.Background(), "/test", &result)

	require.NoError(t, err)
	assert.Equal(t, "success", result.Message)
	assert.Equal(t, 3, requestCount)
}

func TestClient_NilBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Empty(t, body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	resp, err := client.Post(context.Background(), "/test", nil)

	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_Do_SetsGetBodyForReplayableRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		assert.Equal(t, "hello", string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// RoundTripper that asserts GetBody is present and replay works.
	assertRT := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		require.NotNil(t, req.GetBody)
		rc, err := req.GetBody()
		require.NoError(t, err)
		defer rc.Close()
		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(b))

		return http.DefaultTransport.RoundTrip(req)
	})

	hc := &http.Client{Transport: assertRT}
	client := NewClient(WithBaseURL(server.URL), WithHTTPClient(hc))

	resp, err := client.Post(context.Background(), "/test", []byte("hello"))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClient_Do_NilBodyDoesNotSetContentLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	assertRT := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		assert.Nil(t, req.GetBody)
		assert.Equal(t, int64(0), req.ContentLength)
		return http.DefaultTransport.RoundTrip(req)
	})

	hc := &http.Client{Transport: assertRT}
	client := NewClient(WithBaseURL(server.URL), WithHTTPClient(hc))

	resp, err := client.Post(context.Background(), "/test", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
}

func TestClient_DoWithResponse_EmptyBodyIsOKForNonNoContentWhenResultNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	err := client.DoWithResponse(context.Background(), http.MethodGet, "/test", nil, nil)
	require.NoError(t, err)
}

func TestClient_HandleHTTPError_ReadBodyFailure(t *testing.T) {
	client := NewClient(WithBaseURL("https://api.example.com"))
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"X-Request-ID": []string{"req-1"}},
		Body:       io.NopCloser(failingReader{}),
	}

	err := client.handleHTTPError(resp, "/test", http.MethodGet)
	require.Error(t, err)
	var clientErr *Error
	require.True(t, errors.As(err, &clientErr))
	assert.Equal(t, ErrorTypeNetwork, clientErr.Type)
}

type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) { return 0, errors.New("read failed") }

func (failingReader) Close() error { return nil }

func TestClient_DoWithResponse_InvalidResultPointer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"ok","id":1}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	// Pass a non-pointer result - json decode should fail.
	var result testResponse
	err := client.GetJSON(context.Background(), "/test", result)
	require.Error(t, err)
	var clientErr *Error
	require.True(t, errors.As(err, &clientErr))
	assert.Equal(t, ErrorTypeValidation, clientErr.Type)
}

func TestClient_SetHTTPClient_IsThreadSafe(t *testing.T) {
	client := NewClient()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			client.SetHTTPClient(&http.Client{Transport: http.DefaultTransport})
		}
	}()

	for i := 0; i < 200; i++ {
		_ = client.GetHTTPClient()
	}

	<-done
}

func TestClient_Do_CopyHeadersUnderLock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ensure header exists; value may vary if mutated concurrently.
		assert.NotEmpty(t, r.Header.Get("X-Token"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithHeader("X-Token", "a"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 250; i++ {
			client.SetHeader("X-Token", fmt.Sprintf("v-%d", i))
		}
	}()

	for i := 0; i < 50; i++ {
		resp, err := client.Get(context.Background(), "/test")
		require.NoError(t, err)
		_ = resp.Body.Close()
	}

	<-done
}

func TestNewClient_DoesNotMutateDefaultClient(t *testing.T) {
	original := http.DefaultClient.Timeout
	defer func() { http.DefaultClient.Timeout = original }()

	http.DefaultClient.Timeout = 0
	_ = NewClient(WithTimeout(123 * time.Millisecond))

	assert.Equal(t, time.Duration(0), http.DefaultClient.Timeout)
}

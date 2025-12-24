package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFlakyServer(failCount int, statusCode int) *httptest.Server {
	var count atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentCount := count.Add(1)
		if failCount == 0 {
			w.WriteHeader(statusCode)
			if statusCode == http.StatusOK {
				w.Write([]byte("success"))
			}
			return
		}
		if currentCount <= int32(failCount) {
			w.WriteHeader(statusCode)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
}

func newSlowServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(delay):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("slow response"))
		}
	}))
}

func newCustomServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestThresholdRatio(t *testing.T) {
	tests := []struct {
		name     string
		failures uint
		total    uint
	}{
		{
			name:     "3 out of 5",
			failures: 3,
			total:    5,
		},
		{
			name:     "1 out of 2",
			failures: 1,
			total:    2,
		},
		{
			name:     "50 out of 100",
			failures: 50,
			total:    100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio := &ThresholdRatio{
				Failures: tt.failures,
				Total:    tt.total,
			}

			assert.Equal(t, tt.failures, ratio.Failures)
			assert.Equal(t, tt.total, ratio.Total)
		})
	}
}

func TestCircuitBreakerConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *CircuitBreakerConfig
		assert func(t *testing.T, cfg *CircuitBreakerConfig)
	}{
		{
			name: "count based threshold",
			config: &CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 5,
				SuccessThreshold: 2,
				Delay:            10 * time.Second,
			},
			assert: func(t *testing.T, cfg *CircuitBreakerConfig) {
				assert.True(t, cfg.Enabled)
				assert.Equal(t, uint(5), cfg.FailureThreshold)
				assert.Equal(t, uint(2), cfg.SuccessThreshold)
				assert.Equal(t, 10*time.Second, cfg.Delay)
			},
		},
		{
			name: "ratio based threshold",
			config: &CircuitBreakerConfig{
				Enabled: true,
				FailureThresholdRatio: &ThresholdRatio{
					Failures: 3,
					Total:    5,
				},
				SuccessThresholdRatio: &ThresholdRatio{
					Failures: 2,
					Total:    3,
				},
				Delay: 5 * time.Second,
			},
			assert: func(t *testing.T, cfg *CircuitBreakerConfig) {
				assert.True(t, cfg.Enabled)
				require.NotNil(t, cfg.FailureThresholdRatio)
				assert.Equal(t, uint(3), cfg.FailureThresholdRatio.Failures)
				assert.Equal(t, uint(5), cfg.FailureThresholdRatio.Total)
			},
		},
		{
			name: "with callbacks",
			config: &CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 3,
				OnStateChanged:   func(event circuitbreaker.StateChangedEvent) {},
				OnOpen:           func(event circuitbreaker.StateChangedEvent) {},
				OnHalfOpen:       func(event circuitbreaker.StateChangedEvent) {},
				OnClose:          func(event circuitbreaker.StateChangedEvent) {},
			},
			assert: func(t *testing.T, cfg *CircuitBreakerConfig) {
				assert.NotNil(t, cfg.OnStateChanged)
				assert.NotNil(t, cfg.OnOpen)
				assert.NotNil(t, cfg.OnHalfOpen)
				assert.NotNil(t, cfg.OnClose)
			},
		},
		{
			name: "disabled",
			config: &CircuitBreakerConfig{
				Enabled: false,
			},
			assert: func(t *testing.T, cfg *CircuitBreakerConfig) {
				assert.False(t, cfg.Enabled)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.assert(t, tt.config)
		})
	}
}

func TestRetryPolicyConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *RetryPolicyConfig
		assert func(t *testing.T, cfg *RetryPolicyConfig)
	}{
		{
			name: "max attempts",
			config: &RetryPolicyConfig{
				Enabled:     true,
				MaxAttempts: 3,
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.True(t, cfg.Enabled)
				assert.Equal(t, 3, cfg.MaxAttempts)
			},
		},
		{
			name: "max retries",
			config: &RetryPolicyConfig{
				Enabled:    true,
				MaxRetries: 5,
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.Equal(t, 5, cfg.MaxRetries)
			},
		},
		{
			name: "fixed delay",
			config: &RetryPolicyConfig{
				Enabled: true,
				Delay:   500 * time.Millisecond,
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.Equal(t, 500*time.Millisecond, cfg.Delay)
			},
		},
		{
			name: "exponential backoff",
			config: &RetryPolicyConfig{
				Enabled:        true,
				BackoffInitial: 100 * time.Millisecond,
				BackoffMax:     10 * time.Second,
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.Equal(t, 100*time.Millisecond, cfg.BackoffInitial)
				assert.Equal(t, 10*time.Second, cfg.BackoffMax)
			},
		},
		{
			name: "random delay",
			config: &RetryPolicyConfig{
				Enabled:        true,
				RandomDelayMin: 100 * time.Millisecond,
				RandomDelayMax: 1 * time.Second,
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.Equal(t, 100*time.Millisecond, cfg.RandomDelayMin)
				assert.Equal(t, 1*time.Second, cfg.RandomDelayMax)
			},
		},
		{
			name: "jitter factor",
			config: &RetryPolicyConfig{
				Enabled:      true,
				JitterFactor: 0.25,
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.Equal(t, 0.25, cfg.JitterFactor)
			},
		},
		{
			name: "time based jitter",
			config: &RetryPolicyConfig{
				Enabled: true,
				Jitter:  50 * time.Millisecond,
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.Equal(t, 50*time.Millisecond, cfg.Jitter)
			},
		},
		{
			name: "retryable status codes",
			config: &RetryPolicyConfig{
				Enabled:         true,
				RetryableStatus: []int{500, 502, 503, 504},
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.Equal(t, []int{500, 502, 503, 504}, cfg.RetryableStatus)
			},
		},
		{
			name: "abort on status codes",
			config: &RetryPolicyConfig{
				Enabled:       true,
				AbortOnStatus: []int{400, 401, 403, 404},
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.Equal(t, []int{400, 401, 403, 404}, cfg.AbortOnStatus)
			},
		},
		{
			name: "with callbacks",
			config: &RetryPolicyConfig{
				Enabled:           true,
				OnRetry:           func(event failsafe.ExecutionEvent[*http.Response]) {},
				OnRetriesExceeded: func(event failsafe.ExecutionEvent[*http.Response]) {},
				OnAbort:           func(event failsafe.ExecutionEvent[*http.Response]) {},
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.NotNil(t, cfg.OnRetry)
				assert.NotNil(t, cfg.OnRetriesExceeded)
				assert.NotNil(t, cfg.OnAbort)
			},
		},
		{
			name: "max duration",
			config: &RetryPolicyConfig{
				Enabled:     true,
				MaxDuration: 30 * time.Second,
			},
			assert: func(t *testing.T, cfg *RetryPolicyConfig) {
				assert.Equal(t, 30*time.Second, cfg.MaxDuration)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.assert(t, tt.config)
		})
	}
}

func TestTimeoutConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *TimeoutConfig
		assert func(t *testing.T, cfg *TimeoutConfig)
	}{
		{
			name: "basic timeout",
			config: &TimeoutConfig{
				Enabled:  true,
				Duration: 30 * time.Second,
			},
			assert: func(t *testing.T, cfg *TimeoutConfig) {
				assert.True(t, cfg.Enabled)
				assert.Equal(t, 30*time.Second, cfg.Duration)
			},
		},
		{
			name: "with callback",
			config: &TimeoutConfig{
				Enabled:           true,
				Duration:          10 * time.Second,
				OnTimeoutExceeded: func(event failsafe.ExecutionDoneEvent[*http.Response]) {},
			},
			assert: func(t *testing.T, cfg *TimeoutConfig) {
				assert.NotNil(t, cfg.OnTimeoutExceeded)
			},
		},
		{
			name: "disabled",
			config: &TimeoutConfig{
				Enabled: false,
			},
			assert: func(t *testing.T, cfg *TimeoutConfig) {
				assert.False(t, cfg.Enabled)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.assert(t, tt.config)
		})
	}
}

func TestFallbackConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *FallbackConfig
		assert func(t *testing.T, cfg *FallbackConfig)
	}{
		{
			name: "with fallback func",
			config: &FallbackConfig{
				Enabled: true,
				FallbackFunc: func(exec failsafe.Execution[*http.Response]) (*http.Response, error) {
					return &http.Response{StatusCode: 200}, nil
				},
			},
			assert: func(t *testing.T, cfg *FallbackConfig) {
				assert.True(t, cfg.Enabled)
				assert.NotNil(t, cfg.FallbackFunc)
			},
		},
		{
			name: "with callback",
			config: &FallbackConfig{
				Enabled: true,
				FallbackFunc: func(exec failsafe.Execution[*http.Response]) (*http.Response, error) {
					return nil, nil
				},
				OnFallbackExecuted: func(event failsafe.ExecutionDoneEvent[*http.Response]) {},
			},
			assert: func(t *testing.T, cfg *FallbackConfig) {
				assert.NotNil(t, cfg.OnFallbackExecuted)
			},
		},
		{
			name: "disabled",
			config: &FallbackConfig{
				Enabled: false,
			},
			assert: func(t *testing.T, cfg *FallbackConfig) {
				assert.False(t, cfg.Enabled)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.assert(t, tt.config)
		})
	}
}

func TestResilienceBuilder(t *testing.T) {
	tests := []struct {
		name   string
		build  func() *ResilienceConfig
		assert func(t *testing.T, cfg *ResilienceConfig)
	}{
		{
			name: "empty builder",
			build: func() *ResilienceConfig {
				return NewResilienceBuilder().Build()
			},
			assert: func(t *testing.T, cfg *ResilienceConfig) {
				assert.Nil(t, cfg.CircuitBreaker)
				assert.Nil(t, cfg.RetryPolicy)
				assert.Nil(t, cfg.Timeout)
				assert.Nil(t, cfg.Fallback)
			},
		},
		{
			name: "with circuit breaker",
			build: func() *ResilienceConfig {
				return NewResilienceBuilder().
					WithCircuitBreaker(&CircuitBreakerConfig{Enabled: true}).
					Build()
			},
			assert: func(t *testing.T, cfg *ResilienceConfig) {
				require.NotNil(t, cfg.CircuitBreaker)
				assert.True(t, cfg.CircuitBreaker.Enabled)
			},
		},
		{
			name: "with retry policy",
			build: func() *ResilienceConfig {
				return NewResilienceBuilder().
					WithRetryPolicy(&RetryPolicyConfig{Enabled: true}).
					Build()
			},
			assert: func(t *testing.T, cfg *ResilienceConfig) {
				require.NotNil(t, cfg.RetryPolicy)
				assert.True(t, cfg.RetryPolicy.Enabled)
			},
		},
		{
			name: "with timeout",
			build: func() *ResilienceConfig {
				return NewResilienceBuilder().
					WithTimeout(&TimeoutConfig{Enabled: true}).
					Build()
			},
			assert: func(t *testing.T, cfg *ResilienceConfig) {
				require.NotNil(t, cfg.Timeout)
				assert.True(t, cfg.Timeout.Enabled)
			},
		},
		{
			name: "with fallback",
			build: func() *ResilienceConfig {
				return NewResilienceBuilder().
					WithFallback(&FallbackConfig{Enabled: true}).
					Build()
			},
			assert: func(t *testing.T, cfg *ResilienceConfig) {
				require.NotNil(t, cfg.Fallback)
				assert.True(t, cfg.Fallback.Enabled)
			},
		},
		{
			name: "all policies",
			build: func() *ResilienceConfig {
				return NewResilienceBuilder().
					WithCircuitBreaker(&CircuitBreakerConfig{Enabled: true}).
					WithRetryPolicy(&RetryPolicyConfig{Enabled: true}).
					WithTimeout(&TimeoutConfig{Enabled: true}).
					WithFallback(&FallbackConfig{Enabled: true}).
					Build()
			},
			assert: func(t *testing.T, cfg *ResilienceConfig) {
				require.NotNil(t, cfg.CircuitBreaker)
				require.NotNil(t, cfg.RetryPolicy)
				require.NotNil(t, cfg.Timeout)
				require.NotNil(t, cfg.Fallback)
				assert.True(t, cfg.CircuitBreaker.Enabled)
				assert.True(t, cfg.RetryPolicy.Enabled)
				assert.True(t, cfg.Timeout.Enabled)
				assert.True(t, cfg.Fallback.Enabled)
			},
		},
		{
			name: "builder chaining returns self",
			build: func() *ResilienceConfig {
				builder := NewResilienceBuilder()
				b1 := builder.WithCircuitBreaker(&CircuitBreakerConfig{})
				b2 := b1.WithRetryPolicy(&RetryPolicyConfig{})
				b3 := b2.WithTimeout(&TimeoutConfig{})
				b4 := b3.WithFallback(&FallbackConfig{})
				assert.Equal(t, builder, b1)
				assert.Equal(t, builder, b2)
				assert.Equal(t, builder, b3)
				assert.Equal(t, builder, b4)
				return builder.Build()
			},
			assert: func(t *testing.T, cfg *ResilienceConfig) {
				assert.NotNil(t, cfg)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.build()
			tt.assert(t, cfg)
		})
	}
}

func TestDefaultResilienceConfig(t *testing.T) {
	cfg := DefaultResilienceConfig()

	require.NotNil(t, cfg)
	require.NotNil(t, cfg.CircuitBreaker)
	require.NotNil(t, cfg.RetryPolicy)
	require.NotNil(t, cfg.Timeout)
	assert.Nil(t, cfg.Fallback)

	assert.True(t, cfg.CircuitBreaker.Enabled)
	assert.Equal(t, uint(5), cfg.CircuitBreaker.FailureThreshold)
	assert.Equal(t, uint(2), cfg.CircuitBreaker.SuccessThreshold)
	assert.Equal(t, 10*time.Second, cfg.CircuitBreaker.Delay)

	assert.True(t, cfg.RetryPolicy.Enabled)
	assert.Equal(t, 3, cfg.RetryPolicy.MaxAttempts)
	assert.Equal(t, 100*time.Millisecond, cfg.RetryPolicy.BackoffInitial)
	assert.Equal(t, 10*time.Second, cfg.RetryPolicy.BackoffMax)
	assert.Equal(t, 0.1, cfg.RetryPolicy.JitterFactor)
	assert.NotEmpty(t, cfg.RetryPolicy.RetryableStatus)
	assert.Contains(t, cfg.RetryPolicy.RetryableStatus, http.StatusInternalServerError)
	assert.Contains(t, cfg.RetryPolicy.RetryableStatus, http.StatusBadGateway)
	assert.Contains(t, cfg.RetryPolicy.RetryableStatus, http.StatusServiceUnavailable)
	assert.Contains(t, cfg.RetryPolicy.RetryableStatus, http.StatusGatewayTimeout)
	assert.Contains(t, cfg.RetryPolicy.RetryableStatus, http.StatusTooManyRequests)

	assert.True(t, cfg.Timeout.Enabled)
	assert.Equal(t, 30*time.Second, cfg.Timeout.Duration)
}

func TestNewResilientClient(t *testing.T) {
	tests := []struct {
		name   string
		config *ResilienceConfig
		assert func(t *testing.T, client *http.Client)
	}{
		{
			name:   "with default config",
			config: DefaultResilienceConfig(),
			assert: func(t *testing.T, client *http.Client) {
				assert.NotNil(t, client)
				assert.NotNil(t, client.Transport)
			},
		},
		{
			name:   "with nil config",
			config: nil,
			assert: func(t *testing.T, client *http.Client) {
				assert.NotNil(t, client)
				assert.Equal(t, http.DefaultTransport, client.Transport)
			},
		},
		{
			name: "with custom config",
			config: &ResilienceConfig{
				Timeout: &TimeoutConfig{
					Enabled:  true,
					Duration: 5 * time.Second,
				},
			},
			assert: func(t *testing.T, client *http.Client) {
				assert.NotNil(t, client)
				assert.NotNil(t, client.Transport)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewResilientClient(tt.config)
			tt.assert(t, client)
		})
	}
}

func TestNewResilientTransport(t *testing.T) {
	tests := []struct {
		name          string
		baseTransport http.RoundTripper
		config        *ResilienceConfig
		assert        func(t *testing.T, transport http.RoundTripper)
	}{
		{
			name:          "nil base transport",
			baseTransport: nil,
			config:        DefaultResilienceConfig(),
			assert: func(t *testing.T, transport http.RoundTripper) {
				assert.NotNil(t, transport)
			},
		},
		{
			name:          "custom base transport",
			baseTransport: http.DefaultTransport,
			config:        DefaultResilienceConfig(),
			assert: func(t *testing.T, transport http.RoundTripper) {
				assert.NotNil(t, transport)
				assert.NotEqual(t, http.DefaultTransport, transport)
			},
		},
		{
			name:          "nil config",
			baseTransport: http.DefaultTransport,
			config:        nil,
			assert: func(t *testing.T, transport http.RoundTripper) {
				assert.Equal(t, http.DefaultTransport, transport)
			},
		},
		{
			name:          "all policies disabled",
			baseTransport: http.DefaultTransport,
			config: &ResilienceConfig{
				CircuitBreaker: &CircuitBreakerConfig{Enabled: false},
				RetryPolicy:    &RetryPolicyConfig{Enabled: false},
				Timeout:        &TimeoutConfig{Enabled: false},
				Fallback:       &FallbackConfig{Enabled: false},
			},
			assert: func(t *testing.T, transport http.RoundTripper) {
				assert.Equal(t, http.DefaultTransport, transport)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := NewResilientTransport(tt.baseTransport, tt.config)
			tt.assert(t, transport)
		})
	}
}

func TestRetryPolicyIntegration(t *testing.T) {
	tests := []struct {
		name          string
		serverSetup   func() *httptest.Server
		config        *ResilienceConfig
		expectedCode  int
		shouldSucceed bool
		checkError    func(t *testing.T, err error)
	}{
		{
			name: "success after 2 failures",
			serverSetup: func() *httptest.Server {
				return newFlakyServer(2, http.StatusInternalServerError)
			},
			config: &ResilienceConfig{
				RetryPolicy: &RetryPolicyConfig{
					Enabled:        true,
					MaxAttempts:    3,
					BackoffInitial: 10 * time.Millisecond,
					BackoffMax:     100 * time.Millisecond,
					RetryableStatus: []int{
						http.StatusInternalServerError,
					},
				},
			},
			expectedCode:  http.StatusOK,
			shouldSucceed: true,
		},
		{
			name: "exceeds max attempts",
			serverSetup: func() *httptest.Server {
				return newFlakyServer(10, http.StatusInternalServerError)
			},
			config: &ResilienceConfig{
				RetryPolicy: &RetryPolicyConfig{
					Enabled:        true,
					MaxAttempts:    2,
					BackoffInitial: 10 * time.Millisecond,
					BackoffMax:     100 * time.Millisecond,
					RetryableStatus: []int{
						http.StatusInternalServerError,
					},
				},
			},
			shouldSucceed: false,
			checkError: func(t *testing.T, err error) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "retries exceeded")
			},
		},
		{
			name: "abort on 4xx status",
			serverSetup: func() *httptest.Server {
				return newCustomServer(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				})
			},
			config: &ResilienceConfig{
				RetryPolicy: &RetryPolicyConfig{
					Enabled:     true,
					MaxAttempts: 5,
					RetryableStatus: []int{
						http.StatusInternalServerError,
						http.StatusBadGateway,
						http.StatusServiceUnavailable,
					},
					AbortOnStatus: []int{http.StatusUnauthorized},
				},
			},
			expectedCode:  http.StatusUnauthorized,
			shouldSucceed: true,
		},
		{
			name: "no retry on success",
			serverSetup: func() *httptest.Server {
				return newFlakyServer(0, http.StatusOK)
			},
			config: &ResilienceConfig{
				RetryPolicy: &RetryPolicyConfig{
					Enabled:        true,
					MaxAttempts:    3,
					BackoffInitial: 10 * time.Millisecond,
					BackoffMax:     100 * time.Millisecond,
					RetryableStatus: []int{
						http.StatusInternalServerError,
					},
				},
			},
			expectedCode:  http.StatusOK,
			shouldSucceed: true,
		},
		{
			name: "retry on 503",
			serverSetup: func() *httptest.Server {
				return newFlakyServer(1, http.StatusServiceUnavailable)
			},
			config: &ResilienceConfig{
				RetryPolicy: &RetryPolicyConfig{
					Enabled:        true,
					MaxAttempts:    2,
					BackoffInitial: 10 * time.Millisecond,
					BackoffMax:     100 * time.Millisecond,
					RetryableStatus: []int{
						http.StatusServiceUnavailable,
					},
				},
			},
			expectedCode:  http.StatusOK,
			shouldSucceed: true,
		},
		{
			name: "retry on 429 too many requests",
			serverSetup: func() *httptest.Server {
				return newFlakyServer(1, http.StatusTooManyRequests)
			},
			config: &ResilienceConfig{
				RetryPolicy: &RetryPolicyConfig{
					Enabled:        true,
					MaxAttempts:    2,
					BackoffInitial: 10 * time.Millisecond,
					BackoffMax:     100 * time.Millisecond,
					RetryableStatus: []int{
						http.StatusTooManyRequests,
					},
				},
			},
			expectedCode:  http.StatusOK,
			shouldSucceed: true,
		},
		{
			name: "fixed delay strategy",
			serverSetup: func() *httptest.Server {
				return newFlakyServer(2, http.StatusInternalServerError)
			},
			config: &ResilienceConfig{
				RetryPolicy: &RetryPolicyConfig{
					Enabled:     true,
					MaxAttempts: 3,
					Delay:       50 * time.Millisecond,
					RetryableStatus: []int{
						http.StatusInternalServerError,
					},
				},
			},
			expectedCode:  http.StatusOK,
			shouldSucceed: true,
		},
		{
			name: "max duration exceeded",
			serverSetup: func() *httptest.Server {
				return newFlakyServer(10, http.StatusInternalServerError)
			},
			config: &ResilienceConfig{
				RetryPolicy: &RetryPolicyConfig{
					Enabled:        true,
					MaxAttempts:    100,
					MaxDuration:    50 * time.Millisecond,
					BackoffInitial: 10 * time.Millisecond,
					BackoffMax:     100 * time.Millisecond,
					RetryableStatus: []int{
						http.StatusInternalServerError,
					},
				},
			},
			shouldSucceed: false,
			checkError: func(t *testing.T, err error) {
				assert.Error(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.serverSetup()
			defer server.Close()

			client := NewResilientClient(tt.config)
			resp, err := client.Get(server.URL)

			if tt.shouldSucceed {
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, tt.expectedCode, resp.StatusCode)
			} else {
				if tt.checkError != nil {
					tt.checkError(t, err)
				} else {
					assert.Error(t, err)
				}
			}
		})
	}
}

func TestRetryPolicyWithCallbacks(t *testing.T) {
	server := newFlakyServer(2, http.StatusInternalServerError)
	defer server.Close()

	var retryCount, exceededCount, abortCount atomic.Int32

	cfg := &ResilienceConfig{
		RetryPolicy: &RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    3,
			BackoffInitial: 10 * time.Millisecond,
			BackoffMax:     100 * time.Millisecond,
			RetryableStatus: []int{
				http.StatusInternalServerError,
			},
			OnRetry: func(event failsafe.ExecutionEvent[*http.Response]) {
				retryCount.Add(1)
			},
			OnRetriesExceeded: func(event failsafe.ExecutionEvent[*http.Response]) {
				exceededCount.Add(1)
			},
			OnAbort: func(event failsafe.ExecutionEvent[*http.Response]) {
				abortCount.Add(1)
			},
		},
	}

	client := NewResilientClient(cfg)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), retryCount.Load())
	assert.Equal(t, int32(0), exceededCount.Load())
	assert.Equal(t, int32(0), abortCount.Load())
}

func TestRetryPolicyExceedsAttempts(t *testing.T) {
	server := newFlakyServer(10, http.StatusInternalServerError)
	defer server.Close()

	var exceededCount atomic.Int32

	cfg := &ResilienceConfig{
		RetryPolicy: &RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    2,
			BackoffInitial: 10 * time.Millisecond,
			BackoffMax:     100 * time.Millisecond,
			RetryableStatus: []int{
				http.StatusInternalServerError,
			},
			OnRetriesExceeded: func(event failsafe.ExecutionEvent[*http.Response]) {
				exceededCount.Add(1)
			},
		},
	}

	client := NewResilientClient(cfg)
	_, err := client.Get(server.URL)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "retries exceeded")
	assert.Equal(t, int32(1), exceededCount.Load())
}

func TestRetryPolicyAbortCallback(t *testing.T) {
	var requestCount atomic.Int32
	server := newCustomServer(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	defer server.Close()

	var abortCount, retryCount atomic.Int32

	cfg := &ResilienceConfig{
		RetryPolicy: &RetryPolicyConfig{
			Enabled:     true,
			MaxAttempts: 5,
			RetryableStatus: []int{
				http.StatusInternalServerError,
				http.StatusUnauthorized,
			},
			AbortOnStatus: []int{http.StatusUnauthorized},
			OnRetry: func(event failsafe.ExecutionEvent[*http.Response]) {
				retryCount.Add(1)
			},
			OnAbort: func(event failsafe.ExecutionEvent[*http.Response]) {
				abortCount.Add(1)
			},
		},
	}

	client := NewResilientClient(cfg)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, int32(1), retryCount.Load())
	assert.Equal(t, int32(1), abortCount.Load())
}

func TestRetryPolicyRandomDelay(t *testing.T) {
	server := newFlakyServer(2, http.StatusInternalServerError)
	defer server.Close()

	cfg := &ResilienceConfig{
		RetryPolicy: &RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    3,
			RandomDelayMin: 10 * time.Millisecond,
			RandomDelayMax: 50 * time.Millisecond,
			RetryableStatus: []int{
				http.StatusInternalServerError,
			},
		},
	}

	client := NewResilientClient(cfg)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRetryPolicyJitterFactor(t *testing.T) {
	server := newFlakyServer(2, http.StatusInternalServerError)
	defer server.Close()

	cfg := &ResilienceConfig{
		RetryPolicy: &RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    3,
			BackoffInitial: 10 * time.Millisecond,
			BackoffMax:     100 * time.Millisecond,
			JitterFactor:   0.5,
			RetryableStatus: []int{
				http.StatusInternalServerError,
			},
		},
	}

	client := NewResilientClient(cfg)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRetryPolicyTimeBasedJitter(t *testing.T) {
	server := newFlakyServer(2, http.StatusInternalServerError)
	defer server.Close()

	cfg := &ResilienceConfig{
		RetryPolicy: &RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    3,
			BackoffInitial: 10 * time.Millisecond,
			BackoffMax:     100 * time.Millisecond,
			Jitter:         20 * time.Millisecond,
			RetryableStatus: []int{
				http.StatusInternalServerError,
			},
		},
	}

	client := NewResilientClient(cfg)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRetryPolicyMaxRetries(t *testing.T) {
	server := newFlakyServer(2, http.StatusInternalServerError)
	defer server.Close()

	cfg := &ResilienceConfig{
		RetryPolicy: &RetryPolicyConfig{
			Enabled:        true,
			MaxRetries:     2,
			BackoffInitial: 10 * time.Millisecond,
			BackoffMax:     100 * time.Millisecond,
			RetryableStatus: []int{
				http.StatusInternalServerError,
			},
		},
	}

	client := NewResilientClient(cfg)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestTimeoutIntegration(t *testing.T) {
	tests := []struct {
		name        string
		serverDelay time.Duration
		timeout     time.Duration
		shouldFail  bool
	}{
		{
			name:        "request completes within timeout",
			serverDelay: 50 * time.Millisecond,
			timeout:     200 * time.Millisecond,
			shouldFail:  false,
		},
		{
			name:        "request exceeds timeout",
			serverDelay: 200 * time.Millisecond,
			timeout:     50 * time.Millisecond,
			shouldFail:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newSlowServer(tt.serverDelay)
			defer server.Close()

			cfg := &ResilienceConfig{
				Timeout: &TimeoutConfig{
					Enabled:  true,
					Duration: tt.timeout,
				},
			}

			client := NewResilientClient(cfg)
			resp, err := client.Get(server.URL)

			if tt.shouldFail {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			}
		})
	}
}

func TestCircuitBreakerIntegration(t *testing.T) {
	server := newFlakyServer(100, http.StatusInternalServerError)
	defer server.Close()

	var openCount atomic.Int32

	cfg := &ResilienceConfig{
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 3,
			SuccessThreshold: 1,
			Delay:            50 * time.Millisecond,
			OnOpen: func(event circuitbreaker.StateChangedEvent) {
				openCount.Add(1)
			},
		},
	}

	client := NewResilientClient(cfg)

	for i := 0; i < 5; i++ {
		resp, err := client.Get(server.URL)
		if err == nil {
			resp.Body.Close()
		}
		time.Sleep(10 * time.Millisecond)
	}

	assert.Greater(t, openCount.Load(), int32(0))
}

func TestFallbackIntegration(t *testing.T) {
	tests := []struct {
		name             string
		serverSetup      func() *httptest.Server
		fallbackResponse *http.Response
		fallbackError    error
		expectedCode     int
		shouldSucceed    bool
	}{
		{
			name: "fallback on server error",
			serverSetup: func() *httptest.Server {
				return newFlakyServer(100, http.StatusInternalServerError)
			},
			fallbackResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("fallback response")),
				Header:     make(http.Header),
			},
			fallbackError: nil,
			expectedCode:  http.StatusOK,
			shouldSucceed: true,
		},
		{
			name: "fallback returns error",
			serverSetup: func() *httptest.Server {
				return newFlakyServer(100, http.StatusInternalServerError)
			},
			fallbackResponse: nil,
			fallbackError:    errors.New("fallback failed"),
			shouldSucceed:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.serverSetup()
			defer server.Close()

			cfg := &ResilienceConfig{
				Fallback: &FallbackConfig{
					Enabled: true,
					FallbackFunc: func(exec failsafe.Execution[*http.Response]) (*http.Response, error) {
						return tt.fallbackResponse, tt.fallbackError
					},
				},
			}

			client := NewResilientClient(cfg)
			resp, err := client.Get(server.URL)

			if tt.shouldSucceed {
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, tt.expectedCode, resp.StatusCode)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestCombinedPoliciesIntegration(t *testing.T) {
	server := newFlakyServer(2, http.StatusServiceUnavailable)
	defer server.Close()

	cfg := &ResilienceConfig{
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 5,
			SuccessThreshold: 2,
			Delay:            10 * time.Millisecond,
		},
		RetryPolicy: &RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    3,
			BackoffInitial: 10 * time.Millisecond,
			BackoffMax:     100 * time.Millisecond,
			RetryableStatus: []int{
				http.StatusServiceUnavailable,
			},
		},
		Timeout: &TimeoutConfig{
			Enabled:  true,
			Duration: 5 * time.Second,
		},
	}

	client := NewResilientClient(cfg)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "success", string(body))
}

func TestHTTPMethodsIntegration(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		body         string
		expectedCode int
	}{
		{
			name:         "GET request",
			method:       http.MethodGet,
			body:         "",
			expectedCode: http.StatusOK,
		},
		{
			name:         "POST request",
			method:       http.MethodPost,
			body:         `{"data":"test"}`,
			expectedCode: http.StatusCreated,
		},
		{
			name:         "PUT request",
			method:       http.MethodPut,
			body:         `{"data":"updated"}`,
			expectedCode: http.StatusOK,
		},
		{
			name:         "DELETE request",
			method:       http.MethodDelete,
			body:         "",
			expectedCode: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newCustomServer(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.method, r.Method)
				if tt.body != "" {
					body, _ := io.ReadAll(r.Body)
					assert.Equal(t, tt.body, string(body))
				}
				w.WriteHeader(tt.expectedCode)
			})
			defer server.Close()

			client := NewResilientClient(DefaultResilienceConfig())

			var req *http.Request
			if tt.body != "" {
				req, _ = http.NewRequest(tt.method, server.URL, strings.NewReader(tt.body))
			} else {
				req, _ = http.NewRequest(tt.method, server.URL, nil)
			}

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedCode, resp.StatusCode)
		})
	}
}

func TestConcurrentRequests(t *testing.T) {
	server := newFlakyServer(0, http.StatusOK)
	defer server.Close()

	cfg := DefaultResilienceConfig()
	client := NewResilientClient(cfg)

	concurrency := 50
	var wg sync.WaitGroup
	successCount := atomic.Int32{}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(server.URL)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					successCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int32(concurrency), successCount.Load())
}

func TestContextCancellation(t *testing.T) {
	server := newSlowServer(5 * time.Second)
	defer server.Close()

	client := NewResilientClient(DefaultResilienceConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	_, err := client.Do(req)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
}

func TestRetryCallbacks(t *testing.T) {
	server := newFlakyServer(2, http.StatusInternalServerError)
	defer server.Close()

	var retryCount, exceededCount atomic.Int32

	cfg := &ResilienceConfig{
		RetryPolicy: &RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    3,
			BackoffInitial: 10 * time.Millisecond,
			BackoffMax:     100 * time.Millisecond,
			RetryableStatus: []int{
				http.StatusInternalServerError,
			},
			OnRetry: func(event failsafe.ExecutionEvent[*http.Response]) {
				retryCount.Add(1)
			},
			OnRetriesExceeded: func(event failsafe.ExecutionEvent[*http.Response]) {
				exceededCount.Add(1)
			},
		},
	}

	client := NewResilientClient(cfg)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, int32(2), retryCount.Load())
	assert.Equal(t, int32(0), exceededCount.Load())
}

func TestCircuitBreakerCallbacks(t *testing.T) {
	server := newFlakyServer(100, http.StatusInternalServerError)
	defer server.Close()

	var stateChangeCount, openCount atomic.Int32

	cfg := &ResilienceConfig{
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 3,
			SuccessThreshold: 1,
			Delay:            50 * time.Millisecond,
			OnStateChanged: func(event circuitbreaker.StateChangedEvent) {
				stateChangeCount.Add(1)
			},
			OnOpen: func(event circuitbreaker.StateChangedEvent) {
				openCount.Add(1)
			},
		},
	}

	client := NewResilientClient(cfg)

	for i := 0; i < 5; i++ {
		resp, err := client.Get(server.URL)
		if err == nil {
			resp.Body.Close()
		}
		time.Sleep(10 * time.Millisecond)
	}

	assert.Greater(t, stateChangeCount.Load(), int32(0))
	assert.Greater(t, openCount.Load(), int32(0))
}

func TestTimeoutCallback(t *testing.T) {
	server := newSlowServer(200 * time.Millisecond)
	defer server.Close()

	var timeoutCount atomic.Int32

	cfg := &ResilienceConfig{
		Timeout: &TimeoutConfig{
			Enabled:  true,
			Duration: 50 * time.Millisecond,
			OnTimeoutExceeded: func(event failsafe.ExecutionDoneEvent[*http.Response]) {
				timeoutCount.Add(1)
			},
		},
	}

	client := NewResilientClient(cfg)
	_, err := client.Get(server.URL)

	assert.Error(t, err)
	assert.Equal(t, int32(1), timeoutCount.Load())
}

func TestFallbackCallback(t *testing.T) {
	server := newFlakyServer(100, http.StatusInternalServerError)
	defer server.Close()

	var fallbackCount atomic.Int32

	cfg := &ResilienceConfig{
		Fallback: &FallbackConfig{
			Enabled: true,
			FallbackFunc: func(exec failsafe.Execution[*http.Response]) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("fallback")),
					Header:     make(http.Header),
				}, nil
			},
			OnFallbackExecuted: func(event failsafe.ExecutionDoneEvent[*http.Response]) {
				fallbackCount.Add(1)
			},
		},
	}

	client := NewResilientClient(cfg)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, int32(1), fallbackCount.Load())
}

func TestHeadersPropagation(t *testing.T) {
	server := newCustomServer(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	})
	defer server.Close()

	client := NewResilientClient(DefaultResilienceConfig())

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token123")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestResponseBodyReadable(t *testing.T) {
	expectedBody := "test response body"
	server := newCustomServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(expectedBody))
	})
	defer server.Close()

	client := NewResilientClient(DefaultResilienceConfig())
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, expectedBody, string(body))
}

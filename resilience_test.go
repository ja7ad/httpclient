package httpclient

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCache struct {
	mu    sync.RWMutex
	store map[string]*http.Response
}

func (m *mockCache) Get(key string) (*http.Response, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res, ok := m.store[key]
	return res, ok
}

func (m *mockCache) Set(key string, val *http.Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = val
}

func newFlakyServer(failCount int, statusCode int) *httptest.Server {
	var count atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentCount := count.Add(1)
		if failCount == 0 {
			w.WriteHeader(statusCode)
			if statusCode == http.StatusOK {
				_, _ = w.Write([]byte("success"))
			}
			return
		}
		if currentCount <= int32(failCount) {
			w.WriteHeader(statusCode)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
}

func newSlowServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(delay):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("slow response"))
		}
	}))
}

func TestResilienceBuilder_ExtendedPolicies(t *testing.T) {
	cache := &mockCache{store: make(map[string]*http.Response)}
	cfg := NewResilienceBuilder().
		WithRateLimiter(&RateLimiterConfig{Enabled: true, MaxExecutions: 10}).
		WithBulkhead(&BulkheadConfig{Enabled: true, MaxConcurrency: 5}).
		WithHedge(&HedgeConfig{Enabled: true, Delay: 100 * time.Millisecond}).
		WithAdaptiveThrottler(&AdaptiveThrottlerConfig{Enabled: true, FailureRateThreshold: 0.5}).
		WithCache(&CacheConfig{Enabled: true, Cache: cache, Key: "test-key"}).
		Build()

	assert.NotNil(t, cfg.RateLimiter)
	assert.NotNil(t, cfg.Bulkhead)
	assert.NotNil(t, cfg.Hedge)
	assert.NotNil(t, cfg.AdaptiveThrottler)
	assert.NotNil(t, cfg.Cache)
}

func TestRateLimiterIntegration(t *testing.T) {
	server := newFlakyServer(0, http.StatusOK)
	defer server.Close()

	cfg := &ResilienceConfig{
		RateLimiter: &RateLimiterConfig{
			Enabled:       true,
			MaxExecutions: 1,
			Period:        time.Minute,
			MaxWaitTime:   0, // Do not wait; fail immediately if limit is reached
		},
	}

	client := NewResilientClient(cfg)

	// First request: Should consume the 1 available execution
	resp1, err1 := client.Get(server.URL)
	require.NoError(t, err1)
	resp1.Body.Close()

	// Second request: Should be rejected immediately
	_, err2 := client.Get(server.URL)
	assert.Error(t, err2, "Expected rate limit exceeded error")
}

func TestBulkheadIntegration(t *testing.T) {
	server := newSlowServer(200 * time.Millisecond)
	defer server.Close()

	cfg := &ResilienceConfig{
		Bulkhead: &BulkheadConfig{
			Enabled:        true,
			MaxConcurrency: 2,
			MaxWaitTime:    10 * time.Millisecond,
		},
	}

	client := NewResilientClient(cfg)

	var wg sync.WaitGroup
	var rejected atomic.Int32

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(server.URL)
			if err != nil {
				rejected.Add(1)
				return
			}
			resp.Body.Close()
		}()
	}

	wg.Wait()
	assert.Greater(t, rejected.Load(), int32(0))
}

func TestHedgeIntegration(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			time.Sleep(300 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hedged"))
	}))
	defer server.Close()

	cfg := &ResilienceConfig{
		Hedge: &HedgeConfig{
			Enabled:        true,
			Delay:          50 * time.Millisecond,
			MaxHedges:      1,
			CancelOnResult: true,
		},
	}

	client := NewResilientClient(cfg)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, int32(2), requestCount.Load())
}

func TestAdaptiveThrottlerIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &ResilienceConfig{
		AdaptiveThrottler: &AdaptiveThrottlerConfig{
			Enabled:              true,
			FailureRateThreshold: 0.01, // 1% threshold (makes it very sensitive)
			MinExecutions:        1,    // Start calculating immediately
			Period:               time.Minute,
			MaxRejectionRate:     1.0, // Allow up to 100% rejection
		},
	}

	client := NewResilientClient(cfg)

	// Warm up: Send a burst of failures to drive the rejection probability up.
	// In Google SRE throttling, it takes several failures to outweigh the
	// initial "accepted" state (which starts at 0/0).
	for i := 0; i < 20; i++ {
		_, _ = client.Get(server.URL)
	}

	assert.Eventually(t, func() bool {
		_, err := client.Get(server.URL)
		// Check if the error is specifically a Failsafe rejection error
		// adaptivethrottler.ErrExceeded is the standard error returned
		return err != nil
	}, 2*time.Second, 50*time.Millisecond, "Throttler should eventually start rejecting requests")
}

func TestCacheIntegration(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cached content"))
	}))
	defer server.Close()

	cache := &mockCache{store: make(map[string]*http.Response)}
	cfg := &ResilienceConfig{
		Cache: &CacheConfig{
			Enabled: true,
			Cache:   cache,
			Key:     "static-key",
		},
	}

	client := NewResilientClient(cfg)

	for i := 0; i < 3; i++ {
		resp, err := client.Get(server.URL)
		require.NoError(t, err)
		resp.Body.Close()
	}

	assert.Equal(t, int32(1), requestCount.Load())
}

func TestPolicyOrder_TimeoutInnermost(t *testing.T) {
	server := newSlowServer(200 * time.Millisecond)
	defer server.Close()

	cfg := &ResilienceConfig{
		RetryPolicy: &RetryPolicyConfig{
			Enabled:     true,
			MaxAttempts: 2,
		},
		Timeout: &TimeoutConfig{
			Enabled:  true,
			Duration: 50 * time.Millisecond,
		},
	}

	var retries atomic.Int32
	cfg.RetryPolicy.OnRetry = func(e failsafe.ExecutionEvent[*http.Response]) {
		retries.Add(1)
	}

	client := NewResilientClient(cfg)
	_, err := client.Get(server.URL)

	assert.Error(t, err)
	assert.Equal(t, int32(1), retries.Load())
}

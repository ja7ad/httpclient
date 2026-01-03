package httpclient

import (
	"net/http"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/adaptivethrottler"
	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/failsafe-go/failsafe-go/cachepolicy"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/failsafehttp"
	"github.com/failsafe-go/failsafe-go/fallback"
	"github.com/failsafe-go/failsafe-go/hedgepolicy"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
	"github.com/failsafe-go/failsafe-go/timeout"
)

// ThresholdRatio represents a ratio-based threshold (e.g., 3 failures out of 5 total)
type ThresholdRatio struct {
	Failures uint // Number of failures
	Total    uint // Total executions
}

// CircuitBreakerConfig holds circuit breaker configuration
type CircuitBreakerConfig struct {
	Enabled               bool
	FailureThreshold      uint            // Count-based: Opens after N consecutive failures
	FailureThresholdRatio *ThresholdRatio // Ratio-based: Opens when X of Y executions fail
	SuccessThreshold      uint            // Count-based: Closes after N consecutive successes in half-open
	SuccessThresholdRatio *ThresholdRatio // Ratio-based: Closes when X of Y executions succeed in half-open
	Delay                 time.Duration   // Time to wait in open state before half-opening
	OnStateChanged        func(event circuitbreaker.StateChangedEvent)
	OnOpen                func(event circuitbreaker.StateChangedEvent)
	OnHalfOpen            func(event circuitbreaker.StateChangedEvent)
	OnClose               func(event circuitbreaker.StateChangedEvent)
}

// RetryPolicyConfig holds retry policy configuration
type RetryPolicyConfig struct {
	Enabled           bool
	MaxAttempts       int           // Maximum number of execution attempts (initial + retries)
	MaxRetries        int           // Maximum number of retries (MaxAttempts - 1)
	MaxDuration       time.Duration // Maximum total duration for all attempts
	Delay             time.Duration // Fixed delay between retries
	BackoffInitial    time.Duration // Initial delay for exponential backoff
	BackoffMax        time.Duration // Maximum delay for exponential backoff
	RandomDelayMin    time.Duration // Minimum random delay
	RandomDelayMax    time.Duration // Maximum random delay
	JitterFactor      float64       // Jitter factor (0-1) to add randomness to delays
	Jitter            time.Duration // Time-based jitter to add to delays
	RetryableStatus   []int         // HTTP status codes that should trigger a retry
	AbortOnStatus     []int         // HTTP status codes that should abort retries immediately
	OnRetry           func(event failsafe.ExecutionEvent[*http.Response])
	OnRetriesExceeded func(event failsafe.ExecutionEvent[*http.Response])
	OnAbort           func(event failsafe.ExecutionEvent[*http.Response])
}

// TimeoutConfig holds timeout configuration
type TimeoutConfig struct {
	Enabled           bool
	Duration          time.Duration // Maximum time allowed for execution
	OnTimeoutExceeded func(event failsafe.ExecutionDoneEvent[*http.Response])
}

// FallbackConfig holds fallback configuration
type FallbackConfig struct {
	Enabled            bool
	FallbackFunc       func(exec failsafe.Execution[*http.Response]) (*http.Response, error)
	OnFallbackExecuted func(event failsafe.ExecutionDoneEvent[*http.Response])
}

// RateLimiterConfig holds rate limiter configuration
type RateLimiterConfig struct {
	Enabled       bool
	MaxExecutions uint
	Period        time.Duration
	MaxWaitTime   time.Duration
	IsBursty      bool // If true, uses fixed window; else uses smooth leaky bucket
}

// BulkheadConfig limits concurrent executions to prevent resource exhaustion
type BulkheadConfig struct {
	Enabled        bool
	MaxConcurrency uint
	MaxWaitTime    time.Duration
}

// HedgeConfig handles tail latency by sending backup requests
type HedgeConfig struct {
	Enabled        bool
	Delay          time.Duration
	MaxHedges      int
	CancelOnResult bool // Cancel outstanding hedges once a result is received
}

// AdaptiveThrottlerConfig limits requests based on failure rate
type AdaptiveThrottlerConfig struct {
	Enabled              bool
	FailureRateThreshold float64
	MinExecutions        uint
	Period               time.Duration
	MaxRejectionRate     float64
}

// CacheConfig provides read-through caching
type CacheConfig struct {
	Enabled bool
	Cache   cachepolicy.Cache[*http.Response]
	Key     string
}

// ResilienceConfig updated with new policies
type ResilienceConfig struct {
	CircuitBreaker    *CircuitBreakerConfig
	RetryPolicy       *RetryPolicyConfig
	Timeout           *TimeoutConfig
	Fallback          *FallbackConfig
	RateLimiter       *RateLimiterConfig
	Bulkhead          *BulkheadConfig
	Hedge             *HedgeConfig
	AdaptiveThrottler *AdaptiveThrottlerConfig
	Cache             *CacheConfig
}

// ResilienceBuilder builds resilience configurations using fluent API
type ResilienceBuilder struct {
	circuitBreaker    *CircuitBreakerConfig
	retryPolicy       *RetryPolicyConfig
	timeout           *TimeoutConfig
	fallback          *FallbackConfig
	rateLimiter       *RateLimiterConfig
	bulkhead          *BulkheadConfig
	hedge             *HedgeConfig
	adaptiveThrottler *AdaptiveThrottlerConfig
	cache             *CacheConfig
}

// NewResilienceBuilder creates a new resilience configuration builder
func NewResilienceBuilder() *ResilienceBuilder {
	return &ResilienceBuilder{}
}

// WithCircuitBreaker configures circuit breaker settings
func (rb *ResilienceBuilder) WithCircuitBreaker(cfg *CircuitBreakerConfig) *ResilienceBuilder {
	rb.circuitBreaker = cfg

	return rb
}

// WithRetryPolicy configures retry policy settings
func (rb *ResilienceBuilder) WithRetryPolicy(cfg *RetryPolicyConfig) *ResilienceBuilder {
	rb.retryPolicy = cfg

	return rb
}

// WithTimeout configures timeout settings
func (rb *ResilienceBuilder) WithTimeout(cfg *TimeoutConfig) *ResilienceBuilder {
	rb.timeout = cfg

	return rb
}

// WithFallback configures fallback settings
func (rb *ResilienceBuilder) WithFallback(cfg *FallbackConfig) *ResilienceBuilder {
	rb.fallback = cfg

	return rb
}

func (rb *ResilienceBuilder) WithRateLimiter(cfg *RateLimiterConfig) *ResilienceBuilder {
	rb.rateLimiter = cfg
	return rb
}

func (rb *ResilienceBuilder) WithBulkhead(cfg *BulkheadConfig) *ResilienceBuilder {
	rb.bulkhead = cfg
	return rb
}

func (rb *ResilienceBuilder) WithHedge(cfg *HedgeConfig) *ResilienceBuilder {
	rb.hedge = cfg
	return rb
}

func (rb *ResilienceBuilder) WithAdaptiveThrottler(cfg *AdaptiveThrottlerConfig) *ResilienceBuilder {
	rb.adaptiveThrottler = cfg
	return rb
}

func (rb *ResilienceBuilder) WithCache(cfg *CacheConfig) *ResilienceBuilder {
	rb.cache = cfg
	return rb
}

// Build constructs the final resilience configuration
func (rb *ResilienceBuilder) Build() *ResilienceConfig {
	return &ResilienceConfig{
		CircuitBreaker:    rb.circuitBreaker,
		RetryPolicy:       rb.retryPolicy,
		Timeout:           rb.timeout,
		Fallback:          rb.fallback,
		RateLimiter:       rb.rateLimiter,
		Bulkhead:          rb.bulkhead,
		Hedge:             rb.hedge,
		AdaptiveThrottler: rb.adaptiveThrottler,
		Cache:             rb.cache,
	}
}

// NewResilientClient creates an HTTP client with resilience policies
// This is the recommended way to create a resilient HTTP client
func NewResilientClient(cfg *ResilienceConfig) *http.Client {
	transport := buildResilientTransport(http.DefaultTransport, cfg)

	return &http.Client{
		Transport: transport,
	}
}

// NewResilientTransport wraps an existing transport with resilience policies
// Use this if you need to customize the base transport
func NewResilientTransport(baseTransport http.RoundTripper, cfg *ResilienceConfig) http.RoundTripper {
	return buildResilientTransport(baseTransport, cfg)
}

// buildResilientTransport constructs a RoundTripper with all configured policies
// Policies are applied by wrapping one at a time in this order (innermost to outermost):
// Base Transport -> Timeout -> CircuitBreaker -> Retry -> Fallback
func buildResilientTransport(baseTransport http.RoundTripper, cfg *ResilienceConfig) http.RoundTripper {
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	if cfg == nil {
		return baseTransport
	}

	layers := []struct {
		enabled bool
		builder func() failsafe.Policy[*http.Response]
	}{
		{cfg.Fallback != nil && cfg.Fallback.Enabled, func() failsafe.Policy[*http.Response] { return buildFallbackPolicy(cfg.Fallback) }},
		{cfg.Cache != nil && cfg.Cache.Enabled, func() failsafe.Policy[*http.Response] { return buildCachePolicy(cfg.Cache) }},
		{cfg.RetryPolicy != nil && cfg.RetryPolicy.Enabled, func() failsafe.Policy[*http.Response] { return buildRetryPolicy(cfg.RetryPolicy) }},
		{cfg.Hedge != nil && cfg.Hedge.Enabled, func() failsafe.Policy[*http.Response] { return buildHedgePolicy(cfg.Hedge) }},
		{cfg.CircuitBreaker != nil && cfg.CircuitBreaker.Enabled, func() failsafe.Policy[*http.Response] { return buildCircuitBreakerPolicy(cfg.CircuitBreaker) }},
		{cfg.RateLimiter != nil && cfg.RateLimiter.Enabled, func() failsafe.Policy[*http.Response] { return buildRateLimiterPolicy(cfg.RateLimiter) }},
		{cfg.AdaptiveThrottler != nil && cfg.AdaptiveThrottler.Enabled, func() failsafe.Policy[*http.Response] { return buildAdaptiveThrottlerPolicy(cfg.AdaptiveThrottler) }},
		{cfg.Bulkhead != nil && cfg.Bulkhead.Enabled, func() failsafe.Policy[*http.Response] { return buildBulkheadPolicy(cfg.Bulkhead) }},
		{cfg.Timeout != nil && cfg.Timeout.Enabled, func() failsafe.Policy[*http.Response] { return buildTimeoutPolicy(cfg.Timeout) }},
	}

	var policies []failsafe.Policy[*http.Response]
	for _, layer := range layers {
		if layer.enabled {
			policies = append(policies, layer.builder())
		}
	}

	return failsafehttp.NewRoundTripper(baseTransport, policies...)
}

func buildHedgePolicy(cfg *HedgeConfig) hedgepolicy.HedgePolicy[*http.Response] {
	builder := hedgepolicy.NewBuilderWithDelay[*http.Response](cfg.Delay).
		WithMaxHedges(cfg.MaxHedges)

	if cfg.CancelOnResult {
		builder.CancelIf(func(res *http.Response, err error) bool {
			return err == nil && res != nil && res.StatusCode < 500
		})
	}
	return builder.Build()
}

func buildBulkheadPolicy(cfg *BulkheadConfig) bulkhead.Bulkhead[*http.Response] {
	return bulkhead.NewBuilder[*http.Response](cfg.MaxConcurrency).
		WithMaxWaitTime(cfg.MaxWaitTime).
		Build() //
}

func buildRateLimiterPolicy(cfg *RateLimiterConfig) ratelimiter.RateLimiter[*http.Response] {
	if cfg.IsBursty {
		return ratelimiter.NewBursty[*http.Response](cfg.MaxExecutions, cfg.Period)
	}
	return ratelimiter.NewSmooth[*http.Response](cfg.MaxExecutions, cfg.Period) //
}

func buildAdaptiveThrottlerPolicy(cfg *AdaptiveThrottlerConfig) adaptivethrottler.AdaptiveThrottler[*http.Response] {
	builder := adaptivethrottler.NewBuilder[*http.Response]().
		WithFailureRateThreshold(cfg.FailureRateThreshold, cfg.MinExecutions, cfg.Period).
		WithMaxRejectionRate(cfg.MaxRejectionRate)

	// Treat network errors and HTTP 5xx responses as failures by default.
	// (By default, net/http only returns an error for transport-level failures;
	// HTTP 5xx still produces a non-nil response, so we must classify it.)
	builder = builder.HandleIf(func(res *http.Response, err error) bool {
		if err != nil {
			return true
		}
		if res == nil {
			return true
		}
		return res.StatusCode >= 500
	})

	return builder.Build()
}

func buildCachePolicy(cfg *CacheConfig) cachepolicy.CachePolicy[*http.Response] {
	return cachepolicy.NewBuilder(cfg.Cache).
		WithKey(cfg.Key).
		Build()
}

func buildCircuitBreakerPolicy(cfg *CircuitBreakerConfig) circuitbreaker.CircuitBreaker[*http.Response] {
	builder := circuitbreaker.NewBuilder[*http.Response]()

	// Handle 5xx errors and network errors by default
	builder = builder.HandleIf(func(res *http.Response, err error) bool {
		if err != nil {
			return true
		}
		if res == nil {
			return true
		}

		return res.StatusCode >= 500
	})

	// Configure failure thresholds
	if cfg.FailureThreshold > 0 {
		builder = builder.WithFailureThreshold(cfg.FailureThreshold)
	}

	if cfg.FailureThresholdRatio != nil {
		builder = builder.WithFailureThresholdRatio(
			cfg.FailureThresholdRatio.Failures,
			cfg.FailureThresholdRatio.Total,
		)
	}

	// Configure success thresholds
	if cfg.SuccessThreshold > 0 {
		builder = builder.WithSuccessThreshold(cfg.SuccessThreshold)
	}

	if cfg.SuccessThresholdRatio != nil {
		builder = builder.WithSuccessThresholdRatio(
			cfg.SuccessThresholdRatio.Failures,
			cfg.SuccessThresholdRatio.Total,
		)
	}

	// Configure delay
	if cfg.Delay > 0 {
		builder = builder.WithDelay(cfg.Delay)
	}

	// Configure event listeners
	if cfg.OnStateChanged != nil {
		builder = builder.OnStateChanged(cfg.OnStateChanged)
	}

	if cfg.OnOpen != nil {
		builder = builder.OnOpen(cfg.OnOpen)
	}

	if cfg.OnHalfOpen != nil {
		builder = builder.OnHalfOpen(cfg.OnHalfOpen)
	}

	if cfg.OnClose != nil {
		builder = builder.OnClose(cfg.OnClose)
	}

	return builder.Build()
}

func buildRetryPolicy(cfg *RetryPolicyConfig) retrypolicy.RetryPolicy[*http.Response] {
	builder := retrypolicy.NewBuilder[*http.Response]()

	// Configure max attempts/retries
	if cfg.MaxAttempts > 0 {
		builder = builder.WithMaxAttempts(cfg.MaxAttempts)
	}

	if cfg.MaxRetries > 0 {
		builder = builder.WithMaxRetries(cfg.MaxRetries)
	}

	if cfg.MaxDuration > 0 {
		builder = builder.WithMaxDuration(cfg.MaxDuration)
	}

	// Configure delay strategies
	if cfg.Delay > 0 {
		builder = builder.WithDelay(cfg.Delay)
	}

	if cfg.BackoffInitial > 0 && cfg.BackoffMax > 0 {
		builder = builder.WithBackoff(cfg.BackoffInitial, cfg.BackoffMax)
	}

	if cfg.RandomDelayMin > 0 && cfg.RandomDelayMax > 0 {
		builder = builder.WithRandomDelay(cfg.RandomDelayMin, cfg.RandomDelayMax)
	}

	if cfg.JitterFactor > 0 {
		builder = builder.WithJitterFactor(cfg.JitterFactor)
	}

	if cfg.Jitter > 0 {
		builder = builder.WithJitter(cfg.Jitter)
	}

	// Configure abort conditions FIRST (highest priority)
	if len(cfg.AbortOnStatus) > 0 {
		builder = builder.AbortIf(func(res *http.Response, err error) bool {
			if err != nil || res == nil {
				return false
			}

			for _, status := range cfg.AbortOnStatus {
				if res.StatusCode == status {
					return true
				}
			}
			return false
		})
	} else {
		// Default: abort on 4xx errors (authentication, validation, etc)
		builder = builder.AbortIf(func(res *http.Response, err error) bool {
			if err != nil || res == nil {
				return false
			}

			// Abort on 4xx status codes (except 429 which is rate limiting)
			if res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
				return true
			}

			return false
		})
	}

	// Configure retryable status codes
	if len(cfg.RetryableStatus) > 0 {
		builder = builder.HandleIf(func(res *http.Response, err error) bool {
			// Always retry on network errors
			if err != nil {
				return true
			}

			if res == nil {
				return true
			}

			// Retry on specific status codes
			for _, status := range cfg.RetryableStatus {
				if res.StatusCode == status {
					return true
				}
			}
			return false
		})
	} else {
		// Default: retry on 5xx and network errors only
		builder = builder.HandleIf(func(res *http.Response, err error) bool {
			// Always retry on network errors
			if err != nil {
				return true
			}

			if res == nil {
				return true
			}

			// Default: only retry 5xx errors
			return res.StatusCode >= 500
		})
	}

	// Configure event listeners
	if cfg.OnRetry != nil {
		builder = builder.OnRetry(cfg.OnRetry)
	}

	if cfg.OnRetriesExceeded != nil {
		builder = builder.OnRetriesExceeded(cfg.OnRetriesExceeded)
	}

	if cfg.OnAbort != nil {
		builder = builder.OnAbort(cfg.OnAbort)
	}

	return builder.Build()
}

func buildTimeoutPolicy(cfg *TimeoutConfig) timeout.Timeout[*http.Response] {
	builder := timeout.NewBuilder[*http.Response](cfg.Duration)

	if cfg.OnTimeoutExceeded != nil {
		builder = builder.OnTimeoutExceeded(cfg.OnTimeoutExceeded)
	}

	return builder.Build()
}

func buildFallbackPolicy(cfg *FallbackConfig) fallback.Fallback[*http.Response] {
	builder := fallback.NewBuilderWithFunc[*http.Response](cfg.FallbackFunc)

	builder = builder.HandleIf(func(res *http.Response, err error) bool {
		if err != nil {
			return true
		}
		if res == nil {
			return true
		}

		return res.StatusCode >= 500
	})

	// Configure event listener
	if cfg.OnFallbackExecuted != nil {
		builder = builder.OnFallbackExecuted(cfg.OnFallbackExecuted)
	}

	return builder.Build()
}

// DefaultResilienceConfig returns a production-ready default configuration
func DefaultResilienceConfig() *ResilienceConfig {
	return &ResilienceConfig{
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 5,
			SuccessThreshold: 2,
			Delay:            10 * time.Second,
		},
		RetryPolicy: &RetryPolicyConfig{
			Enabled:         true,
			MaxAttempts:     3,
			BackoffInitial:  100 * time.Millisecond,
			BackoffMax:      2 * time.Second,
			JitterFactor:    0.1,
			RetryableStatus: []int{500, 502, 503, 504, 429},
		},
		Timeout: &TimeoutConfig{
			Enabled:  true,
			Duration: 15 * time.Second,
		},
		RateLimiter: &RateLimiterConfig{
			Enabled:       true,
			MaxExecutions: 100,
			Period:        time.Second,
			MaxWaitTime:   500 * time.Millisecond,
		},
		Bulkhead: &BulkheadConfig{
			Enabled:        true,
			MaxConcurrency: 20,
			MaxWaitTime:    time.Second,
		},
	}
}

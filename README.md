# go-httpclient

[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.25-blue)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/ja7ad/httpclient)](https://goreportcard.com/report/github.com/ja7ad/httpclient)
[![GoDoc](https://godoc.org/github.com/ja7ad/httpclient?status.svg)](https://godoc.org/github.com/ja7ad/httpclient)

A production-ready HTTP client for Go with **built-in resilience patterns** (retry, circuit breaker, timeouts, fallback, and more). It’s designed for reliable microservices and external API integrations.

## Features

### Resilience patterns (built-in)

- **Retry policy**: max attempts/retries, max duration, fixed delay, exponential backoff, random delay, jitter
- **Circuit breaker**: count- or ratio-based thresholds with open/half-open/close hooks
- **Timeout**: per-execution timeout policy (in addition to `context.Context`)
- **Fallback**: custom fallback response when failures happen
- **Rate limiter**: smooth or bursty limiting
- **Bulkhead**: limit concurrent executions and queue wait time
- **Hedging**: tail-latency hedging with optional cancellation on first good result
- **Adaptive throttling**: automatically reject requests when failure rate is high
- **Cache (read-through)**: cache `*http.Response` via `failsafe-go` cache policy

### Developer experience

- Simple API (`Get`, `Post`, `Do`, plus `*JSON` helpers)
- Built-in JSON marshaling/unmarshaling
- `context.Context` support for cancellation and deadlines
- Typed errors with rich context + pluggable error body parsers

## Installation

```bash
go get github.com/ja7ad/httpclient
```

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ja7ad/httpclient"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	// NewDefaultClient sets JSON headers and enables a production-ready resilience config.
	client := httpclient.NewDefaultClient("https://jsonplaceholder.typicode.com")

	var user User
	if err := client.GetJSON(context.Background(), "/users/1", &user); err != nil {
		log.Fatalf("request failed: %v", err)
	}

	fmt.Printf("User: %+v\n", user)
}
```

## Client configuration

### Constructors

- `NewDefaultClient(baseURL)`
  - sets `Content-Type: application/json` and `Accept: application/json`
  - enables `DefaultResilienceConfig()`
- `NewClient(...ClientOption)`
  - fully configurable via options

### Options

Available `ClientOption`s:

- `WithBaseURL(baseURL string)`
- `WithTimeout(timeout time.Duration)` (applied to `http.Client.Timeout` when it’s 0)
- `WithHeader(key, value string)` / `WithHeaders(map[string]string)`
- `WithResilienceConfig(cfg *ResilienceConfig)` (wraps the transport with resilience policies)
- `WithHTTPClient(httpClient *http.Client)`
- `WithErrorParser(parser ErrorResponseParser)` (adds a parser to the error parsing chain)

## Make Client Faster

We use encoding/json as default json library due to stability and producibility. However, the standard library is a 
bit slow compared to 3rd party libraries. If you're not happy with the performance of encoding/json, we recommend you 
to use these libraries:

- [goccy/go-json](https://github.com/goccy/go-json)
- [bytedance/sonic](https://github.com/bytedance/sonic)
- [segmentio/encoding](https://github.com/segmentio/encoding)
- [mailru/easyjson](https://github.com/mailru/easyjson)
- [minio/simdjson-go](https://github.com/minio/simdjson-go)
- [wI2L/jettison](https://github.com/wI2L/jettison)

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ja7ad/httpclient"
	"github.com/bytedance/sonic"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	// NewDefaultClient sets JSON headers and enables a production-ready resilience config.
	client := httpclient.NewClient(
		httpclient.WithBaseURL("https://jsonplaceholder.typicode.com"),
	    httpclient.WithCustomJsonMarshaler(sonic.Marshal),
        httpclient.WithCustomJsonUnmarshaler(sonic.Unmarshal),
	)

	var user User
	if err := client.GetJSON(context.Background(), "/users/1", &user); err != nil {
		log.Fatalf("request failed: %v", err)
	}

	fmt.Printf("User: %+v\n", user)
}
```

## Resilience

### How policies are applied

Policies wrap a base `http.RoundTripper` in this order (innermost → outermost):

**Fallback → Cache → Retry → Hedge → CircuitBreaker → RateLimiter → AdaptiveThrottler → Bulkhead → Timeout**

### Using `ResilienceConfig`

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/ja7ad/httpclient"
)

func main() {
	cfg := &httpclient.ResilienceConfig{
		RetryPolicy: &httpclient.RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    5,
			BackoffInitial: 100 * time.Millisecond,
			BackoffMax:     3 * time.Second,
			JitterFactor:   0.2,
			RetryableStatus: []int{
				http.StatusInternalServerError,
				http.StatusBadGateway,
				http.StatusServiceUnavailable,
				http.StatusGatewayTimeout,
				http.StatusTooManyRequests,
			},
			OnRetry: func(event failsafe.ExecutionEvent[*http.Response]) {
				fmt.Printf("retry attempt: %d\n", event.Attempts())
			},
		},
		CircuitBreaker: &httpclient.CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 3,
			SuccessThreshold: 2,
			Delay:            5 * time.Second,
			OnOpen: func(event circuitbreaker.StateChangedEvent) {
				fmt.Println("circuit opened")
			},
			OnHalfOpen: func(event circuitbreaker.StateChangedEvent) {
				fmt.Println("circuit half-open")
			},
			OnClose: func(event circuitbreaker.StateChangedEvent) {
				fmt.Println("circuit closed")
			},
		},
		Timeout: &httpclient.TimeoutConfig{
			Enabled:  true,
			Duration: 10 * time.Second,
		},
		RateLimiter: &httpclient.RateLimiterConfig{
			Enabled:       true,
			MaxExecutions: 50,
			Period:        time.Second,
			IsBursty:      false,
		},
		Bulkhead: &httpclient.BulkheadConfig{
			Enabled:        true,
			MaxConcurrency: 10,
			MaxWaitTime:    200 * time.Millisecond,
		},
	}

	client := httpclient.NewClient(
		httpclient.WithBaseURL("https://httpstat.us"),
		httpclient.WithResilienceConfig(cfg),
	)

	resp, err := client.Get(context.Background(), "/200")
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	fmt.Println("status:", resp.StatusCode)
}
```

### Using `ResilienceBuilder` (fluent)

```go
package main

import (
	"time"

	"github.com/ja7ad/httpclient"
)

func main() {
	cfg := httpclient.NewResilienceBuilder().
		WithRetryPolicy(&httpclient.RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    3,
			BackoffInitial: 100 * time.Millisecond,
			BackoffMax:     2 * time.Second,
			JitterFactor:   0.1,
		}).
		WithCircuitBreaker(&httpclient.CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 5,
			SuccessThreshold: 2,
			Delay:            10 * time.Second,
		}).
		WithRateLimiter(&httpclient.RateLimiterConfig{
			Enabled:       true,
			MaxExecutions: 100,
			Period:        time.Second,
			IsBursty:      true,
		}).
		WithBulkhead(&httpclient.BulkheadConfig{
			Enabled:        true,
			MaxConcurrency: 20,
			MaxWaitTime:    time.Second,
		}).
		Build()

	_ = cfg
}
```

### Fallback example

```go
package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/failsafe-go/failsafe-go"
	"github.com/ja7ad/httpclient"
)

func main() {
	cfg := &httpclient.ResilienceConfig{
		Fallback: &httpclient.FallbackConfig{
			Enabled: true,
			FallbackFunc: func(exec failsafe.Execution[*http.Response]) (*http.Response, error) {
				// Return a synthetic response when the primary request fails.
				body := `{"id": 1, "name": "Cached User"}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			},
		},
	}

	client := httpclient.NewClient(
		httpclient.WithBaseURL("https://invalid-domain-that-does-not-exist.com"),
		httpclient.WithResilienceConfig(cfg),
	)

	resp, err := client.Get(context.Background(), "/users/1")
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	log.Println("status:", resp.StatusCode)
}
```

## Errors

All errors returned by this package are standard Go `error`s. For richer information, use `errors.As` to unwrap `*httpclient.Error`.

### Typed error fields

`*httpclient.Error` includes:

- `Type` (`timeout`, `network`, `http`, `retry_exhausted`, `validation`, `unknown`)
- `StatusCode` (for HTTP errors)
- `URL`, `Method`
- `RequestID` (from `X-Request-ID`, if present)
- `CorrelationID`, `ErrorCode`, `Details` (parsed from the error response body when possible)

### Custom error response parsing

By default, the client parses common API error formats using an `ErrorParserChain`:

1. Sumsub
2. Stripe
3. RFC 7807 (Problem Details)
4. Generic JSON parser (always last)

You can add your own parser with `WithErrorParser(...)`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/ja7ad/httpclient"
)

func main() {
	client := httpclient.NewDefaultClient("https://api.sumsub.com")
	client.SetHeader("Authorization", "Bearer your-token")

	var result map[string]any
	err := client.GetJSON(context.Background(), "/resources/applicants/12313213", &result)
	if err == nil {
		return
	}

	var httpErr *httpclient.Error
	if errors.As(err, &httpErr) {
		fmt.Printf("type: %s\n", httpErr.Type)
		fmt.Printf("message: %s\n", httpErr.Message)
		fmt.Printf("status: %d\n", httpErr.StatusCode)
		fmt.Printf("errorCode: %s\n", httpErr.ErrorCode)
		fmt.Printf("correlationID: %s\n", httpErr.CorrelationID)
		if v, ok := httpErr.GetDetail("description"); ok {
			fmt.Printf("description: %v\n", v)
		}
		return
	}

	log.Printf("request failed: %v", err)
}
```

## License

MIT. See [LICENSE](LICENSE).


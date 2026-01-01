# go-httpclient

[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.25-blue)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/ja7ad/httpclient)](https://goreportcard.com/report/github.com/ja7ad/httpclient)
[![GoDoc](https://godoc.org/github.com/ja7ad/httpclient?status.svg)](https://godoc.org/github.com/ja7ad/httpclient)

A production-ready HTTP client for Go with **built-in resilience patterns** including retry policies, circuit breakers, timeouts, and fallback mechanisms. Perfect for building reliable microservices and external API integrations.

## Features

✨ **Resilience Patterns**
- 🔄 **Retry Policy** - Exponential backoff, jitter, max attempts/duration
- ⚡ **Circuit Breaker** - Prevent cascading failures with automatic recovery
- ⏱️ **Timeout** - Request-level and global timeout controls
- 🛡️ **Fallback** - Graceful degradation with custom fallback responses

🎯 **Developer Experience**
- Simple, intuitive API with method chaining
- JSON marshaling/unmarshaling built-in
- Context support for cancellation and deadlines
- Typed error handling with detailed context

## Installation

```bash
go get github.com/ja7ad/httpclient
```

## Examples

### Basic

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ja7ad/httpclient"
)

type User struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

func main() {
	client := httpclient.NewDefaultClient("https://jsonplaceholder.typicode.com")

	var user User
	err := client.GetJSON(context.Background(), "/users/1", &user)
	if err != nil {
		log.Fatalf("Failed to get user: %v", err)
	}

	fmt.Printf("User ID: %d\n", user.ID)
	fmt.Printf("Name: %s\n", user.Name)
	fmt.Printf("Email: %s\n", user.Email)
	fmt.Printf("Username: %s\n", user.Username)
}
```

### CRUD

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ja7ad/httpclient"
)

type Post struct {
	ID     int    `json:"id"`
	UserID int    `json:"userId"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

type CreatePostRequest struct {
	UserID int    `json:"userId"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

func main() {
	client := httpclient.NewDefaultClient("https://jsonplaceholder.typicode.com")
	ctx := context.Background()

	fmt.Println("=== GET: Fetch a post ===")
	getExample(client, ctx)

	fmt.Println("\n=== POST: Create a new post ===")
	postExample(client, ctx)

	fmt.Println("\n=== PUT: Update a post ===")
	putExample(client, ctx)

	fmt.Println("\n=== DELETE: Delete a post ===")
	deleteExample(client, ctx)
}

func getExample(client *httpclient.Client, ctx context.Context) {
	var post Post
	err := client.GetJSON(ctx, "/posts/1", &post)
	if err != nil {
		log.Printf("GET failed: %v", err)
		return
	}

	fmt.Printf("Post: %+v\n", post)
}

func postExample(client *httpclient.Client, ctx context.Context) {
	newPost := CreatePostRequest{
		UserID: 1,
		Title:  "My New Post",
		Body:   "This is the content of my new post",
	}

	var createdPost Post
	err := client.PostJSON(ctx, "/posts", newPost, &createdPost)
	if err != nil {
		log.Printf("POST failed: %v", err)
		return
	}

	fmt.Printf("Created Post: %+v\n", createdPost)
}

func putExample(client *httpclient.Client, ctx context.Context) {
	updatedPost := CreatePostRequest{
		UserID: 1,
		Title:  "Updated Post Title",
		Body:   "This is the updated content",
	}

	var post Post
	err := client.PutJSON(ctx, "/posts/1", updatedPost, &post)
	if err != nil {
		log.Printf("PUT failed: %v", err)
		return
	}

	fmt.Printf("Updated Post: %+v\n", post)
}

func deleteExample(client *httpclient.Client, ctx context.Context) {
	resp, err := client.Delete(ctx, "/posts/1")
	if err != nil {
		log.Printf("DELETE failed: %v", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Delete Status: %d\n", resp.StatusCode)
}
```

### With Resilience

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
	fmt.Println("=== Retry Example ===")
	retryExample()

	fmt.Println("\n=== Circuit Breaker Example ===")
	circuitBreakerExample()

	fmt.Println("\n=== Timeout Example ===")
	timeoutExample()

	fmt.Println("\n=== Combined Resilience Example ===")
	combinedExample()

	fmt.Println("\n=== Fallback Example ===")
	fallbackExample()
}

func retryExample() {
	cfg := &httpclient.ResilienceConfig{
		RetryPolicy: &httpclient.RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    5,
			BackoffInitial: 100 * time.Millisecond,
			BackoffMax:     5 * time.Second,
			JitterFactor:   0.2,
			RetryableStatus: []int{
				http.StatusInternalServerError,
				http.StatusBadGateway,
				http.StatusServiceUnavailable,
				http.StatusGatewayTimeout,
			},
			OnRetry: func(event failsafe.ExecutionEvent[*http.Response]) {
				fmt.Printf("Retry attempt #%d\n", event.Attempts())
			},
		},
	}

	client := httpclient.NewClient(
		httpclient.WithBaseURL("https://httpstat.us"),
		httpclient.WithResilienceConfig(cfg),
	)

	resp, err := client.Get(context.Background(), "/200")
	if err != nil {
		log.Printf("Request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Success! Status: %d\n", resp.StatusCode)
}

func circuitBreakerExample() {
	cfg := &httpclient.ResilienceConfig{
		CircuitBreaker: &httpclient.CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 3,
			SuccessThreshold: 2,
			Delay:            5 * time.Second,
			OnOpen: func(event circuitbreaker.StateChangedEvent) {
				fmt.Println("⚠️  Circuit breaker opened!")
			},
			OnClose: func(event circuitbreaker.StateChangedEvent) {
				fmt.Println("✅ Circuit breaker closed!")
			},
			OnHalfOpen: func(event circuitbreaker.StateChangedEvent) {
				fmt.Println("🔄 Circuit breaker half-open, testing...")
			},
		},
	}

	client := httpclient.NewClient(
		httpclient.WithBaseURL("https://httpstat.us"),
		httpclient.WithResilienceConfig(cfg),
	)

	for i := 1; i <= 5; i++ {
		fmt.Printf("\nRequest #%d\n", i)
		resp, err := client.Get(context.Background(), "/200")
		if err != nil {
			log.Printf("Request failed: %v", err)
			continue
		}
		resp.Body.Close()
		fmt.Printf("Success! Status: %d\n", resp.StatusCode)
	}
}

func timeoutExample() {
	cfg := &httpclient.ResilienceConfig{
		Timeout: &httpclient.TimeoutConfig{
			Enabled:  true,
			Duration: 2 * time.Second,
			OnTimeoutExceeded: func(event failsafe.ExecutionDoneEvent[*http.Response]) {
				fmt.Println("⏰ Request timed out!")
			},
		},
	}

	client := httpclient.NewClient(
		httpclient.WithBaseURL("https://httpstat.us"),
		httpclient.WithResilienceConfig(cfg),
	)

	resp, err := client.Get(context.Background(), "/200?sleep=1000")
	if err != nil {
		log.Printf("Request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Success within timeout! Status: %d\n", resp.StatusCode)
}

func combinedExample() {
	cfg := &httpclient.ResilienceConfig{
		CircuitBreaker: &httpclient.CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 5,
			SuccessThreshold: 2,
			Delay:            10 * time.Second,
		},
		RetryPolicy: &httpclient.RetryPolicyConfig{
			Enabled:        true,
			MaxAttempts:    3,
			BackoffInitial: 100 * time.Millisecond,
			BackoffMax:     10 * time.Second,
			JitterFactor:   0.1,
			RetryableStatus: []int{
				http.StatusInternalServerError,
				http.StatusBadGateway,
				http.StatusServiceUnavailable,
			},
		},
		Timeout: &httpclient.TimeoutConfig{
			Enabled:  true,
			Duration: 30 * time.Second,
		},
	}

	client := httpclient.NewClient(
		httpclient.WithBaseURL("https://jsonplaceholder.typicode.com"),
		httpclient.WithResilienceConfig(cfg),
	)

	type User struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	var user User
	err := client.GetJSON(context.Background(), "/users/1", &user)
	if err != nil {
		log.Printf("Request failed: %v", err)
		return
	}

	fmt.Printf("User: %+v\n", user)
}

func fallbackExample() {
	cfg := &httpclient.ResilienceConfig{
		Fallback: &httpclient.FallbackConfig{
			Enabled: true,
			FallbackFunc: func(exec failsafe.Execution[*http.Response]) (*http.Response, error) {
				fmt.Println("🛡️  Fallback triggered! Returning cached response...")
				
				cachedResponse := `{"id": 1, "name": "Cached User", "email": "cached@example.com"}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       http.NoBody,
					Header:     make(http.Header),
				}, nil
			},
			OnFallbackExecuted: func(event failsafe.ExecutionDoneEvent[*http.Response]) {
				fmt.Println("✅ Fallback executed successfully")
			},
		},
	}

	client := httpclient.NewClient(
		httpclient.WithBaseURL("https://invalid-domain-that-does-not-exist.com"),
		httpclient.WithResilienceConfig(cfg),
	)

	resp, err := client.Get(context.Background(), "/users/1")
	if err != nil {
		log.Printf("Request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Response Status: %d\n", resp.StatusCode)
}
```

### Custom Error Response

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ja7ad/httpclient"
)

func main() {
	// Create client with automatic error parsing
	client := httpclient.NewDefaultClient("https://api.sumsub.com")
	client.SetHeader("Authorization", "Bearer your-token")

	var result map[string]interface{}
	err := client.GetJSON(context.Background(), "/resources/applicants/12313213", &result)
	
	if err != nil {
		var httpErr *httpclient.Error
		if errors.As(err, &httpErr) {
			fmt.Printf("Error: %s\n", httpErr.Message)
			fmt.Printf("Status Code: %d\n", httpErr.StatusCode)
			fmt.Printf("Error Code: %s\n", httpErr.ErrorCode)
			fmt.Printf("Correlation ID: %s\n", httpErr.CorrelationID)
			
			// Access specific details
			if desc, ok := httpErr.GetDetail("description"); ok {
				fmt.Printf("Description: %v\n", desc)
			}
			
			// Check all details
			for key, value := range httpErr.Details {
				fmt.Printf("%s: %v\n", key, value)
			}
		}
	}
}
```

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
- Comprehensive test coverage (>95%)

🏗️ **Production Ready**
- Built on [failsafe-go](https://github.com/failsafe-go/failsafe-go) - battle-tested resilience library
- Clean Architecture principles
- Zero external dependencies (except failsafe-go)
- Fully documented with examples

## Installation

```bash
go get github.com/ja7ad/httpclient
```
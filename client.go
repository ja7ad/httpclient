package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var _ Executor = (*Client)(nil)

type Executor interface {
	Get(ctx context.Context, path string) (*http.Response, error)
	Post(ctx context.Context, path string, body any) (*http.Response, error)
	Put(ctx context.Context, path string, body any) (*http.Response, error)
	Patch(ctx context.Context, path string, body any) (*http.Response, error)
	Delete(ctx context.Context, path string) (*http.Response, error)
	Do(ctx context.Context, method, path string, body any) (*http.Response, error)
	DoWithResponse(ctx context.Context, method, path string, body, result any) error
	GetJSON(ctx context.Context, path string, result any) error
	PostJSON(ctx context.Context, path string, body, result any) error
	PutJSON(ctx context.Context, path string, body, result any) error
	PatchJSON(ctx context.Context, path string, body, result any) error
	DeleteJSON(ctx context.Context, path string, result any) error
	GetErrorParser() *ErrorParserChain
	SetErrorParser(parser *ErrorParserChain)
	SetBaseURL(baseURL string)
	SetHeader(key, value string)
	SetHeaders(headers map[string]string)
	RemoveHeader(key string)
	GetHTTPClient() *http.Client
	SetHTTPClient(httpClient *http.Client)
}

type Client struct {
	httpClient  *http.Client
	baseURL     string
	headers     map[string]string
	timeout     time.Duration
	errorParser *ErrorParserChain

	marshal   JSONMarshal
	unmarshal JSONUnmarshal

	mu sync.RWMutex
}

func NewClient(opts ...ClientOption) *Client {
	// Avoid mutating http.DefaultClient (global) by default.
	defaultHTTPClient := &http.Client{Transport: http.DefaultTransport}

	client := &Client{
		httpClient:  defaultHTTPClient,
		headers:     make(map[string]string),
		timeout:     30 * time.Second,
		errorParser: NewErrorParserChain(),
		marshal:     json.Marshal,
		unmarshal:   json.Unmarshal,
	}

	for _, opt := range opts {
		opt(client)
	}

	client.mu.Lock()
	if client.httpClient != nil && client.httpClient.Timeout == 0 {
		client.httpClient.Timeout = client.timeout
	}
	client.mu.Unlock()

	return client
}

func NewDefaultClient(baseURL string) *Client {
	return NewClient(
		WithBaseURL(baseURL),
		WithResilienceConfig(DefaultResilienceConfig()),
		WithHeader("Content-Type", "application/json"),
		WithHeader("Accept", "application/json"),
	)
}

func (c *Client) Get(ctx context.Context, path string) (*http.Response, error) {
	return c.Do(ctx, http.MethodGet, path, nil)
}

func (c *Client) Post(ctx context.Context, path string, body any) (*http.Response, error) {
	return c.Do(ctx, http.MethodPost, path, body)
}

func (c *Client) Put(ctx context.Context, path string, body any) (*http.Response, error) {
	return c.Do(ctx, http.MethodPut, path, body)
}

func (c *Client) Patch(ctx context.Context, path string, body any) (*http.Response, error) {
	return c.Do(ctx, http.MethodPatch, path, body)
}

func (c *Client) Delete(ctx context.Context, path string) (*http.Response, error) {
	return c.Do(ctx, http.MethodDelete, path, nil)
}

func (c *Client) Do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	urlStr := c.buildURL(path)

	var bodyBytes []byte
	var err error
	if body != nil {
		bodyBytes, err = c.marshalBody(body)
		if err != nil {
			return nil, newError(
				ErrorTypeValidation,
				"failed to marshal request body",
				urlStr,
				method,
				"",
				err,
			)
		}
	}

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		return nil, newError(
			ErrorTypeValidation,
			"failed to create request",
			urlStr,
			method,
			"",
			err,
		)
	}

	if bodyBytes != nil {
		b := bodyBytes
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		}
		req.ContentLength = int64(len(bodyBytes))
	}

	c.mu.RLock()
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	httpClient := c.httpClient
	c.mu.RUnlock()

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, c.wrapError(err, urlStr, method, "")
	}

	return resp, nil
}

func (c *Client) DoWithResponse(ctx context.Context, method, path string, body, result any) error {
	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return c.handleHTTPError(resp, path, method)
	}

	if result != nil && resp.StatusCode != http.StatusNoContent {
		decErr := json.NewDecoder(resp.Body).Decode(result)
		if decErr != nil {
			return newError(
				ErrorTypeValidation,
				"failed to decode response body",
				c.buildURL(path),
				method,
				resp.Header.Get("X-Request-ID"),
				decErr,
			)
		}
	}

	return nil
}

func (c *Client) GetJSON(ctx context.Context, path string, result any) error {
	return c.DoWithResponse(ctx, http.MethodGet, path, nil, result)
}

func (c *Client) PostJSON(ctx context.Context, path string, body, result any) error {
	return c.DoWithResponse(ctx, http.MethodPost, path, body, result)
}

func (c *Client) PutJSON(ctx context.Context, path string, body, result any) error {
	return c.DoWithResponse(ctx, http.MethodPut, path, body, result)
}

func (c *Client) PatchJSON(ctx context.Context, path string, body, result any) error {
	return c.DoWithResponse(ctx, http.MethodPatch, path, body, result)
}

func (c *Client) DeleteJSON(ctx context.Context, path string, result any) error {
	return c.DoWithResponse(ctx, http.MethodDelete, path, nil, result)
}

func (c *Client) GetErrorParser() *ErrorParserChain {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.errorParser
}

func (c *Client) SetErrorParser(parser *ErrorParserChain) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errorParser = parser
}

func (c *Client) SetBaseURL(baseURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = strings.TrimRight(baseURL, "/")
}

func (c *Client) SetHeader(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.headers == nil {
		c.headers = make(map[string]string)
	}
	c.headers[key] = value
}

func (c *Client) SetHeaders(headers map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.headers == nil {
		c.headers = make(map[string]string)
	}
	for k, v := range headers {
		c.headers[k] = v
	}
}

func (c *Client) RemoveHeader(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.headers, key)
}

func (c *Client) GetHTTPClient() *http.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.httpClient
}

func (c *Client) SetHTTPClient(httpClient *http.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.httpClient = httpClient
}

func (c *Client) buildURL(path string) string {
	if u, err := url.Parse(path); err == nil && u.IsAbs() {
		return path
	}

	c.mu.RLock()
	base := c.baseURL
	c.mu.RUnlock()

	if base == "" {
		return path
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		p := strings.TrimLeft(path, "/")
		return fmt.Sprintf("%s/%s", strings.TrimRight(base, "/"), p)
	}

	relURL, err := url.Parse(path)
	if err != nil {
		rel := strings.TrimLeft(path, "/")
		return baseURL.JoinPath(rel).String()
	}

	relPath := strings.TrimLeft(relURL.Path, "/")
	joined := baseURL.JoinPath(relPath)

	joined.RawQuery = relURL.RawQuery
	joined.Fragment = relURL.Fragment

	return joined.String()
}

func (c *Client) marshalBody(body any) ([]byte, error) {
	switch v := body.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	case io.Reader:
		return io.ReadAll(v)
	default:
		return json.Marshal(body)
	}
}

func (c *Client) wrapError(err error, urlStr, method, requestID string) error {
	if err == nil {
		return nil
	}

	var httpErr *Error
	if errors.As(err, &httpErr) {
		return err
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return newError(
			ErrorTypeTimeout,
			"request timeout",
			urlStr,
			method,
			requestID,
			err,
		)
	}

	if errors.Is(err, context.Canceled) {
		return newError(
			ErrorTypeTimeout,
			"request canceled",
			urlStr,
			method,
			requestID,
			err,
		)
	}

	if strings.Contains(strings.ToLower(err.Error()), "retries exceeded") {
		return newError(
			ErrorTypeRetryExhausted,
			"maximum retry attempts exceeded",
			urlStr,
			method,
			requestID,
			err,
		)
	}

	return newError(
		ErrorTypeNetwork,
		"network request failed",
		urlStr,
		method,
		requestID,
		err,
	)
}

func (c *Client) handleHTTPError(resp *http.Response, path, method string) error {
	requestID := resp.Header.Get("X-Request-ID")
	urlStr := c.buildURL(path)

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return newError(
			ErrorTypeNetwork,
			"failed to read error response body",
			urlStr,
			method,
			requestID,
			readErr,
		)
	}
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	httpErr := newHTTPError(resp.StatusCode, urlStr, method, requestID)
	httpErr.ResponseBody = bodyBytes

	c.mu.RLock()
	parser := c.errorParser
	c.mu.RUnlock()

	if len(bodyBytes) > 0 && parser != nil {
		parsed, err := parser.Parse(bodyBytes)
		if err == nil && parsed != nil {
			if parsed.Message != "" {
				httpErr.Message = parsed.Message
			}
			if parsed.ErrorCode != "" {
				httpErr.ErrorCode = parsed.ErrorCode
			}
			if parsed.CorrelationID != "" {
				httpErr.CorrelationID = parsed.CorrelationID
			}
			if len(parsed.Details) > 0 {
				httpErr.Details = parsed.Details
			}
		}
	}

	return httpErr
}

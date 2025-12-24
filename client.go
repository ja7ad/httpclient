package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	httpClient  *http.Client
	baseURL     string
	headers     map[string]string
	timeout     time.Duration
	errorParser *ErrorParserChain
}

func NewClient(opts ...ClientOption) *Client {
	client := &Client{
		httpClient:  http.DefaultClient,
		headers:     make(map[string]string),
		timeout:     30 * time.Second,
		errorParser: NewErrorParserChain(), // Initialize here
	}

	for _, opt := range opts {
		opt(client)
	}

	if client.httpClient.Timeout == 0 {
		client.httpClient.Timeout = client.timeout
	}

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

func (c *Client) Post(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.Do(ctx, http.MethodPost, path, body)
}

func (c *Client) Put(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.Do(ctx, http.MethodPut, path, body)
}

func (c *Client) Patch(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.Do(ctx, http.MethodPatch, path, body)
}

func (c *Client) Delete(ctx context.Context, path string) (*http.Response, error) {
	return c.Do(ctx, http.MethodDelete, path, nil)
}

func (c *Client) Do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	url := c.buildURL(path)

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := c.marshalBody(body)
		if err != nil {
			return nil, newError(
				ErrorTypeValidation,
				"failed to marshal request body",
				url,
				method,
				"",
				err,
			)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, newError(
			ErrorTypeValidation,
			"failed to create request",
			url,
			method,
			"",
			err,
		)
	}

	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, c.wrapError(err, url, method, "")
	}

	return resp, nil
}

func (c *Client) DoWithResponse(ctx context.Context, method, path string, body, result interface{}) error {
	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.handleHTTPError(resp, path, method)
	}

	if result != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return newError(
				ErrorTypeValidation,
				"failed to decode response body",
				c.buildURL(path),
				method,
				resp.Header.Get("X-Request-ID"),
				err,
			)
		}
	}

	return nil
}

func (c *Client) GetJSON(ctx context.Context, path string, result interface{}) error {
	return c.DoWithResponse(ctx, http.MethodGet, path, nil, result)
}

func (c *Client) PostJSON(ctx context.Context, path string, body, result interface{}) error {
	return c.DoWithResponse(ctx, http.MethodPost, path, body, result)
}

func (c *Client) PutJSON(ctx context.Context, path string, body, result interface{}) error {
	return c.DoWithResponse(ctx, http.MethodPut, path, body, result)
}

func (c *Client) PatchJSON(ctx context.Context, path string, body, result interface{}) error {
	return c.DoWithResponse(ctx, http.MethodPatch, path, body, result)
}

func (c *Client) DeleteJSON(ctx context.Context, path string, result interface{}) error {
	return c.DoWithResponse(ctx, http.MethodDelete, path, nil, result)
}

func (c *Client) GetErrorParser() *ErrorParserChain {
	return c.errorParser
}

func (c *Client) SetErrorParser(parser *ErrorParserChain) {
	c.errorParser = parser
}

func (c *Client) SetBaseURL(baseURL string) {
	c.baseURL = strings.TrimRight(baseURL, "/")
}

func (c *Client) SetHeader(key, value string) {
	if c.headers == nil {
		c.headers = make(map[string]string)
	}
	c.headers[key] = value
}

func (c *Client) SetHeaders(headers map[string]string) {
	if c.headers == nil {
		c.headers = make(map[string]string)
	}
	for k, v := range headers {
		c.headers[k] = v
	}
}

func (c *Client) RemoveHeader(key string) {
	delete(c.headers, key)
}

func (c *Client) GetHTTPClient() *http.Client {
	return c.httpClient
}

func (c *Client) SetHTTPClient(httpClient *http.Client) {
	c.httpClient = httpClient
}

func (c *Client) buildURL(path string) string {
	if c.baseURL == "" {
		return path
	}

	path = strings.TrimLeft(path, "/")
	return fmt.Sprintf("%s/%s", c.baseURL, path)
}

func (c *Client) marshalBody(body interface{}) ([]byte, error) {
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

func (c *Client) wrapError(err error, url, method, requestID string) error {
	if err == nil {
		return nil
	}

	var httpErr *Error
	if errors.As(err, &httpErr) {
		return err
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return newError(
			ErrorTypeTimeout,
			"request timeout or cancelled",
			url,
			method,
			requestID,
			err,
		)
	}

	if strings.Contains(err.Error(), "retries exceeded") {
		return newError(
			ErrorTypeRetryExhausted,
			"maximum retry attempts exceeded",
			url,
			method,
			requestID,
			err,
		)
	}

	return newError(
		ErrorTypeNetwork,
		"network request failed",
		url,
		method,
		requestID,
		err,
	)
}

func (c *Client) handleHTTPError(resp *http.Response, path, method string) error {
	requestID := resp.Header.Get("X-Request-ID")
	url := c.buildURL(path)

	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	httpErr := newHTTPError(resp.StatusCode, url, method, requestID)
	httpErr.ResponseBody = bodyBytes

	if len(bodyBytes) > 0 {
		parsed, err := c.errorParser.Parse(bodyBytes)
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

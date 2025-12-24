package httpclient

import (
	"net/http"
	"strings"
	"time"
)

type ClientOption func(*Client)

func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.timeout = timeout
	}
}

func WithHeaders(headers map[string]string) ClientOption {
	return func(c *Client) {
		if c.headers == nil {
			c.headers = make(map[string]string)
		}
		for k, v := range headers {
			c.headers[k] = v
		}
	}
}

func WithHeader(key, value string) ClientOption {
	return func(c *Client) {
		if c.headers == nil {
			c.headers = make(map[string]string)
		}
		c.headers[key] = value
	}
}

func WithResilienceConfig(cfg *ResilienceConfig) ClientOption {
	return func(c *Client) {
		c.httpClient = NewResilientClient(cfg)
	}
}

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

func WithErrorParser(parser ErrorResponseParser) ClientOption {
	return func(c *Client) {
		if c.errorParser == nil {
			c.errorParser = NewErrorParserChain()
		}
		c.errorParser.AddParser(parser)
	}
}

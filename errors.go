package httpclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type ErrorType string

const (
	ErrorTypeTimeout        ErrorType = "timeout"
	ErrorTypeNetwork        ErrorType = "network"
	ErrorTypeHTTP           ErrorType = "http"
	ErrorTypeRetryExhausted ErrorType = "retry_exhausted"
	ErrorTypeValidation     ErrorType = "validation"
	ErrorTypeUnknown        ErrorType = "unknown"
)

// Error represents an HTTP client error with detailed context
type Error struct {
	Type          ErrorType              `json:"type"`
	Message       string                 `json:"message"`
	StatusCode    int                    `json:"status_code,omitempty"`
	URL           string                 `json:"url"`
	Method        string                 `json:"method"`
	RequestID     string                 `json:"request_id,omitempty"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	ErrorCode     string                 `json:"error_code,omitempty"`
	Details       map[string]interface{} `json:"details,omitempty"`
	Err           error                  `json:"-"`
	Timestamp     time.Time              `json:"timestamp"`
	ResponseBody  []byte                 `json:"-"`
}

func (e *Error) Error() string {
	if e.CorrelationID != "" {
		return fmt.Sprintf("[%s] %s: %s (correlationID: %s)", e.Type, e.Message, e.URL, e.CorrelationID)
	}
	if e.RequestID != "" {
		return fmt.Sprintf("[%s] %s: %s (requestID: %s)", e.Type, e.Message, e.URL, e.RequestID)
	}
	return fmt.Sprintf("[%s] %s: %s", e.Type, e.Message, e.URL)
}

func (e *Error) Unwrap() error {
	return e.Err
}

// GetDetail retrieves a specific detail from the error response
func (e *Error) GetDetail(key string) (interface{}, bool) {
	if e.Details == nil {
		return nil, false
	}
	val, ok := e.Details[key]
	return val, ok
}

// HasDetail checks if a specific detail key exists
func (e *Error) HasDetail(key string) bool {
	_, ok := e.GetDetail(key)
	return ok
}

func newError(errType ErrorType, message, url, method, requestID string, err error) *Error {
	return &Error{
		Type:      errType,
		Message:   message,
		URL:       url,
		Method:    method,
		RequestID: requestID,
		Err:       err,
		Timestamp: time.Now(),
	}
}

func newHTTPError(statusCode int, url, method, requestID string) *Error {
	return &Error{
		Type:       ErrorTypeHTTP,
		Message:    fmt.Sprintf("HTTP %d %s", statusCode, http.StatusText(statusCode)),
		StatusCode: statusCode,
		URL:        url,
		Method:     method,
		RequestID:  requestID,
		Timestamp:  time.Now(),
	}
}

// ErrorResponseParser defines an interface for parsing custom error responses
type ErrorResponseParser interface {
	Parse(body []byte) (*ParsedErrorResponse, error)
	CanParse(body []byte) bool
}

// ParsedErrorResponse represents a parsed error response from an external API
type ParsedErrorResponse struct {
	Message       string
	ErrorCode     string
	CorrelationID string
	Details       map[string]interface{}
}

// GenericErrorParser parses common error response formats
type GenericErrorParser struct{}

func (p *GenericErrorParser) CanParse(body []byte) bool {
	return len(body) > 0 && (body[0] == '{' || body[0] == '[')
}

func (p *GenericErrorParser) Parse(body []byte) (*ParsedErrorResponse, error) {
	var parsed ParsedErrorResponse
	parsed.Details = make(map[string]interface{})

	// Try multiple common formats
	formats := []map[string]interface{}{
		{}, // Generic format
	}

	for _, format := range formats {
		if err := json.Unmarshal(body, &format); err == nil {
			p.extractFields(format, &parsed)
			break
		}
	}

	return &parsed, nil
}

func (p *GenericErrorParser) extractFields(data map[string]interface{}, parsed *ParsedErrorResponse) {
	// Common message fields
	messageFields := []string{"message", "error", "error_message", "description", "detail", "title"}
	for _, field := range messageFields {
		if val, ok := data[field].(string); ok && val != "" {
			parsed.Message = val
			break
		}
	}

	// Common error code fields
	codeFields := []string{"code", "error_code", "errorCode", "status", "status_code"}
	for _, field := range codeFields {
		if val, ok := data[field]; ok {
			switch v := val.(type) {
			case string:
				parsed.ErrorCode = v
			case float64:
				parsed.ErrorCode = fmt.Sprintf("%.0f", v)
			case int:
				parsed.ErrorCode = fmt.Sprintf("%d", v)
			}
			if parsed.ErrorCode != "" {
				break
			}
		}
	}

	// Common correlation ID fields
	correlationFields := []string{"correlation_id", "correlationId", "request_id", "requestId", "trace_id", "traceId"}
	for _, field := range correlationFields {
		if val, ok := data[field].(string); ok && val != "" {
			parsed.CorrelationID = val
			break
		}
	}

	// Store all fields in details
	for key, value := range data {
		parsed.Details[key] = value
	}
}

// SumsubErrorParser parses Sumsub-specific error responses
type SumsubErrorParser struct {
	GenericErrorParser
}

func (p *SumsubErrorParser) CanParse(body []byte) bool {
	if !p.GenericErrorParser.CanParse(body) {
		return false
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}

	// Sumsub typically has: description, code, correlationId
	_, hasDesc := data["description"]
	_, hasCode := data["code"]
	_, hasCorrelation := data["correlationId"]

	return hasDesc || (hasCode && hasCorrelation)
}

func (p *SumsubErrorParser) Parse(body []byte) (*ParsedErrorResponse, error) {
	var sumsubError struct {
		Description   string `json:"description"`
		Code          int    `json:"code"`
		CorrelationID string `json:"correlationId"`
	}

	if err := json.Unmarshal(body, &sumsubError); err != nil {
		return p.GenericErrorParser.Parse(body)
	}

	parsed := &ParsedErrorResponse{
		Message:       sumsubError.Description,
		ErrorCode:     fmt.Sprintf("%d", sumsubError.Code),
		CorrelationID: sumsubError.CorrelationID,
		Details: map[string]interface{}{
			"description":   sumsubError.Description,
			"code":          sumsubError.Code,
			"correlationId": sumsubError.CorrelationID,
		},
	}

	return parsed, nil
}

// StripeErrorParser parses Stripe-specific error responses
type StripeErrorParser struct {
	GenericErrorParser
}

func (p *StripeErrorParser) CanParse(body []byte) bool {
	if !p.GenericErrorParser.CanParse(body) {
		return false
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}

	// Stripe has an "error" object
	_, hasError := data["error"].(map[string]interface{})
	return hasError
}

func (p *StripeErrorParser) Parse(body []byte) (*ParsedErrorResponse, error) {
	var stripeError struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
			Param   string `json:"param"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &stripeError); err != nil {
		return p.GenericErrorParser.Parse(body)
	}

	parsed := &ParsedErrorResponse{
		Message:   stripeError.Error.Message,
		ErrorCode: stripeError.Error.Code,
		Details: map[string]interface{}{
			"type":  stripeError.Error.Type,
			"code":  stripeError.Error.Code,
			"param": stripeError.Error.Param,
		},
	}

	return parsed, nil
}

// RFC7807ErrorParser parses RFC 7807 Problem Details for HTTP APIs
type RFC7807ErrorParser struct {
	GenericErrorParser
}

func (p *RFC7807ErrorParser) CanParse(body []byte) bool {
	if !p.GenericErrorParser.CanParse(body) {
		return false
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}

	// RFC 7807 typically has: type, title, status, detail
	_, hasType := data["type"]
	_, hasTitle := data["title"]
	return hasType || hasTitle
}

func (p *RFC7807ErrorParser) Parse(body []byte) (*ParsedErrorResponse, error) {
	var rfc7807Error struct {
		Type     string                 `json:"type"`
		Title    string                 `json:"title"`
		Status   int                    `json:"status"`
		Detail   string                 `json:"detail"`
		Instance string                 `json:"instance"`
		Extra    map[string]interface{} `json:"-"`
	}

	if err := json.Unmarshal(body, &rfc7807Error); err != nil {
		return p.GenericErrorParser.Parse(body)
	}

	message := rfc7807Error.Detail
	if message == "" {
		message = rfc7807Error.Title
	}

	parsed := &ParsedErrorResponse{
		Message:   message,
		ErrorCode: fmt.Sprintf("%d", rfc7807Error.Status),
		Details: map[string]interface{}{
			"type":     rfc7807Error.Type,
			"title":    rfc7807Error.Title,
			"status":   rfc7807Error.Status,
			"detail":   rfc7807Error.Detail,
			"instance": rfc7807Error.Instance,
		},
	}

	return parsed, nil
}

// ErrorParserChain tries multiple parsers in order
type ErrorParserChain struct {
	parsers []ErrorResponseParser
}

func NewErrorParserChain() *ErrorParserChain {
	return &ErrorParserChain{
		parsers: []ErrorResponseParser{
			&SumsubErrorParser{},
			&StripeErrorParser{},
			&RFC7807ErrorParser{},
			&GenericErrorParser{}, // Fallback
		},
	}
}

func (c *ErrorParserChain) AddParser(parser ErrorResponseParser) {
	// Insert before the generic parser (always keep it last)
	if len(c.parsers) > 0 {
		c.parsers = append(c.parsers[:len(c.parsers)-1], parser, c.parsers[len(c.parsers)-1])
	} else {
		c.parsers = append(c.parsers, parser)
	}
}

func (c *ErrorParserChain) Parse(body []byte) (*ParsedErrorResponse, error) {
	for _, parser := range c.parsers {
		if parser.CanParse(body) {
			return parser.Parse(body)
		}
	}

	// Fallback to generic parser
	return (&GenericErrorParser{}).Parse(body)
}

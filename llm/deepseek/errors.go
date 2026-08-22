package deepseek

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Code is a stable error classification, mirroring adapter.ts's httpErrorCode
// vocabulary so callers can branch on Code without string-matching Message.
type Code string

const (
	CodeAuth                  Code = "AUTH"
	CodeQuotaExceeded         Code = "QUOTA_EXCEEDED"
	CodeRateLimit             Code = "RATE_LIMIT"
	CodeContextWindowExceeded Code = "CONTEXT_WINDOW_EXCEEDED"
	CodeInvalidRequest        Code = "INVALID_REQUEST"
	CodeServer                Code = "SERVER"
	CodeTransport             Code = "TRANSPORT"
	CodeTimeout               Code = "TIMEOUT"
	CodeAborted               Code = "ABORTED"
	CodeMalformedResponse     Code = "MALFORMED_RESPONSE"
	CodeStreamClosed          Code = "STREAM_CLOSED"
	CodeEmptyResponse         Code = "EMPTY_RESPONSE"
	CodeModelStopped          Code = "MODEL_STOPPED"
	CodeUnsupportedReasoning  Code = "UNSUPPORTED_REASONING_EFFORT"
	CodeUnsupportedContent    Code = "UNSUPPORTED_CONTENT"
	CodeInternal              Code = "INTERNAL" // recovered panic while processing a response stream
)

// Error is this adapter's error type. Code is stable and safe to branch on;
// Message and Err carry human-readable and wrapped diagnostic detail.
type Error struct {
	Code       Code
	Message    string
	StatusCode int // 0 when not an HTTP response failure
	RetryAfter time.Duration
	RequestID  string
	Err        error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("deepseek: %s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("deepseek: %s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// RetryCode exposes Code as the stable string vocabulary a provider-agnostic
// retry policy (llm/llmretry) classifies against, without that package
// importing this one.
func (e *Error) RetryCode() string { return string(e.Code) }

// ProviderRetryDelay exposes RetryAfter under the name a retry policy's
// classification interface expects — RetryAfter itself is already a struct
// field, so a same-named method isn't legal Go.
func (e *Error) ProviderRetryDelay() time.Duration { return e.RetryAfter }

// httpError classifies a non-2xx response into the stable Code vocabulary.
// body is the (already read) response body, parsed best-effort as a
// wireError; a body that isn't JSON still yields a generic message from the
// status code, matching the TS adapter's "malformed gateway JSON must not
// mask the status" rule.
func httpError(status int, body []byte, retryAfterHeader, requestIDHeader string) *Error {
	var parsed wireError
	_ = json.Unmarshal(body, &parsed)

	message := fmt.Sprintf("DeepSeek API error (HTTP %d)", status)
	var detail string
	if parsed.Error != nil {
		if parsed.Error.Message != "" {
			message = parsed.Error.Message
		}
		detail = strings.ToLower(parsed.Error.Code + " " + parsed.Error.Type + " " + parsed.Error.Message)
	}

	e := &Error{Code: classifyStatus(status, detail), Message: message, StatusCode: status}
	if d := parseRetryAfter(retryAfterHeader); d > 0 {
		e.RetryAfter = d
	}
	if requestIDHeader != "" {
		e.RequestID = requestIDHeader
	}
	return e
}

func classifyStatus(status int, detail string) Code {
	switch {
	case status == 401 || status == 403:
		return CodeAuth
	case strings.Contains(detail, "quota") || strings.Contains(detail, "balance") || strings.Contains(detail, "credit"):
		return CodeQuotaExceeded
	case status == 429:
		return CodeRateLimit
	case status == 400:
		if strings.Contains(detail, "context") && (strings.Contains(detail, "length") || strings.Contains(detail, "exceed") || strings.Contains(detail, "window") || strings.Contains(detail, "too long")) {
			return CodeContextWindowExceeded
		}
		return CodeInvalidRequest
	case status >= 500:
		return CodeServer
	default:
		return Code(fmt.Sprintf("HTTP_%d", status))
	}
}

// parseRetryAfter accepts either a delay in seconds or an HTTP-date, per the
// Retry-After header spec; a value that parses to zero or negative is
// treated as absent (the wire sent something unusable, not "retry now").
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		return 0
	}
	if when, err := time.Parse(time.RFC1123, value); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

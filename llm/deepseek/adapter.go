// Port of adapter.ts: DeepSeekAdapter, a direct HTTP+SSE transport against a
// DeepSeek (OpenAI-compatible) chat-completions endpoint, implementing
// agentloop.LLM. One stable per-call idle watchdog covers the whole
// request+stream; caller cancellation and idle expiry are distinguished in
// the resulting error's Code (ABORTED vs TIMEOUT).
package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"go-harness/core/agentloop"
)

// DefaultBaseURL is the public DeepSeek API endpoint.
const DefaultBaseURL = "https://api.deepseek.com"

// DefaultStreamIdleTimeout bounds each outstanding read while a stream is
// in flight; it does not count time the consumer spends between chunks.
const DefaultStreamIdleTimeout = 5 * time.Minute

// unmarshalChunk is a seam over json.Unmarshal so tests can force pump's
// recover path without needing a real malformed-input edge case that
// happens to panic encoding/json (there isn't a known one — that's the
// point of testing the backstop directly).
var unmarshalChunk = json.Unmarshal

// Config is the adapter's connection facts, resolved once at construction —
// there is no dynamic re-resolution seam here, unlike the harness's
// settings/credentials layer (out of scope for this port; see README).
type Config struct {
	APIKey  string
	BaseURL string // default DefaultBaseURL

	// Thinking locks the deployment to "disabled" when set; empty leaves the
	// provider's own default (thinking enabled) in effect, with ReasoningEffort
	// on each GenerateRequest selecting a level.
	Thinking string

	StreamIdleTimeout time.Duration // default DefaultStreamIdleTimeout
	HTTPClient        *http.Client  // default http.DefaultClient
}

// Adapter is a DeepSeek chat-completions transport. It implements
// agentloop.LLM: one instance serves every model name it's asked to stream
// (the harness model name IS the wire model name).
type Adapter struct {
	cfg Config
}

var _ agentloop.LLM = (*Adapter)(nil)

// New validates cfg and applies defaults for unset optional fields.
func New(cfg Config) (*Adapter, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("deepseek: APIKey is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.StreamIdleTimeout <= 0 {
		cfg.StreamIdleTimeout = DefaultStreamIdleTimeout
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	return &Adapter{cfg: cfg}, nil
}

// Stream implements agentloop.LLM. A non-nil error return means the request
// never reached a usable response (build, transport, or non-2xx failure);
// once the channel is returned, any failure arrives as one final
// StreamChunk{Err: ...} instead, matching how agentloop.step() consumes it.
func (a *Adapter) Stream(ctx context.Context, req agentloop.GenerateRequest) (<-chan agentloop.StreamChunk, error) {
	wireReq, err := serialize(req, a.cfg)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("deepseek: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := a.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &Error{Code: CodeAborted, Message: "DeepSeek request aborted by caller", Err: err}
		}
		return nil, &Error{Code: CodeTransport, Message: fmt.Sprintf("DeepSeek API request to %s failed", a.cfg.BaseURL), Err: err}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		requestID := resp.Header.Get("x-request-id")
		if requestID == "" {
			requestID = resp.Header.Get("x-deepseek-request-id")
		}
		return nil, httpError(resp.StatusCode, body, resp.Header.Get("Retry-After"), requestID)
	}
	if resp.Body == nil {
		return nil, &Error{Code: CodeEmptyResponse, Message: "DeepSeek API returned no response body"}
	}

	out := make(chan agentloop.StreamChunk)
	go a.pump(ctx, resp.Body, out)
	return out, nil
}

// pump owns resp.Body for the lifetime of the stream: it parses SSE,
// accumulates blocks, and emits the finished blocks (or one error) before
// closing out. It always closes both out and body, on every exit path.
//
// pump runs in its own goroutine (Stream's caller never sees it), so a
// recover() here is not decoration: nothing else in the process can catch a
// panic from this stack, and an unrecovered one crashes the whole binary,
// not just this one Stream() call. No caller-supplied code runs on this
// path — unlike agentloop's tool-execution goroutine, which wraps
// arbitrary ToolExecutor.Execute implementations — but this goroutine does
// unmarshal and fold bytes that came from the network, which a future bug
// (here or in a dependency) could mishandle. The recover is registered
// after close(out)/body.Close() so it unwinds *before* them, while out is
// still open to receive the resulting error chunk.
func (a *Adapter) pump(ctx context.Context, body io.ReadCloser, out chan<- agentloop.StreamChunk) {
	defer close(out)
	defer body.Close()
	defer func() {
		if r := recover(); r != nil {
			select {
			case out <- agentloop.StreamChunk{Err: &Error{Code: CodeInternal, Message: fmt.Sprintf("deepseek: panic while processing the response stream: %v", r)}}:
			case <-ctx.Done():
			}
		}
	}()

	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	activity := make(chan struct{}, 1)
	timedOut := make(chan struct{})
	go idleWatchdog(watchCtx, a.cfg.StreamIdleTimeout, activity, timedOut)

	payloads := make(chan string)
	parseErr := make(chan error, 1)
	go func() {
		defer close(payloads)
		parseErr <- parseSSE(watchCtx, body, payloads)
	}()

	send := func(chunk agentloop.StreamChunk) bool {
		select {
		case out <- chunk:
			return true
		case <-ctx.Done():
			return false
		}
	}
	pulse := func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	}

	assembler := newBlockAssembler()
	for {
		select {
		case <-timedOut:
			send(agentloop.StreamChunk{Err: &Error{Code: CodeTimeout, Message: fmt.Sprintf("DeepSeek stream idle timeout after %s", a.cfg.StreamIdleTimeout)}})
			return
		case <-ctx.Done():
			// Caller cancellation: agentloop's own ctx handling already covers
			// this at the turn/step boundary, so no error chunk is needed here.
			return
		case payload, ok := <-payloads:
			if !ok {
				if err := <-parseErr; err != nil && ctx.Err() == nil {
					send(agentloop.StreamChunk{Err: translateStreamErr(err)})
				}
				return
			}
			pulse()
			if payload == doneMarker {
				chunks, err := assembler.finish()
				if err != nil {
					send(agentloop.StreamChunk{Err: err})
					return
				}
				for _, chunk := range chunks {
					if !send(chunk) {
						return
					}
				}
				return
			}
			var wc wireChunk
			if err := unmarshalChunk([]byte(payload), &wc); err != nil {
				send(agentloop.StreamChunk{Err: &Error{Code: CodeMalformedResponse, Message: fmt.Sprintf("malformed SSE payload: %.120s", payload), Err: err}})
				return
			}
			assembler.consume(wc)
		}
	}
}

// idleWatchdog closes timedOut if idle passes without a receive on activity;
// it exits without closing timedOut once ctx is done.
func idleWatchdog(ctx context.Context, idle time.Duration, activity <-chan struct{}, timedOut chan<- struct{}) {
	timer := time.NewTimer(idle)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-activity:
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(idle)
		case <-timer.C:
			close(timedOut)
			return
		}
	}
}

// translateStreamErr classifies a parseSSE failure that isn't caller
// cancellation (already handled by the ctx.Done() branch above it).
func translateStreamErr(err error) *Error {
	if errors.Is(err, errStreamClosed) {
		return &Error{Code: CodeStreamClosed, Message: "DeepSeek SSE stream ended without [DONE]", Err: err}
	}
	return &Error{Code: CodeTransport, Message: "DeepSeek response stream read failed", Err: err}
}

package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"go-harness/core/agentloop"
)

// ---- test helpers ----------------------------------------------------------

// sseServer replies to POST /chat/completions with the given raw SSE data
// lines (each already including "data: " and the trailing blank line is
// added automatically per line), flushing after every write so a consuming
// client actually observes a stream instead of one buffered response.
func sseServer(t *testing.T, lines []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, line := range lines {
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}
	}))
}

func jsonLine(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func collect(t *testing.T, ch <-chan agentloop.StreamChunk) []agentloop.StreamChunk {
	t.Helper()
	var out []agentloop.StreamChunk
	timeout := time.After(5 * time.Second)
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, c)
		case <-timeout:
			t.Fatal("timed out waiting for stream to close")
		}
	}
}

func newTestAdapter(t *testing.T, baseURL string) *Adapter {
	t.Helper()
	a, err := New(Config{APIKey: "test-key", BaseURL: baseURL, StreamIdleTimeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestNewAppliesConnectTimeouts(t *testing.T) {
	a, err := New(Config{
		APIKey:              "k",
		ConnectTimeout:      2 * time.Second,
		TLSHandshakeTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.cfg.HTTPClient == http.DefaultClient {
		t.Fatal("New should not reuse http.DefaultClient when building from timeouts")
	}
	tr, ok := a.cfg.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T", a.cfg.HTTPClient.Transport)
	}
	if tr.TLSHandshakeTimeout != 3*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %s", tr.TLSHandshakeTimeout)
	}
	if a.cfg.HTTPClient.Timeout != 0 {
		t.Fatalf("Client.Timeout = %s, must stay 0 so streams are not cut off", a.cfg.HTTPClient.Timeout)
	}
}

func TestNewKeepsCallerHTTPClient(t *testing.T) {
	c := &http.Client{Timeout: time.Second}
	a, err := New(Config{APIKey: "k", HTTPClient: c, ConnectTimeout: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if a.cfg.HTTPClient != c {
		t.Fatal("New should keep the caller-supplied HTTPClient")
	}
}

func userMsg(text string) agentloop.Message {
	return agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: text}}}
}

// ---- serialize tests --------------------------------------------------------

func TestSerializeSystemAndUserMessage(t *testing.T) {
	req := agentloop.GenerateRequest{
		Model:    "deepseek-v4-flash",
		System:   "be helpful",
		Messages: []agentloop.Message{userMsg("hello")},
	}
	wr, err := serialize(req, Config{})
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	want := []wireMessage{
		{Role: "system", Content: "be helpful"},
		{Role: "user", Content: "hello"},
	}
	if !reflect.DeepEqual(wr.Messages, want) {
		t.Fatalf("messages = %+v, want %+v", wr.Messages, want)
	}
	if wr.Model != "deepseek-v4-flash" || !wr.Stream || !wr.StreamOptions.IncludeUsage {
		t.Fatalf("unexpected request shape: %+v", wr)
	}
}

func TestSerializeAssistantToolCallKeepsReasoningPassback(t *testing.T) {
	msg := agentloop.Message{
		Role: agentloop.RoleAssistant,
		Content: []agentloop.ContentBlock{
			{Kind: agentloop.BlockReasoning, Text: "thinking..."},
			{Kind: agentloop.BlockText, Text: "let me check"},
			{Kind: agentloop.BlockToolCall, CallID: "c1", ToolName: "read", Args: map[string]any{"path": "a.txt"}},
		},
	}
	wm := serializeAssistant(msg)
	if wm.ReasoningContent != "thinking..." {
		t.Fatalf("reasoning_content = %q, want it kept because this turn has a tool call", wm.ReasoningContent)
	}
	if wm.Content != "let me check" {
		t.Fatalf("content = %q, want %q", wm.Content, "let me check")
	}
	if len(wm.ToolCalls) != 1 || wm.ToolCalls[0].ID != "c1" || wm.ToolCalls[0].Function.Name != "read" {
		t.Fatalf("tool_calls = %+v", wm.ToolCalls)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(wm.ToolCalls[0].Function.Arguments), &args); err != nil || args["path"] != "a.txt" {
		t.Fatalf("tool call arguments = %q, want valid JSON with path=a.txt", wm.ToolCalls[0].Function.Arguments)
	}
}

func TestSerializeAssistantDropsReasoningWithoutToolCall(t *testing.T) {
	msg := agentloop.Message{
		Role: agentloop.RoleAssistant,
		Content: []agentloop.ContentBlock{
			{Kind: agentloop.BlockReasoning, Text: "thinking..."},
			{Kind: agentloop.BlockText, Text: "final answer"},
		},
	}
	wm := serializeAssistant(msg)
	if wm.ReasoningContent != "" {
		t.Fatalf("reasoning_content = %q, want dropped on a tool-call-free turn", wm.ReasoningContent)
	}
	if wm.Content != "final answer" {
		t.Fatalf("content = %q, want %q", wm.Content, "final answer")
	}
}

func TestSerializeToolResultBecomesToolMessage(t *testing.T) {
	messages := []agentloop.Message{
		{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockToolResult, CallID: "c1", Text: "file contents"}}},
	}
	wire, err := serializeMessages(messages)
	if err != nil {
		t.Fatalf("serializeMessages: %v", err)
	}
	want := []wireMessage{{Role: "tool", ToolCallID: "c1", Content: "file contents"}}
	if !reflect.DeepEqual(wire, want) {
		t.Fatalf("wire = %+v, want %+v", wire, want)
	}
}

func TestSerializeEmptyToolResultUsesPlaceholder(t *testing.T) {
	messages := []agentloop.Message{
		{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockToolResult, CallID: "c1", Text: ""}}},
	}
	wire, err := serializeMessages(messages)
	if err != nil {
		t.Fatalf("serializeMessages: %v", err)
	}
	if wire[0].Content != "(no output)" {
		t.Fatalf("content = %q, want the (no output) placeholder", wire[0].Content)
	}
}

func TestResolveThinkingRejectsEffortUnderDisabledDeployment(t *testing.T) {
	_, _, err := resolveThinking("high", "disabled")
	var derr *Error
	if err == nil {
		t.Fatal("want an error requesting reasoning effort under a thinking-disabled deployment")
	}
	if !asError(err, &derr) || derr.Code != CodeUnsupportedReasoning {
		t.Fatalf("err = %v, want CodeUnsupportedReasoning", err)
	}
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

// ---- translate tests --------------------------------------------------------

func TestParseArgumentsFallsBackOnInvalidJSON(t *testing.T) {
	if got := parseArguments(""); len(got) != 0 {
		t.Fatalf("empty input = %v, want empty map", got)
	}
	got := parseArguments("{not json")
	if got["_raw"] != "{not json" {
		t.Fatalf("got = %v, want _raw fallback", got)
	}
	got = parseArguments(`{"path":"a.txt"}`)
	if got["path"] != "a.txt" {
		t.Fatalf("got = %v, want parsed path", got)
	}
}

func TestMapUsageSubtractsCacheHitsFromInput(t *testing.T) {
	u := wireUsage{PromptTokens: 100, CompletionTokens: 20}
	u.PromptTokensDetails.CachedTokens = 30
	input, output, cacheRead, reasoning := mapUsage(u)
	if input != 70 || output != 20 || cacheRead != 30 || reasoning != 0 {
		t.Fatalf("mapUsage = (%d,%d,%d,%d), want (70,20,30,0)", input, output, cacheRead, reasoning)
	}
}

// ---- end-to-end stream tests -----------------------------------------------

func TestStreamTextResponse(t *testing.T) {
	srv := sseServer(t, []string{
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "hi "}}}}),
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "there"}, FinishReason: "stop"}}}),
		jsonLine(t, wireChunk{Usage: &wireUsage{PromptTokens: 10, CompletionTokens: 2}}),
		doneMarker,
	})
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	ch, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("hi")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ch)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 live text deltas", len(chunks))
	}
	if chunks[0].Err != nil || chunks[1].Err != nil {
		t.Fatalf("chunk error: %v / %v", chunks[0].Err, chunks[1].Err)
	}
	if chunks[0].Delta.Kind != agentloop.BlockText || chunks[0].Delta.Text != "hi " {
		t.Fatalf("delta 0 = %+v, want text %q", chunks[0].Delta, "hi ")
	}
	if chunks[1].Delta.Kind != agentloop.BlockText || chunks[1].Delta.Text != "there" {
		t.Fatalf("delta 1 = %+v, want text %q", chunks[1].Delta, "there")
	}
	if chunks[0].MaxTokens || chunks[1].MaxTokens {
		t.Fatal("MaxTokens = true, want false for a stop finish")
	}
}

func TestStreamToolCallResponseAssemblesFragments(t *testing.T) {
	srv := sseServer(t, []string{
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
			{Index: 0, ID: "c1", Function: &wireToolFunctionDelta{Name: "read", Arguments: `{"pa`}},
		}}}}}),
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
			{Index: 0, Function: &wireToolFunctionDelta{Arguments: `th":"a.txt"}`}},
		}}, FinishReason: "tool_calls"}}}),
		doneMarker,
	})
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	ch, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("read the file")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ch)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	d := chunks[0].Delta
	if d.Kind != agentloop.BlockToolCall || d.CallID != "c1" || d.ToolName != "read" {
		t.Fatalf("delta = %+v", d)
	}
	if d.Args["path"] != "a.txt" {
		t.Fatalf("args = %v, want path=a.txt assembled from two fragments", d.Args)
	}
}

func TestStreamReasoningThenToolCallOrder(t *testing.T) {
	srv := sseServer(t, []string{
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{ReasoningContent: ""}}}}), // documented empty-first-chunk quirk
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{ReasoningContent: "let me think"}}}}),
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
			{Index: 0, ID: "c1", Function: &wireToolFunctionDelta{Name: "read", Arguments: "{}"}},
		}}, FinishReason: "tool_calls"}}}),
		doneMarker,
	})
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	ch, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("go")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ch)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 (reasoning, then tool-call)", len(chunks))
	}
	if chunks[0].Delta.Kind != agentloop.BlockReasoning || chunks[0].Delta.Text != "let me think" {
		t.Fatalf("chunk 0 = %+v, want the reasoning block first (empty leading delta must not open its own block)", chunks[0].Delta)
	}
	if chunks[1].Delta.Kind != agentloop.BlockToolCall {
		t.Fatalf("chunk 1 = %+v, want the tool call second", chunks[1].Delta)
	}
}

func TestStreamMaxTokensFinishMarksLastChunk(t *testing.T) {
	srv := sseServer(t, []string{
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "cut off"}, FinishReason: "length"}}}),
		doneMarker,
	})
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	ch, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("go")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ch)
	var sawMax bool
	var text string
	for _, c := range chunks {
		if c.Err != nil {
			t.Fatalf("unexpected error chunk: %v", c.Err)
		}
		text += c.Delta.Text
		if c.MaxTokens {
			sawMax = true
		}
	}
	if text != "cut off" || !sawMax {
		t.Fatalf("chunks = %+v, want streamed text %q with MaxTokens on some chunk", chunks, "cut off")
	}
}

func TestStreamEmptyResponseIsError(t *testing.T) {
	srv := sseServer(t, []string{
		jsonLine(t, wireChunk{Choices: []wireChoice{{FinishReason: "stop"}}}),
		doneMarker,
	})
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	ch, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("go")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ch)
	if len(chunks) != 1 || chunks[0].Err == nil {
		t.Fatalf("chunks = %+v, want exactly 1 error chunk", chunks)
	}
	var derr *Error
	if !asError(chunks[0].Err, &derr) || derr.Code != CodeEmptyResponse {
		t.Fatalf("err = %v, want CodeEmptyResponse", chunks[0].Err)
	}
}

func TestStreamUnknownFinishReasonIsError(t *testing.T) {
	srv := sseServer(t, []string{
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "hmm"}, FinishReason: "content_filter"}}}),
		doneMarker,
	})
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	ch, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("go")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ch)
	if len(chunks) == 0 || chunks[len(chunks)-1].Err == nil {
		t.Fatalf("chunks = %+v, want a trailing error chunk", chunks)
	}
	var derr *Error
	if !asError(chunks[len(chunks)-1].Err, &derr) || derr.Code != CodeModelStopped {
		t.Fatalf("err = %v, want CodeModelStopped", chunks[len(chunks)-1].Err)
	}
}

func TestStreamMalformedPayloadIsError(t *testing.T) {
	srv := sseServer(t, []string{"{not valid json", doneMarker})
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	ch, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("go")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ch)
	if len(chunks) != 1 || chunks[0].Err == nil {
		t.Fatalf("chunks = %+v, want exactly 1 error chunk", chunks)
	}
	var derr *Error
	if !asError(chunks[0].Err, &derr) || derr.Code != CodeMalformedResponse {
		t.Fatalf("err = %v, want CodeMalformedResponse", chunks[0].Err)
	}
}

func TestStreamEndsWithoutDoneIsError(t *testing.T) {
	srv := sseServer(t, []string{
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "partial"}}}}),
		// server closes the connection without ever sending [DONE]
	})
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	ch, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("go")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ch)
	if len(chunks) == 0 || chunks[len(chunks)-1].Err == nil {
		t.Fatalf("chunks = %+v, want a trailing error chunk", chunks)
	}
	var derr *Error
	if !asError(chunks[len(chunks)-1].Err, &derr) || derr.Code != CodeStreamClosed {
		t.Fatalf("err = %v, want CodeStreamClosed", chunks[len(chunks)-1].Err)
	}
}

func TestStreamHTTPErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   Code
	}{
		{http.StatusUnauthorized, `{"error":{"message":"bad key"}}`, CodeAuth},
		{http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, CodeRateLimit},
		{http.StatusInternalServerError, `{"error":{"message":"oops"}}`, CodeServer},
		{http.StatusBadRequest, `{"error":{"message":"invalid field"}}`, CodeInvalidRequest},
		{http.StatusBadRequest, `{"error":{"message":"maximum context length exceeded"}}`, CodeContextWindowExceeded},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d_%s", tc.status, tc.want), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			a := newTestAdapter(t, srv.URL)
			_, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("go")}})
			var derr *Error
			if !asError(err, &derr) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if derr.Code != tc.want {
				t.Fatalf("code = %q, want %q", derr.Code, tc.want)
			}
			if derr.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", derr.StatusCode, tc.status)
			}
		})
	}
}

func TestStreamContextCancellationStopsPromptly(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "partial"}}}}))
		flusher.Flush()
		<-block // hang until the test releases it, simulating a stalled upstream
	}))
	defer srv.Close()
	defer close(block)

	a := newTestAdapter(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := a.Stream(ctx, agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("go")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	cancel()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // closed promptly, any live delta before cancel is fine
			}
		case <-deadline:
			t.Fatal("Stream did not stop promptly after context cancellation")
		}
	}
}

func TestStreamIdleTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-block // never send another byte — the idle watchdog must fire
	}))
	defer srv.Close()
	defer close(block)

	a, err := New(Config{APIKey: "k", BaseURL: srv.URL, StreamIdleTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("go")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ch)
	if len(chunks) != 1 || chunks[0].Err == nil {
		t.Fatalf("chunks = %+v, want exactly 1 timeout error chunk", chunks)
	}
	var derr *Error
	if !asError(chunks[0].Err, &derr) || derr.Code != CodeTimeout {
		t.Fatalf("err = %v, want CodeTimeout", chunks[0].Err)
	}
}

func TestStreamPanicDuringProcessingIsRecoveredAsErrorChunk(t *testing.T) {
	original := unmarshalChunk
	unmarshalChunk = func([]byte, any) error { panic("simulated bug processing a response chunk") }
	defer func() { unmarshalChunk = original }()

	srv := sseServer(t, []string{
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "hi"}}}}),
		doneMarker,
	})
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	ch, err := a.Stream(context.Background(), agentloop.GenerateRequest{Model: "deepseek-v4-flash", Messages: []agentloop.Message{userMsg("go")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ch)
	if len(chunks) != 1 || chunks[0].Err == nil {
		t.Fatalf("chunks = %+v, want exactly 1 recovered error chunk instead of a crash", chunks)
	}
	var derr *Error
	if !asError(chunks[0].Err, &derr) || derr.Code != CodeInternal {
		t.Fatalf("err = %v, want CodeInternal", chunks[0].Err)
	}
}

// ---- integration: plugged into a real agentloop.Agent ----------------------

type noopTools struct{}

func (noopTools) ExecutionMode(agentloop.ContentBlock) agentloop.ExecutionMode {
	return agentloop.ModeExclusive
}
func (noopTools) Execute(context.Context, *agentloop.Agent, agentloop.ContentBlock) agentloop.ToolResult {
	return agentloop.ToolResult{}
}
func (noopTools) Schemas() []agentloop.ToolSchema { return nil }

func TestAdapterDrivesARealAgent(t *testing.T) {
	srv := sseServer(t, []string{
		jsonLine(t, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "the answer is 4"}, FinishReason: "stop"}}}),
		doneMarker,
	})
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	session := agentloop.NewMemorySession()
	agent := agentloop.NewAgent(session, a, noopTools{}, "you are terse", agentloop.Hooks{}).WithModel("deepseek-v4-flash")

	agent.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: "what is 2+2"}}})
	agent.WhenIdle()

	if got := agent.Status(); got != "idle" {
		t.Fatalf("status = %q, want idle", got)
	}
	msgs := session.DeriveMessages()
	if len(msgs) != 2 {
		t.Fatalf("derived %d messages, want 2 (user + assistant)", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != agentloop.RoleAssistant || last.Content[0].Text != "the answer is 4" {
		t.Fatalf("last message = %+v, want the assistant's answer", last)
	}
}

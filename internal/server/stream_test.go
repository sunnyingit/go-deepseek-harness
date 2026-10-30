package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-harness/core/agentloop"
	"go-harness/core/session"
)

func TestOpenStepStart(t *testing.T) {
	events := []session.Event{
		{Type: "turn/start", Seq: 0},
		{Type: "step/start", Seq: 1},
		{Type: "user/message", Seq: 2},
		{Type: "step/end", Seq: 3},
		{Type: "step/start", Seq: 4},
		{Type: "assistant/chunk", Seq: 5},
	}
	if got := openStepStart(events); got != 4 {
		t.Fatalf("openStepStart = %d, want 4 (the second, still-open step)", got)
	}
	if got := openStepStart(events[:4]); got != -1 {
		t.Fatalf("openStepStart = %d, want -1 (the only step is already closed)", got)
	}
	if got := openStepStart(nil); got != -1 {
		t.Fatalf("openStepStart(nil) = %d, want -1", got)
	}
}

// blockingChunkLLM streams one chunk immediately, then blocks until told to
// release before streaming a second — letting a test pause a turn
// mid-generation to simulate a client reconnecting to it.
type blockingChunkLLM struct {
	release chan struct{}
}

func (b *blockingChunkLLM) Stream(ctx context.Context, _ agentloop.GenerateRequest) (<-chan agentloop.StreamChunk, error) {
	ch := make(chan agentloop.StreamChunk)
	go func() {
		defer close(ch)
		select {
		case ch <- agentloop.StreamChunk{Delta: agentloop.ContentBlock{Kind: agentloop.BlockText, Text: "partial-before-reconnect "}}:
		case <-ctx.Done():
			return
		}
		select {
		case <-b.release:
		case <-ctx.Done():
			return
		}
		select {
		case ch <- agentloop.StreamChunk{Delta: agentloop.ContentBlock{Kind: agentloop.BlockText, Text: "rest-after-reconnect"}}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func startSSELines(r io.Reader) <-chan string {
	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	return lines
}

func waitForLineContaining(t *testing.T, lines <-chan string, want string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return false
			}
			if strings.Contains(line, want) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// TestStreamSurvivesClientDisconnectAndReconnects is the regression test
// for the bug this fix addresses: a client (the dailog.html page)
// navigating away mid-turn used to look like the whole conversation broke,
// when in fact the turn was still running server-side the entire time —
// there was just no way to reconnect and see the rest of it. This drives
// the real HTTP handlers end-to-end: start a turn, sever the connection
// mid-generation, confirm the turn is still "running" (not cancelled by
// the disconnect), then reconnect with no text (watch-only) and confirm
// it replays the in-progress step's own prefix and eventually reports done.
func TestStreamSurvivesClientDisconnectAndReconnects(t *testing.T) {
	s, _ := newTestServer(t)

	sess := session.New("", session.Header{})
	release := make(chan struct{})
	llm := &blockingChunkLLM{release: release}
	agent := agentloop.NewAgent(sess, llm, fakeTools{}, "sys", agentloop.Hooks{})

	const sessionID = "stream-sess"
	if _, err := s.agents.Register(sessionID, agent, ""); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	ctx1, cancel1 := context.WithCancel(context.Background())
	req1, err := http.NewRequestWithContext(ctx1, http.MethodGet, srv.URL+"/sessions/"+sessionID+"/stream?text=hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	res1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	lines1 := startSSELines(res1.Body)
	if !waitForLineContaining(t, lines1, "partial-before-reconnect", 5*time.Second) {
		t.Fatal("first connection never saw the first chunk")
	}

	// Simulate a page navigation away: sever this connection entirely.
	cancel1()
	res1.Body.Close()

	deadline := time.Now().Add(5 * time.Second)
	for agent.Status() != "running" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if agent.Status() != "running" {
		t.Fatalf("agent.Status() = %q shortly after client disconnect, want running — the turn must not be cancelled by a client disconnect", agent.Status())
	}

	// Reconnect: no text means "just watch."
	res2, err := http.Get(srv.URL + "/sessions/" + sessionID + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	lines2 := startSSELines(res2.Body)

	if !waitForLineContaining(t, lines2, "partial-before-reconnect", 5*time.Second) {
		t.Fatal("reconnected stream did not replay the in-progress step's own prefix")
	}

	close(release) // let the turn actually finish now

	if !waitForLineContaining(t, lines2, "event: done", 5*time.Second) {
		t.Fatal("reconnected stream never reported done")
	}

	if agent.Status() != "idle" {
		t.Fatalf("agent.Status() = %q after done, want idle", agent.Status())
	}
}

// TestStreamReconnectReplaysSameStepBoundary is the regression test for the
// reconnect-duplication bug: the frontend keeps painting into the same chat
// bubble across a disconnect/reconnect (see handleStreamSession's own doc
// comment on why), so without a boundary marker it has no way to tell "the
// server is replaying a step already partially rendered" apart from "a new
// step just started" — it would just append the replay on top of what it
// already drew. The fix forwards step/start as a lightweight "step" SSE
// event carrying (turn, step); the client uses a repeated (turn, step) as
// the signal to roll its rendering back before repainting. This test only
// asserts the wire contract that fix depends on: the same (turn, step)
// value must appear again, verbatim, when a stream reconnects mid-step.
func TestStreamReconnectReplaysSameStepBoundary(t *testing.T) {
	s, _ := newTestServer(t)

	sess := session.New("", session.Header{})
	release := make(chan struct{})
	llm := &blockingChunkLLM{release: release}
	agent := agentloop.NewAgent(sess, llm, fakeTools{}, "sys", agentloop.Hooks{})

	const sessionID = "step-boundary-sess"
	if _, err := s.agents.Register(sessionID, agent, ""); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	ctx1, cancel1 := context.WithCancel(context.Background())
	req1, err := http.NewRequestWithContext(ctx1, http.MethodGet, srv.URL+"/sessions/"+sessionID+"/stream?text=hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	res1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	defer res1.Body.Close()
	lines1 := startSSELines(res1.Body)
	// json.Marshal on a map[string]any sorts keys alphabetically, so "step"
	// (before "turn") is the actual wire order — see writeStreamEvent.
	if !waitForLineContaining(t, lines1, `"step":1,"turn":1`, 5*time.Second) {
		t.Fatal("first connection never saw the step boundary event")
	}
	if !waitForLineContaining(t, lines1, "partial-before-reconnect", 5*time.Second) {
		t.Fatal("first connection never saw the first chunk")
	}

	cancel1()
	res1.Body.Close()

	deadline := time.Now().Add(5 * time.Second)
	for agent.Status() != "running" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	res2, err := http.Get(srv.URL + "/sessions/" + sessionID + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	lines2 := startSSELines(res2.Body)

	// The reconnect must replay the *same* (turn, step) the first connection
	// already saw — that repetition is exactly what the client keys off of
	// to know it must roll back before repainting.
	if !waitForLineContaining(t, lines2, `"step":1,"turn":1`, 5*time.Second) {
		t.Fatal("reconnected stream did not replay the same step boundary")
	}

	close(release)
	if !waitForLineContaining(t, lines2, "event: done", 5*time.Second) {
		t.Fatal("reconnected stream never reported done")
	}
}

func TestStreamWatchOnlyOnAlreadyIdleSessionIsHarmless(t *testing.T) {
	s, _ := newTestServer(t)

	sess := session.New("", session.Header{})
	agent := agentloop.NewAgent(sess, fakeLLM{}, fakeTools{}, "sys", agentloop.Hooks{})
	const sessionID = "idle-sess"
	if _, err := s.agents.Register(sessionID, agent, ""); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	lines := startSSELines(res.Body)
	if !waitForLineContaining(t, lines, "event: done", 5*time.Second) {
		t.Fatal("a watch-only connection to an idle session should immediately report done")
	}
}

// failingLLM always fails synchronously, simulating an exhausted-retries
// model request (e.g. llm/deepseek giving up after llmretry declines to
// retry further).
type failingLLM struct{ message string }

func (f failingLLM) Stream(context.Context, agentloop.GenerateRequest) (<-chan agentloop.StreamChunk, error) {
	return nil, errors.New(f.message)
}

// TestStreamDoneReportsFailedTurn is the regression test for the gap where
// a turn ending in "error" looked identical to a normal completion over
// SSE — the client only ever saw a bare "done" and would report "已完成"
// even though nothing was actually generated. The done payload must now
// carry failed:true and the real error text, taken from turn/end's own
// durable record (see agent.go's TestTurnEndCarriesTheActualErrorText).
func TestStreamDoneReportsFailedTurn(t *testing.T) {
	s, _ := newTestServer(t)

	sess := session.New("", session.Header{})
	agent := agentloop.NewAgent(sess, failingLLM{message: "simulated model outage"}, fakeTools{}, "sys", agentloop.Hooks{})
	const sessionID = "failing-sess"
	if _, err := s.agents.Register(sessionID, agent, ""); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/stream?text=hello")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	lines := startSSELines(res.Body)

	var doneData string
	deadline := time.After(5 * time.Second)
	for doneData == "" {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("stream closed before a done event carrying failed:true arrived")
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, "failed") {
				doneData = strings.TrimPrefix(line, "data: ")
			}
		case <-deadline:
			t.Fatal("timed out waiting for a failed done event")
		}
	}

	var payload struct {
		Failed       bool   `json:"failed"`
		ErrorMessage string `json:"errorMessage"`
	}
	if err := json.Unmarshal([]byte(doneData), &payload); err != nil {
		t.Fatalf("decoding done payload %q: %v", doneData, err)
	}
	if !payload.Failed {
		t.Fatalf("done payload = %q, want failed:true", doneData)
	}
	if !strings.Contains(payload.ErrorMessage, "simulated model outage") {
		t.Fatalf("errorMessage = %q, want it to contain the underlying failure", payload.ErrorMessage)
	}
}

// maxTokensLLM finishes a stream with the length marker — the same signal
// DeepSeek sends when a single completion hits its output cap. The turn
// then ends as reason "max-tokens" without being an "error".
type maxTokensLLM struct{}

func (maxTokensLLM) Stream(context.Context, agentloop.GenerateRequest) (<-chan agentloop.StreamChunk, error) {
	ch := make(chan agentloop.StreamChunk, 1)
	ch <- agentloop.StreamChunk{
		Delta:     agentloop.ContentBlock{Kind: agentloop.BlockText, Text: "cut off"},
		MaxTokens: true,
	}
	close(ch)
	return ch, nil
}

func TestStreamDoneReportsMaxTokensTurn(t *testing.T) {
	s, _ := newTestServer(t)

	sess := session.New("", session.Header{})
	agent := agentloop.NewAgent(sess, maxTokensLLM{}, fakeTools{}, "sys", agentloop.Hooks{})
	const sessionID = "max-tokens-sess"
	if _, err := s.agents.Register(sessionID, agent, ""); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/stream?text=hello")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	lines := startSSELines(res.Body)

	var doneData string
	deadline := time.After(5 * time.Second)
	for doneData == "" {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("stream closed before a done event arrived")
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, "max-tokens") {
				doneData = strings.TrimPrefix(line, "data: ")
			}
		case <-deadline:
			t.Fatal("timed out waiting for a max-tokens done event")
		}
	}

	var payload struct {
		Failed       bool   `json:"failed"`
		Reason       string `json:"reason"`
		ErrorMessage string `json:"errorMessage"`
	}
	if err := json.Unmarshal([]byte(doneData), &payload); err != nil {
		t.Fatalf("decoding done payload %q: %v", doneData, err)
	}
	if !payload.Failed || payload.Reason != "max-tokens" {
		t.Fatalf("done payload = %q, want failed:true reason=max-tokens", doneData)
	}
	if payload.ErrorMessage == "" {
		t.Fatalf("done payload = %q, want a non-empty errorMessage", doneData)
	}
}

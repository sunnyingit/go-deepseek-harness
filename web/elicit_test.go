package main

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-harness/core/agentloop"
	coretools "go-harness/core/tools"

	"context"

	"go-harness/core/session"
)

// twoStepLLM streams a tool call to ask_user_question on its first Stream
// call, then plain text quoting whatever tool-result text landed for call-1
// on its second — letting a test observe exactly what Execute received back
// from the answer endpoint, not just that *something* unblocked it.
type twoStepLLM struct{ calls int }

func (l *twoStepLLM) Stream(_ context.Context, req agentloop.GenerateRequest) (<-chan agentloop.StreamChunk, error) {
	l.calls++
	ch := make(chan agentloop.StreamChunk, 1)
	if l.calls == 1 {
		ch <- agentloop.StreamChunk{Delta: agentloop.ContentBlock{
			Kind: agentloop.BlockToolCall, CallID: "call-1", ToolName: askUserQuestionTool,
			Args: map[string]any{
				"question": "which one?",
				"options":  []any{map[string]any{"label": "选项A"}, map[string]any{"label": "选项B"}},
			},
		}}
	} else {
		got := "(no tool result found)"
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Kind == agentloop.BlockToolResult && b.CallID == "call-1" {
					got = b.Text
				}
			}
		}
		ch <- agentloop.StreamChunk{Delta: agentloop.ContentBlock{Kind: agentloop.BlockText, Text: "got: " + got}}
	}
	close(ch)
	return ch, nil
}

// TestAskUserQuestionEndToEnd drives the whole ask_user_question path
// through real HTTP: a model tool call blocks on Execute, the SSE stream
// must surface an "elicit" event (not a generic tool-call/tool-result) the
// instant the call is made — not after it resolves, since resolving is
// exactly what's waiting on the user — and POST /sessions/{id}/answer must
// be what unblocks it, delivering the posted text back to the model as this
// call's tool result.
func TestAskUserQuestionEndToEnd(t *testing.T) {
	s, _ := newTestServer(t)

	sess := session.New("", session.Header{})
	const sessionID = "elicit-sess"

	registry := coretools.NewRegistry()
	if _, err := registry.Register(newAskUserQuestionTool(s.answers, sessionID)); err != nil {
		t.Fatal(err)
	}
	llm := &twoStepLLM{}
	agent := agentloop.NewAgent(sess, llm, coretools.Bridge{Registry: registry}, "sys", agentloop.Hooks{})
	if _, err := s.agents.Register(sessionID, agent, ""); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/stream?text=hello")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var lines []string
	var sawElicit, sawChunk, sawDone bool
	answered := false
	deadline := time.Now().Add(5 * time.Second)

	for scanner.Scan() && time.Now().Before(deadline) {
		line := scanner.Text()
		lines = append(lines, line)

		if strings.Contains(line, "event: elicit") {
			sawElicit = true
		}
		// The instant the question is visible, answer it — proves the card
		// went out at tool/call time, before Execute could possibly have
		// resolved on its own (nothing else would ever unblock it).
		if sawElicit && !answered && strings.Contains(line, `"question":"which one?"`) {
			answered = true
			body := strings.NewReader(`{"callId":"call-1","text":"选项A"}`)
			answerRes, err := http.Post(srv.URL+"/sessions/"+sessionID+"/answer", "application/json", body)
			if err != nil {
				t.Fatal(err)
			}
			if answerRes.StatusCode != http.StatusOK {
				t.Fatalf("POST /answer = %d", answerRes.StatusCode)
			}
			answerRes.Body.Close()
		}
		if strings.Contains(line, "got: 选项A") {
			sawChunk = true
		}
		if strings.Contains(line, "event: done") {
			sawDone = true
			break
		}
	}

	if !sawElicit {
		t.Fatalf("never saw an elicit SSE event; lines:\n%s", strings.Join(lines, "\n"))
	}
	if !sawChunk {
		t.Fatalf("model's follow-up text never echoed the posted answer; lines:\n%s", strings.Join(lines, "\n"))
	}
	if !sawDone {
		t.Fatalf("stream never reported done; lines:\n%s", strings.Join(lines, "\n"))
	}
	for _, line := range lines {
		if strings.Contains(line, "event: tool-call") || strings.Contains(line, "event: tool-result") {
			t.Fatalf("ask_user_question must never surface as a generic tool-call/tool-result event, got: %q", line)
		}
	}

	// A second answer for the same (already-consumed) callId must be
	// rejected — nothing should still be waiting on it.
	body := strings.NewReader(`{"callId":"call-1","text":"选项B"}`)
	res2, err := http.Post(srv.URL+"/sessions/"+sessionID+"/answer", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusNotFound {
		t.Fatalf("second answer to the same callId = %d, want 404", res2.StatusCode)
	}
}

// TestAskUserQuestionUnknownCallIDIs404 guards the HTTP layer itself,
// independent of any agent/turn machinery.
func TestAskUserQuestionUnknownCallIDIs404(t *testing.T) {
	s, sessionID := newTestServer(t)
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	body := strings.NewReader(`{"callId":"never-asked","text":"x"}`)
	res, err := http.Post(srv.URL+"/sessions/"+sessionID+"/answer", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
}

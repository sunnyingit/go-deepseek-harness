package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-harness/core/agentloop"
	"go-harness/core/session"
)

// oneShotLLM streams a single fixed reply then closes — enough to drive one
// complete turn without any external dependency.
type oneShotLLM struct{ reply string }

func (o oneShotLLM) Stream(_ context.Context, _ agentloop.GenerateRequest) (<-chan agentloop.StreamChunk, error) {
	ch := make(chan agentloop.StreamChunk, 1)
	ch <- agentloop.StreamChunk{Delta: agentloop.ContentBlock{Kind: agentloop.BlockText, Text: o.reply}}
	close(ch)
	return ch, nil
}

func TestPostMessageReportsTurnTokenUsage(t *testing.T) {
	s, _ := newTestServer(t)

	sess := session.New("", session.Header{})
	agent := agentloop.NewAgent(sess, oneShotLLM{reply: "hi there, general kenobi"}, fakeTools{}, "You are a helpful assistant.", agentloop.Hooks{})
	const sessionID = "usage-sess"
	if _, err := s.agents.Register(sessionID, agent, ""); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	res, err := http.Post(srv.URL+"/sessions/"+sessionID+"/messages", "application/json",
		strings.NewReader(`{"text":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}

	var body struct {
		Usage *struct {
			TurnTokens  int `json:"turnTokens"`
			TotalTokens int `json:"totalTokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Usage == nil {
		t.Fatal("response has no usage field")
	}
	if body.Usage.TurnTokens <= 0 {
		t.Errorf("turnTokens = %d, want > 0 (a user message + assistant reply were just added)", body.Usage.TurnTokens)
	}
	if body.Usage.TotalTokens < body.Usage.TurnTokens {
		t.Errorf("totalTokens = %d, want >= turnTokens (%d)", body.Usage.TotalTokens, body.Usage.TurnTokens)
	}
}

func TestStreamDoneReportsTurnTokenUsage(t *testing.T) {
	s, _ := newTestServer(t)

	sess := session.New("", session.Header{})
	agent := agentloop.NewAgent(sess, oneShotLLM{reply: "hi there"}, fakeTools{}, "sys", agentloop.Hooks{})
	const sessionID = "usage-stream-sess"
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
				t.Fatal("stream closed before a done event with data arrived")
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, "turnTokens") {
				doneData = strings.TrimPrefix(line, "data: ")
			}
		case <-deadline:
			t.Fatal("timed out waiting for a done event carrying usage")
		}
	}

	var usage struct {
		Usage struct {
			TurnTokens  int `json:"turnTokens"`
			TotalTokens int `json:"totalTokens"`
		} `json:"usage"`
		DurationMs int64 `json:"durationMs"`
	}
	if err := json.Unmarshal([]byte(doneData), &usage); err != nil {
		t.Fatalf("decoding done payload %q: %v", doneData, err)
	}
	if usage.Usage.TurnTokens <= 0 {
		t.Errorf("turnTokens = %d, want > 0", usage.Usage.TurnTokens)
	}
	if usage.DurationMs <= 0 {
		t.Errorf("durationMs = %d, want > 0 (turn/start to turn/end, or connection elapsed)", usage.DurationMs)
	}
}

func TestLastTurnDurationMs(t *testing.T) {
	if got := lastTurnDurationMs(nil); got != 0 {
		t.Fatalf("empty log = %d, want 0", got)
	}
	events := []session.Event{
		{Type: "turn/start", Time: 1000},
		{Type: "assistant/chunk", Time: 1100},
		{Type: "turn/end", Time: 2500},
	}
	if got := lastTurnDurationMs(events); got != 1500 {
		t.Fatalf("duration = %d, want 1500", got)
	}
	events = append(events,
		session.Event{Type: "turn/start", Time: 3000},
		session.Event{Type: "turn/end", Time: 3200},
	)
	if got := lastTurnDurationMs(events); got != 200 {
		t.Fatalf("latest turn duration = %d, want 200", got)
	}
}

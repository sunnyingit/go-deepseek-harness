package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-harness/core/agentloop"
	"go-harness/core/session"
)

// fakeLLM/fakeTools exist only so a real *agentloop.Agent can be
// constructed for handleGetTokens to introspect (SystemPrompt/ToolSchemas/
// Session) — the token endpoint never triggers a turn, so neither is ever
// actually invoked.
type fakeLLM struct{}

func (fakeLLM) Stream(_ context.Context, _ agentloop.GenerateRequest) (<-chan agentloop.StreamChunk, error) {
	panic("fakeLLM.Stream should never be called by the tokens endpoint")
}

type fakeTools struct{ schemas []agentloop.ToolSchema }

func (fakeTools) ExecutionMode(agentloop.ContentBlock) agentloop.ExecutionMode {
	return agentloop.ModeExclusive
}
func (fakeTools) Execute(context.Context, *agentloop.Agent, agentloop.ContentBlock) agentloop.ToolResult {
	panic("fakeTools.Execute should never be called by the tokens endpoint")
}
func (f fakeTools) Schemas() []agentloop.ToolSchema { return f.schemas }

func TestGetTokensEndToEnd(t *testing.T) {
	s, _ := newTestServer(t)

	sess := session.New("", session.Header{})
	sess.Append("user/message",
		session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{Kind: session.BlockText, Text: "hello there, this is a test message"}}},
		session.WithSurfaceOp(session.AppendOp()))

	tools := fakeTools{schemas: []agentloop.ToolSchema{{Name: "read", Description: "read a file"}}}
	agent := agentloop.NewAgent(sess, fakeLLM{}, tools, "You are a helpful assistant.", agentloop.Hooks{})

	const sessionID = "tokens-sess"
	if _, err := s.agents.Register(sessionID, agent, ""); err != nil {
		t.Fatalf("registering agent: %v", err)
	}

	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/tokens")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}

	var body struct {
		LogRevision   int `json:"logRevision"`
		HeaderTokens  int `json:"headerTokens"`
		SurfaceTokens int `json:"surfaceTokens"`
		TotalTokens   int `json:"totalTokens"`
		Nodes         []struct {
			Seq    int `json:"seq"`
			Tokens int `json:"tokens"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if body.LogRevision != 1 {
		t.Errorf("logRevision = %d, want 1", body.LogRevision)
	}
	if body.HeaderTokens <= 0 {
		t.Errorf("headerTokens = %d, want > 0 (non-empty system prompt + tools)", body.HeaderTokens)
	}
	if body.SurfaceTokens <= 0 {
		t.Errorf("surfaceTokens = %d, want > 0 (one user message)", body.SurfaceTokens)
	}
	if body.TotalTokens != body.HeaderTokens+body.SurfaceTokens {
		t.Errorf("totalTokens = %d, want headerTokens+surfaceTokens = %d", body.TotalTokens, body.HeaderTokens+body.SurfaceTokens)
	}
	if len(body.Nodes) != 1 {
		t.Errorf("nodes = %+v, want exactly one (the user message)", body.Nodes)
	}
}

func TestGetTokensUnknownSession404s(t *testing.T) {
	s, _ := newTestServer(t)
	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/sessions/does-not-exist/tokens")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

// ask_user_question is a pause-and-wait pseudo-tool: unlike every other tool
// in this package (display.go's show_* included), its Execute genuinely
// blocks — there is no synthetic ack — until either the browser POSTs an
// answer to /sessions/{id}/answer, or the turn's own context is cancelled
// (session cancel, process shutdown). This is the harness's answer to "the
// model isn't sure what the user wants; ask instead of guessing."
//
// Deliberately simplified for a first version: a client that disconnects
// while a question is pending and reconnects will not see the question
// replayed (writeStreamEvent only emits "elicit" once, at tool/call time —
// see handlers.go). The model is still just blocked waiting; a page reload
// can always answer it by re-opening the same watch-only stream and using
// the answer endpoint once the callId is known some other way, but nothing
// here reconstructs that callId for a fresh page load. Hardening that is
// future work, not this pass.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"go-harness/core/session"
	coretools "go-harness/core/tools"
)

// askUserQuestionTool is the model-facing tool name — referenced by
// writeStreamEvent (to special-case its tool/call and tool/result framing)
// and by the system prompt section describing it.
const askUserQuestionTool = "ask_user_question"

// answerRegistry hands out and resolves the channels askUserQuestion's
// Execute blocks on. Keyed by (sessionID, callID) so two different
// sessions can never collide even if a provider ever reused a call id
// (session IDs are this process's own, so that half of the key is trusted;
// callID is model/provider-supplied and only trusted for correlation, never
// as a capability token, since it's already visible in the client's own SSE
// stream anyway).
type answerRegistry struct {
	mu      sync.Mutex
	pending map[[2]string]chan string
}

func newAnswerRegistry() *answerRegistry {
	return &answerRegistry{pending: map[[2]string]chan string{}}
}

// register opens a slot for (sessionID, callID) and returns the channel
// Execute should wait on. The channel is buffered by one so a concurrent
// answer() never blocks even if Execute has already moved on (e.g. the
// turn was cancelled a moment earlier).
func (r *answerRegistry) register(sessionID, callID string) <-chan string {
	ch := make(chan string, 1)
	r.mu.Lock()
	r.pending[[2]string{sessionID, callID}] = ch
	r.mu.Unlock()
	return ch
}

func (r *answerRegistry) unregister(sessionID, callID string) {
	r.mu.Lock()
	delete(r.pending, [2]string{sessionID, callID})
	r.mu.Unlock()
}

// answer delivers text to the pending call, if any. ok is false when
// there's no such pending question — already answered, never asked, or a
// stale callId from an earlier turn — which the HTTP handler reports as
// 404 rather than silently succeeding.
func (r *answerRegistry) answer(sessionID, callID, text string) bool {
	r.mu.Lock()
	ch, ok := r.pending[[2]string{sessionID, callID}]
	if ok {
		delete(r.pending, [2]string{sessionID, callID})
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	ch <- text
	return true
}

// askUserQuestionSkippedText is what the model sees as the tool result when
// the user dismisses the question without picking an option or typing one —
// distinct from an empty string so the model can tell "skipped" apart from
// "answered with nothing," and phrased for the model's own reasoning, not
// shown to the user (the user already saw the card get dismissed).
const askUserQuestionSkippedText = "(the user skipped this question without answering; proceed using your own best judgment, or ask again later if it remains a blocker)"

// askUserQuestionOptionSchema is one {label, description?} choice.
func askUserQuestionOptionSchema() coretools.ValueSchema {
	return coretools.ValueSchema{
		Kind: coretools.KindObject,
		Properties: coretools.ParameterSchema{
			"label":       {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Short option text shown to the user."}, Required: true},
			"description": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Optional one-line elaboration shown under the label."}},
		},
	}
}

// newAskUserQuestionTool builds one session's ask_user_question tool.
// sessionID is baked in at construction (same pattern as buildAgent's own
// per-session python/bash/fs tools) so Execute never has to derive it from
// RunContext, which carries no session-scoping concept of its own.
func newAskUserQuestionTool(registry *answerRegistry, sessionID string) coretools.ToolDefinition {
	optionItem := askUserQuestionOptionSchema()
	return coretools.ToolDefinition{
		Name: askUserQuestionTool,
		Description: "Ask the user a clarifying question with a small set of concrete options, when their " +
			"request is ambiguous or underspecified enough that guessing risks real wasted work. The user " +
			"picks one option, types a free-form answer, or skips — whatever comes back arrives as this " +
			"call's result. Do not use this for routine confirmations or when a reasonable default is " +
			"obvious: prefer making progress over asking. This call blocks until the user responds, so ask " +
			"at most one question at a time and make it count.",
		Parameters: coretools.ParameterSchema{
			"question": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "The question to show the user."}, Required: true},
			"options": {
				ValueSchema: coretools.ValueSchema{
					Kind: coretools.KindArray, Items: &optionItem,
					Description: "2-4 concrete choices the user can pick from. The user can also type a free-form answer or skip, so options don't need to be exhaustive.",
				},
				Required: true,
			},
		},
		Output: coretools.Output{
			Schema: coretools.ValueSchema{Kind: coretools.KindString},
			Render: func(_ map[string]any, value any) []session.ContentBlock {
				return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
			},
		},
		// TimeoutMs left at zero deliberately: a real answer can take
		// however long the user needs, so this must not be cut off by the
		// same fixed timeout a self-contained tool (run_python, bash) uses.
		// The only bound is ctx — turn cancellation or process shutdown.
		Execute: func(ctx context.Context, _ map[string]any, run *coretools.RunContext) (any, error) {
			ch := registry.register(sessionID, run.CallID)
			defer registry.unregister(sessionID, run.CallID)
			select {
			case answer := <-ch:
				return answer, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
}

// handleAnswerQuestion is the browser's side of ask_user_question: it
// delivers the user's pick (or free-typed text, or a skip) to the Execute
// call blocked on it. A 404 here means the question already resolved
// (double-submit, e.g. a double click) or named a callId this session never
// asked — either way there's nothing left to deliver to.
func (s *Server) handleAnswerQuestion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.agents.Get(id); !ok {
		writeError(w, http.StatusNotFound, errors.New("no such session"))
		return
	}
	var body struct {
		CallID  string `json:"callId"`
		Text    string `json:"text"`
		Skipped bool   `json:"skipped"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.CallID == "" {
		writeError(w, http.StatusBadRequest, errors.New("callId is required"))
		return
	}
	text := body.Text
	if body.Skipped {
		text = askUserQuestionSkippedText
	}
	if !s.answers.answer(id, body.CallID, text) {
		writeError(w, http.StatusNotFound, errors.New("no pending question with that callId"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

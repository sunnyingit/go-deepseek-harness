package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	coreagent "go-harness/core/agent"
	"go-harness/core/agentloop"
	"go-harness/core/session"
	fsvocab "go-harness/fs/fs"
	"go-harness/fs/fssandbox"
	"go-harness/llm/tokenmeter"
	"go-harness/sandbox/sandbox"
)

// maxUploadBytes bounds one file upload — held fully in memory (see
// handleUploadFile), so this also bounds memory, not just disk.
func (s *Server) maxUploadBytes() int64 {
	if s.cfg.MaxUploadBytes > 0 {
		return s.cfg.MaxUploadBytes
	}
	return defaultUploadBytes
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleUI)
	mux.HandleFunc("GET /static/{path...}", s.handleStaticAsset)
	mux.HandleFunc("GET /dailog.html", s.handleDialog)
	mux.HandleFunc("GET /dialog.html", s.handleDialog)
	mux.HandleFunc("GET /space.html", s.handleSpace)
	mux.HandleFunc("GET /settings.html", s.handleSettings)
	mux.HandleFunc("GET /config", s.handleGetConfig)
	mux.HandleFunc("PUT /config", s.handlePutConfig)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /skills", s.handleListSkills)
	mux.HandleFunc("POST /sessions", s.handleCreateSession)
	mux.HandleFunc("GET /sessions", s.handleListSessions)
	mux.HandleFunc("GET /sessions/{id}", s.handleGetSession)
	mux.HandleFunc("GET /sessions/{id}/messages", s.handleGetMessages)
	mux.HandleFunc("POST /sessions/{id}/messages", s.handlePostMessage)
	mux.HandleFunc("GET /sessions/{id}/stream", s.handleStreamSession)
	mux.HandleFunc("POST /sessions/{id}/fork", s.handleFork)
	mux.HandleFunc("POST /sessions/{id}/plan-mode", s.handlePlanMode)
	mux.HandleFunc("GET /files", s.handleListAllFiles)
	mux.HandleFunc("POST /sessions/{id}/files", s.handleUploadFile)
	mux.HandleFunc("GET /sessions/{id}/files", s.handleListFiles)
	mux.HandleFunc("GET /sessions/{id}/files/{path...}", s.handleGetFile)
	mux.HandleFunc("DELETE /sessions/{id}/files/{path...}", s.handleDeleteFile)
	mux.HandleFunc("GET /sessions/{id}/tokens", s.handleGetTokens)
	mux.HandleFunc("POST /sessions/{id}/cancel", s.handleCancelSession)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := skillDTOs(s.skills)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, skills)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CWD string `json:"cwd"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // an empty/absent body is valid — cwd is optional

	sess, ag, err := s.createSession(body.CWD)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.sessionDTO(sess, ag))
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	ids := s.agents.List()
	out := make([]sessionDTO, 0, len(ids))
	for _, id := range ids {
		ag, ok := s.agents.Get(id)
		if !ok {
			continue
		}
		out = append(out, s.sessionDTOFromHandle(id, ag))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ag, ok := s.agents.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}
	writeJSON(w, http.StatusOK, s.sessionDTOFromHandle(id, ag))
}

func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ag, ok := s.agents.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}
	writeJSON(w, http.StatusOK, toMessageDTOs(ag.Session().DeriveMessages()))
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	agHandle, ok := s.agents.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}
	ag, ok := agHandle.(*agentloop.Agent)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("unexpected agent implementation"))
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Text == "" {
		writeError(w, http.StatusBadRequest, errors.New("text is required"))
		return
	}

	tokenHeader := &tokenmeter.Header{System: ag.SystemPrompt(), Tools: ag.ToolSchemas()}
	tokensBefore, _ := tokenmeter.New().Measure(ag.Session(), tokenHeader)

	before := len(ag.Session().DeriveMessages())
	ag.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: body.Text}}})
	ag.WhenIdle() // by the time this returns, the turn's own Hooks.TurnEnded has already synced it to disk — see buildAgent

	all := ag.Session().DeriveMessages()
	resp := map[string]any{"messages": toMessageDTOs(all[before:])}
	if usage, ok := turnTokenUsage(ag.Session(), tokenHeader, tokensBefore); ok {
		resp["usage"] = usage
	}
	writeJSON(w, http.StatusOK, resp)
}

// turnTokenUsage prices the token cost of whatever changed in sess since
// before was measured (the state right before this turn's Followup) —
// "one conversation turn's token consumption," which is what a UI showing
// a per-message cost actually wants, as opposed to GET /sessions/{id}/tokens'
// whole-session snapshot. ok is false only if re-measuring itself fails
// (a malformed surface — see llm/tokenmeter's own FoldSurfaceTokens docs),
// in which case the caller should simply omit usage rather than fail an
// otherwise-successful turn over a reporting problem.
func turnTokenUsage(sess *session.Session, header *tokenmeter.Header, before tokenmeter.Measurement) (map[string]any, bool) {
	after, err := tokenmeter.New().Measure(sess, header)
	if err != nil {
		return nil, false
	}
	turnTokens := after.TotalTokens - before.TotalTokens
	if turnTokens < 0 {
		turnTokens = 0 // shouldn't happen (the surface only grows across a turn), but a heuristic estimate is defensively clamped rather than shown negative
	}
	return map[string]any{"turnTokens": turnTokens, "totalTokens": after.TotalTokens}, true
}

// lastTurnDurationMs is the wall-clock length of the most recent turn, taken
// from the session log's own timestamps (turn/start → turn/end). Prefer this
// over the HTTP connection's elapsed time: a reconnecting stream did not
// watch the whole turn, but the log did.
func lastTurnDurationMs(events []session.Event) int64 {
	var start, end int64
	for _, ev := range events {
		switch ev.Type {
		case "turn/start":
			start, end = ev.Time, 0
		case "turn/end":
			if start != 0 {
				end = ev.Time
			}
		}
	}
	if start == 0 || end == 0 || end < start {
		return 0
	}
	return end - start
}

// lastTurnOutcome reads the most recent turn/end event's reason and (if
// present) error text — the durable record agent.go's own turn() now
// writes there specifically so a failure survives past the in-memory err
// value kick() otherwise discards. Used to tell a client watching live
// whether a turn actually succeeded, as opposed to just reaching idle:
// "idle" is also true after a model request exhausted its retries or a
// tool call errored, and reaching idle either way looks identical to
// Agent.Status() alone.
func lastTurnOutcome(events []session.Event) (reason, errMsg string) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "turn/end" {
			continue
		}
		data, ok := events[i].Data.(map[string]any)
		if !ok {
			return "", ""
		}
		reason, _ = data["reason"].(string)
		errMsg, _ = data["error"].(string)
		return reason, errMsg
	}
	return "", ""
}

// handleStreamSession is the streaming counterpart to handlePostMessage: an
// SSE connection (GET, not POST, so the browser's native EventSource can
// open it directly — EventSource has no way to send a request body) that
// triggers a turn via Followup and then tails the session's own event log
// as the turn produces it, instead of blocking on WhenIdle and returning
// everything at once. Polls rather than subscribing to a push source
// because core/session.Session has no such source — Events() is just a
// growing slice, so watching it for new entries on a short tick is the
// simplest thing that gives genuinely incremental output (down to
// individual assistant/chunk deltas, not just whole committed messages).
//
// text is optional: an empty/absent value means "just watch this session's
// current turn" instead of starting a new one. This is what a client
// reconnects with after its previous connection to this same in-flight
// turn was interrupted (e.g. a full page navigation tore down the
// EventSource) — the turn itself was never tied to that HTTP connection's
// lifetime (Followup hands off to the agent's own goroutine, running under
// its own context, not r.Context()), so it kept running server-side the
// whole time; this connection just resumes tailing it from here rather
// than replaying what already happened. If the turn has already finished
// by the time this connects, Status() is "idle" on the very first tick and
// this immediately reports "done" with nothing to tail — a harmless no-op,
// not an error.
func (s *Server) handleStreamSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	agHandle, ok := s.agents.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}
	ag, ok := agHandle.(*agentloop.Agent)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("unexpected agent implementation"))
		return
	}
	text := r.URL.Query().Get("text")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	sess := ag.Session()
	lastSeq := len(sess.Events())
	if text == "" {
		// Watch-only reconnect: if a step is currently open (the model is
		// mid-generation), start tailing from its own step/start instead of
		// from here — the client's static history fetch never saw any of
		// this step's content (assistant/chunk isn't surface-eligible, so
		// none of it reaches DeriveMessages until the step's assistant
		// /message finally commits), so replaying just the tail from
		// reconnect-time-forward would open a new bubble mid-sentence.
		// Replaying the whole open step instead reconstructs it faithfully,
		// tool calls and all, with no risk of duplicating anything the
		// static fetch already rendered — none of it was committed yet.
		if start := openStepStart(sess.Events()); start >= 0 {
			lastSeq = start
		}
	}
	// filesBefore is taken up front so a file the model manages to write in
	// the tiny window before the first poll tick still counts as part of
	// what this connection watched — diffed against filesAfter once the
	// turn goes idle to report exactly what changed (see emitFileEvents).
	// In reconnect (no-text) mode this only ever undercounts (files created
	// earlier in the turn, before this connection existed, won't show up
	// here — the client already has those from its prior connection or,
	// worst case, from the file list it refreshes on its own), never
	// overcounts.
	workspaceRoot := s.workspaceRoot(id)
	filesBefore, _ := listWorkspaceFiles(workspaceRoot)
	tokenHeader := &tokenmeter.Header{System: ag.SystemPrompt(), Tools: ag.ToolSchemas()}
	tokensBefore, _ := tokenmeter.New().Measure(sess, tokenHeader)
	connStarted := time.Now()
	if text != "" {
		ag.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: text}}})
	}

	writeSSE(w, "metadata", map[string]any{"model": s.cfg.Model})
	flusher.Flush()

	// pendingDisplay holds show_* tool calls that have been logged but whose
	// synthetic result hasn't landed yet. The rich MessageType event is
	// emitted only on a successful ack — a schema-invalid call never reaches
	// Execute, so forwarding the args on tool/call would paint a broken
	// table the model is about to be told to redo.
	pendingDisplay := map[string]displayPending{}

	ctx := r.Context()
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	// lastActivity backs the heartbeat below: a client has no way to tell
	// "nothing new has happened yet, but the connection and the turn are
	// both still fine" apart from "the connection is silently dead" — a
	// long-running tool call can easily go tens of seconds without a new
	// event. Sending something on a bounded interval regardless gives the
	// client a signal to reset its own stall timer against, instead of
	// guessing at how long a legitimate gap could be.
	lastActivity := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events := sess.Events()
			for ; lastSeq < len(events); lastSeq++ {
				writeStreamEvent(w, events[lastSeq], pendingDisplay)
				flusher.Flush()
				lastActivity = time.Now()
			}
			if ag.Status() == "idle" {
				// No explicit syncPersistence here: Hooks.TurnEnded (see
				// buildAgent) already synced the turn to disk unconditionally
				// as it closed, whether or not this connection was even open
				// to see it — that's the whole point of hanging it off the
				// turn's own lifecycle instead of this poll loop.
				emitFileEvents(w, workspaceRoot, filesBefore)
				doneData := map[string]any{}
				// Reaching idle isn't the same as succeeding: a model request
				// that ran out of retries or a tool call that errored also
				// ends the turn and reaches idle. Fold the real outcome into
				// this same "done" payload (not a separate event type) so the
				// client's one cleanup path always runs, but can render a
				// failure instead of quietly reporting success.
				if reason, errMsg := lastTurnOutcome(sess.Events()); reason == "error" {
					if errMsg == "" {
						errMsg = "the model request failed"
					}
					doneData["failed"] = true
					doneData["errorMessage"] = errMsg
				}
				if usage, ok := turnTokenUsage(sess, tokenHeader, tokensBefore); ok {
					doneData["usage"] = usage
				}
				if d := lastTurnDurationMs(sess.Events()); d > 0 {
					doneData["durationMs"] = d
				} else {
					elapsed := time.Since(connStarted).Milliseconds()
					if elapsed < 1 {
						elapsed = 1
					}
					doneData["durationMs"] = elapsed
				}
				writeSSE(w, "done", doneData)
				flusher.Flush()
				return
			}
			if time.Since(lastActivity) >= streamHeartbeatInterval {
				writeSSE(w, "heartbeat", map[string]any{})
				flusher.Flush()
				lastActivity = time.Now()
			}
		}
	}
}

// streamHeartbeatInterval bounds the longest silent gap a watching client
// should ever see while a turn is still genuinely in progress — see its use
// in handleStreamSession's poll loop.
const streamHeartbeatInterval = 8 * time.Second

// openStepStart returns the seq of the most recent step/start event that
// has no matching step/end yet (the step currently generating), or -1 if
// none is open.
func openStepStart(events []session.Event) int {
	start := -1
	for _, ev := range events {
		switch ev.Type {
		case "step/start":
			start = ev.Seq
		case "step/end":
			start = -1
		}
	}
	return start
}

// displayPending is a show_* tool/call waiting on its synthetic result.
// Payload is the model's filled-in arguments, forwarded as the SSE body
// once Execute acknowledges the display.
type displayPending struct {
	eventType string
	payload   map[string]any
}

// writeStreamEvent translates one session.Event into zero or one SSE
// events, using the same event-type vocabulary core/agentloop already logs
// (see its README's surface projection) rather than inventing a parallel
// one — except for display tools, which re-label the call as the frontend
// MessageType (table/chart/…) instead of a generic tool-call, because the
// whole point of those tools is the structured payload, not the fact a
// tool ran. Event types not handled here (turn/start, turn/end, step/*,
// user/message, assistant/message) carry no information the client needs
// beyond what it already sees via the chunk/tool-call/tool-result stream —
// assistant/message in particular is just the fully-assembled text the
// chunks it already received sum to.
func writeStreamEvent(w http.ResponseWriter, ev session.Event, pending map[string]displayPending) {
	data, _ := ev.Data.(map[string]any)
	switch ev.Type {
	case "assistant/chunk":
		block, ok := data["chunk"].(session.ContentBlock)
		if !ok || block.Text == "" {
			return
		}
		writeSSE(w, "chunk", map[string]any{"kind": string(block.Kind), "text": block.Text})
	case "tool/call":
		call, ok := data["call"].(session.ContentBlock)
		if !ok {
			return
		}
		if eventType, payload, ok := displayEvent(call); ok {
			pending[call.CallID] = displayPending{eventType: eventType, payload: payload}
			return
		}
		writeSSE(w, "tool-call", map[string]any{"toolName": call.ToolName, "args": call.Args, "callId": call.CallID})
	case "tool/result":
		msg, ok := data["message"].(agentloop.Message)
		if !ok || len(msg.Content) == 0 {
			return
		}
		callID := msg.Content[0].CallID
		if p, ok := pending[callID]; ok {
			delete(pending, callID)
			// Only paint the widget on the synthetic success ack. A
			// schema-invalid call still produces a tool/call then an Error
			// result — forwarding those args would render a broken table
			// the model is about to be told to redo. The Error text is for
			// the model (it lives in DeriveMessages); the user never sees
			// the button-press or the fake receipt.
			if msg.Content[0].Text == displayedAck {
				writeSSE(w, p.eventType, p.payload)
			}
			return
		}
		writeSSE(w, "tool-result", map[string]any{"text": msg.Content[0].Text, "callId": callID})
	}
}

func writeSSE(w http.ResponseWriter, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
}

func (s *Server) handleFork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Boundary *int `json:"boundary"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	boundary := -1
	if body.Boundary != nil {
		boundary = *body.Boundary
	}

	child, ag, err := s.forkSession(id, boundary)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.sessionDTO(child, ag))
}

// handleGetTokens reports a heuristic estimate of this session's current
// context usage — llm/tokenmeter has no provider-usage anchor to reuse in
// this port (see that package's README), so every call fully reprices the
// session's surface plus the agent's own system prompt and tool schemas
// under the fixed 4-chars-per-token heuristic. Good enough for a rough
// "how full is this context window" readout, not a billing record.
func (s *Server) handleGetTokens(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	agHandle, ok := s.agents.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}
	ag, ok := agHandle.(*agentloop.Agent)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("unexpected agent implementation"))
		return
	}

	measurement, err := tokenmeter.New().Measure(ag.Session(), &tokenmeter.Header{
		System: ag.SystemPrompt(),
		Tools:  ag.ToolSchemas(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, measurement)
}

func (s *Server) handlePlanMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	agHandle, ok := s.agents.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}
	ag, ok := agHandle.(*agentloop.Agent)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("unexpected agent implementation"))
		return
	}
	var body struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	outcome := s.planController.Set(ag, body.Active)
	writeJSON(w, http.StatusOK, map[string]string{"outcome": string(outcome)})
}

// handleUploadFile is the file-upload layer: a client POSTs a multipart
// "file" field, it lands directly in that session's workspace — the same
// directory read/write/edit/bash/run_python already operate on — so the
// model can act on it (e.g. call read) with no extra wiring on either
// side. Goes through fs/fssandbox.WriteBytes rather than writing the
// upload straight to disk, for two reasons: it reuses the same path
// -confinement the model's own tool calls are held to (the client can't
// upload its way outside the workspace via a crafted filename), and it
// keeps this handler unaware of whether the session's tools happen to be
// backed by the same or a different fs.FileSystem.
func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ag, ok := s.agents.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}

	limit := s.maxUploadBytes()
	if err := r.ParseMultipartForm(limit); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("parsing multipart form: %w", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf(`expected a multipart "file" field: %w`, err))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if int64(len(data)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("upload exceeds the %d-byte limit", limit))
		return
	}

	workspaceRoot := s.workspaceRoot(id)
	// filepath.Base strips any directory components the client sent — an
	// upload always lands directly in the workspace root by basename only,
	// never a client-chosen subpath.
	filename := filepath.Base(header.Filename)
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		writeError(w, http.StatusBadRequest, errors.New("the uploaded file needs a usable filename"))
		return
	}

	provider := fssandbox.New(workspaceRoot, sandbox.WorkspaceWrite)
	target, err := provider.Resolve(r.Context(), filename, fsvocab.ResolveOptions{CWD: workspaceRoot})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	info, err := provider.WriteBytes(r.Context(), target, data, fsvocab.Unconditional())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// Without this, the model has no way to learn a file exists except by
	// listing the workspace directory itself (e.g. bash "ls") — which can't
	// tell "the file I just uploaded" apart from anything else already
	// sitting there from an earlier upload, so "process the file I
	// uploaded" ends up processing everything it finds. Inject (not
	// Followup) so this note sits in the inbox without forcing a turn to
	// start on its own — it only actually reaches the model folded in
	// alongside whatever the user asks next.
	if err := recordUploadedPath(workspaceRoot, target.DisplayPath); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("recording upload: %w", err))
		return
	}

	ag.Inject(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{
		Kind: agentloop.BlockText,
		Text: fmt.Sprintf("[Uploaded file] %s (%d bytes). This is the file just uploaded — use exactly this path for \"the file I uploaded\"; don't process other files in the workspace unless asked.", target.DisplayPath, info.Size),
	}}})

	writeJSON(w, http.StatusCreated, map[string]any{"path": target.DisplayPath, "bytes": info.Size})
}

// handleCancelSession aborts whatever turn is currently in flight for a
// session — the "stop generating" button POST /sessions/{id}/messages has
// no way to trigger on its own, since that request just blocks until
// Agent.WhenIdle() returns. This call doesn't block on that itself: it
// only asks the phase context to cancel and returns immediately: the
// blocked /messages request (on whatever other connection sent it) is what
// actually unblocks once the turn notices and unwinds, returning its own
// response with however much of the transcript that turn managed to
// commit before it was cut off — Agent.Cancel guarantees a turn always
// closes out (turn/end still gets appended) rather than leaving the
// session in a half-open state.
//
// keepInbox is hardcoded false: a "cancel" request means stop everything,
// including any Steer/Inject messages still queued for the next step —
// not "pause and resume automatically." A caller wanting the queued-work
// -survives behavior would need Agent.Cancel exposed with that choice,
// which this endpoint doesn't do.
func (s *Server) handleCancelSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ag, ok := s.agents.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // an empty/absent body is valid — reason is optional
	if body.Reason == "" {
		body.Reason = "cancelled via API"
	}

	ag.Cancel(agentloop.CancelCause{Kind: "user", Reason: body.Reason}, false)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

// sessionDTO builds the response DTO right after creating or forking a
// session, when the caller already has the concrete *session.Session in
// hand (sessionDTOFromHandle re-derives the same fields from a registry
// lookup, for the list/get paths where all that's on hand is the
// coreagent.Agent interface).
func (s *Server) sessionDTO(sess *session.Session, ag *agentloop.Agent) sessionDTO {
	return s.sessionDTOFromHandle(string(sess.ID()), ag)
}

func (s *Server) sessionDTOFromHandle(id string, ag coreagent.Agent) sessionDTO {
	header := ag.Session().Header()
	active := false
	if concrete, ok := ag.(*agentloop.Agent); ok {
		active, _ = s.planController.Get(concrete)
	}
	dto := sessionDTO{
		ID: id, CWD: header.CWD, ParentSession: string(header.ParentSession),
		PlanModeActive: active, Status: ag.Status(),
		Title:     sessionTitle(ag.Session()),
		CreatedAt: header.CreatedAt,
	}
	if entries, err := listWorkspaceFiles(s.workspaceRoot(id)); err == nil {
		dto.FileCount = len(entries)
	}
	return dto
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

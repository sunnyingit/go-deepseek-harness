package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	coreagent "go-harness/core/agent"
	"go-harness/core/agentloop"
	"go-harness/core/session"
)

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /skills", s.handleListSkills)
	mux.HandleFunc("POST /sessions", s.handleCreateSession)
	mux.HandleFunc("GET /sessions", s.handleListSessions)
	mux.HandleFunc("GET /sessions/{id}", s.handleGetSession)
	mux.HandleFunc("GET /sessions/{id}/messages", s.handleGetMessages)
	mux.HandleFunc("POST /sessions/{id}/messages", s.handlePostMessage)
	mux.HandleFunc("POST /sessions/{id}/fork", s.handleFork)
	mux.HandleFunc("POST /sessions/{id}/plan-mode", s.handlePlanMode)
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
	ag, ok := s.agents.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
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

	before := len(ag.Session().DeriveMessages())
	ag.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: body.Text}}})
	ag.WhenIdle()

	all := ag.Session().DeriveMessages()
	writeJSON(w, http.StatusOK, map[string]any{"messages": toMessageDTOs(all[before:])})
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
	return sessionDTO{
		ID: id, CWD: header.CWD, ParentSession: string(header.ParentSession),
		PlanModeActive: active, Status: ag.Status(),
	}
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

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go-harness/core/agentloop"
)

// trackAnalyst registers an in-flight analyst so POST …/agents/stop can Cancel
// it. Returns untrack; call via defer. Does not take lockCode — stop must not
// wait on the stock lock held by the streaming handler.
func (s *Server) trackAnalyst(key string, ag *agentloop.Agent) func() {
	if s == nil || ag == nil {
		return func() {}
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return func() {}
	}
	s.liveMu.Lock()
	if s.liveAgents == nil {
		s.liveAgents = map[string]map[*agentloop.Agent]struct{}{}
	}
	set := s.liveAgents[key]
	if set == nil {
		set = map[*agentloop.Agent]struct{}{}
		s.liveAgents[key] = set
	}
	set[ag] = struct{}{}
	s.liveMu.Unlock()
	return func() {
		s.liveMu.Lock()
		defer s.liveMu.Unlock()
		if set := s.liveAgents[key]; set != nil {
			delete(set, ag)
			if len(set) == 0 {
				delete(s.liveAgents, key)
			}
		}
	}
}

// cancelAnalysts Cancels every tracked agent for key. Returns how many were
// asked to cancel (0 is OK — idempotent). Does not acquire lockCode.
func (s *Server) cancelAnalysts(key, reason string) int {
	if s == nil {
		return 0
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return 0
	}
	if strings.TrimSpace(reason) == "" {
		reason = "stopped via API"
	}
	s.liveMu.Lock()
	set := s.liveAgents[key]
	agents := make([]*agentloop.Agent, 0, len(set))
	for ag := range set {
		agents = append(agents, ag)
	}
	s.liveMu.Unlock()
	for _, ag := range agents {
		ag.Cancel(agentloop.CancelCause{Kind: "user", Reason: reason}, false)
	}
	return len(agents)
}

func analystTrackKey(code string, cfg agentConfig) string {
	key := strings.TrimSpace(code)
	if key != "" {
		return key
	}
	return cfg.ID + ":topic"
}

// handleAnalystStop stops in-flight diagnose / ask_analyst agents for a stock.
// Disconnecting SSE alone does not cancel; this endpoint does.
func (s *Server) handleAnalystStop(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !tsCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("股票代码无法识别: %s", code))
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	n := s.cancelAnalysts(code, body.Reason)
	writeJSON(w, http.StatusOK, map[string]any{"status": "cancelling", "cancelled": n})
}

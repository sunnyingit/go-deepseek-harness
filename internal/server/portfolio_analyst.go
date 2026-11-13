package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-harness/core/agentloop"
	"go-harness/core/session"
	coretools "go-harness/core/tools"
	"go-harness/internal/sched"
)

// The portfolio analyst looks at a set of stocks together: the user's holdings and wishes ("check"), or any two to
// six stocks the user picks to compare ("compare"). Rules work out the facts, the model weighs them: one call, and
// only facts from the table may be cited.

type portfolioItem struct {
	Code    string   `json:"code"`
	Name    string   `json:"name"`
	Title   string   `json:"title"`
	Why     string   `json:"why"`
	Action  string   `json:"action"`
	FactIDs []string `json:"fact_ids"`
}

type portfolioResult struct {
	Mode        string          `json:"mode"`
	Codes       []string        `json:"codes"`
	Text        string          `json:"text"`
	Items       []portfolioItem `json:"items"`
	FactIDs     []string        `json:"fact_ids"`
	Questions   []string        `json:"suggested_questions"`
	Facts       []pfFact        `json:"facts"`
	GeneratedAt string          `json:"generated_at"`
	InputHash   string          `json:"input_hash"`
}

// portfolioReply is the answer to one follow-up question; it is not kept.
type portfolioReply struct {
	Answer      string `json:"answer"`
	GeneratedAt string `json:"generated_at"`
}

// agentFile reads one of the portfolio analyst's text files.
func portfolioFile(name string) string {
	root, err := agentsRoot()
	if err != nil {
		return ""
	}
	raw, _ := os.ReadFile(filepath.Join(root, "portfolio", name))
	return strings.TrimSpace(string(raw))
}

func portfolioSystem(reply bool) string {
	rules := portfolioFile("rules.md")
	if reply {
		return "你是持仓分析师，正在回答用户对一组股票的追问。只根据事实表说话。\n\n" + rules
	}
	return portfolioFile("prompt.md") + "\n\n## 规则\n\n" + rules
}

func factsPrompt(mode string, codes []string, question string, facts []pfFact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mode=%s\n", mode)
	if q := strings.TrimSpace(question); q != "" {
		fmt.Fprintf(&b, "用户的问题：%s\n", q)
	}
	b.WriteString("\n事实表：\n")
	for _, f := range facts {
		fmt.Fprintf(&b, "- %s: %s\n", f.ID, f.Text)
	}
	return b.String()
}

// portfolioCall makes the one model call. portfolioFn replaces it in tests.
func (s *Server) portfolioCall(ctx context.Context, system, user string) (string, error) {
	if s.portfolioFn != nil {
		return s.portfolioFn(ctx, system, user)
	}
	if s.llm == nil {
		return "", errors.New("模型还没有配置：先到设置里配好模型密钥")
	}
	sess := session.New(session.SessionId(fmt.Sprintf("portfolio-%d", time.Now().UnixNano())), session.Header{})
	ag := s.newAnalyst(sess, coretools.NewRegistry(), 2, system, 2048)
	ag.Followup(session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{Kind: session.BlockText, Text: user}}})
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			ag.Cancel(agentloop.CancelCause{Kind: "user", Reason: "client disconnected"}, false)
		case <-stopped:
		}
	}()
	ag.WhenIdle()
	close(stopped)
	text := strings.TrimSpace(lastAssistantText(sess.DeriveMessages()))
	if text == "" {
		if e := lastTurnError(sess); e != "" {
			return "", errors.New(e)
		}
		return "", errors.New("持仓分析师这次没有写出内容，请再试一次")
	}
	return text, nil
}

// parsePortfolio reads the model's JSON and keeps only what checks out against the table.
func parsePortfolio(raw string, facts []pfFact, codes []string, nameOf func(string) string) (portfolioResult, error) {
	var out struct {
		Text      string   `json:"text"`
		FactIDs   []string `json:"fact_ids"`
		Questions []any    `json:"suggested_questions"`
		Items     []struct {
			Code    string   `json:"code"`
			Title   string   `json:"title"`
			Why     string   `json:"why"`
			Action  string   `json:"action"`
			FactIDs []string `json:"fact_ids"`
		} `json:"items"`
	}
	if err := decodeObject(raw, &out); err != nil || strings.TrimSpace(out.Text) == "" {
		return portfolioResult{}, errors.New("持仓分析师这次没有给出可用的结论，请再试一次")
	}
	known := map[string]bool{}
	for _, f := range facts {
		known[f.ID] = true
	}
	inSet := map[string]bool{}
	for _, c := range codes {
		inSet[c] = true
	}
	keep := func(ids []string) []string {
		var ok []string
		for _, id := range ids {
			if id = strings.TrimSpace(id); known[id] {
				ok = append(ok, id)
			}
		}
		return ok
	}
	res := portfolioResult{Text: strings.TrimSpace(out.Text), FactIDs: keep(out.FactIDs), Items: []portfolioItem{}}
	for _, it := range out.Items {
		code := strings.ToUpper(strings.TrimSpace(it.Code))
		if !inSet[code] || strings.TrimSpace(it.Title) == "" {
			continue
		}
		res.Items = append(res.Items, portfolioItem{Code: code, Name: nameOf(code), Title: strings.TrimSpace(it.Title), Why: strings.TrimSpace(it.Why), Action: cleanAction(it.Action), FactIDs: keep(it.FactIDs)})
	}
	for _, q := range out.Questions {
		switch v := q.(type) {
		case string:
			if v = strings.TrimSpace(v); v != "" {
				res.Questions = append(res.Questions, v)
			}
		case map[string]any:
			if t, _ := v["text"].(string); strings.TrimSpace(t) != "" {
				res.Questions = append(res.Questions, strings.TrimSpace(t))
			}
		}
	}
	if len(res.Questions) > 3 {
		res.Questions = res.Questions[:3]
	}
	return res, nil
}

// ---- keeping the latest result for each set of stocks

type portfolioStore struct {
	mu   sync.Mutex
	path string
	all  map[string]portfolioResult
}

func openPortfolio(path string) *portfolioStore {
	p := &portfolioStore{path: path, all: map[string]portfolioResult{}}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(raw, &p.all)
		}
	}
	return p
}

func (p *portfolioStore) get(key string) (portfolioResult, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.all[key]
	return r, ok
}

func (p *portfolioStore) put(key string, r portfolioResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.all[key] = r
	if p.path == "" {
		return
	}
	if raw, err := json.Marshal(p.all); err == nil {
		tmp := p.path + ".tmp"
		if os.WriteFile(tmp, raw, 0o644) == nil {
			_ = os.Rename(tmp, p.path)
		}
	}
}

var portfolioStores sync.Map

func (s *Server) portfolioResults() *portfolioStore {
	path := ""
	if strings.TrimSpace(s.cfg.DataDir) != "" {
		path = filepath.Join(s.cfg.DataDir, "client", "portfolio_analyst.json")
	}
	if path == "" {
		return openPortfolio("")
	}
	if v, ok := portfolioStores.Load(path); ok {
		return v.(*portfolioStore)
	}
	v, _ := portfolioStores.LoadOrStore(path, openPortfolio(path))
	return v.(*portfolioStore)
}

func portfolioKey(mode string, codes []string) string {
	if mode == "check" {
		return "check"
	}
	return "compare:" + strings.Join(codes, ",")
}

func hashFacts(facts []pfFact) string {
	h := sha1.New()
	for _, f := range facts {
		h.Write([]byte(f.ID + "=" + f.Text + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// analysePortfolio makes (or reuses) the analyst's reading of a set of stocks. The same facts give the same
// reading without another model call, unless force asks for a new one.
func (s *Server) analysePortfolio(ctx context.Context, mode string, codes []string, force bool) (portfolioResult, bool, error) {
	if mode != "check" && mode != "compare" {
		return portfolioResult{}, false, errors.New("mode 要是 check 或 compare")
	}
	facts, used := s.portfolioFacts(mode, codes)
	if mode == "compare" && len(used) < 2 {
		return portfolioResult{}, false, errors.New("对比至少要选两只股票")
	}
	if len(used) == 0 {
		return portfolioResult{}, false, errors.New("你还没有持有或想买的股票，没有东西可分析")
	}
	key, hash := portfolioKey(mode, used), hashFacts(facts)
	store := s.portfolioResults()
	if saved, ok := store.get(key); ok && !force && saved.InputHash == hash {
		return saved, true, nil
	}
	raw, err := s.portfolioCall(ctx, portfolioSystem(false), factsPrompt(mode, used, "", facts))
	if err != nil {
		return portfolioResult{}, false, err
	}
	res, err := parsePortfolio(raw, facts, used, s.stockName)
	if err != nil {
		log.Printf("portfolio analyst: could not read its answer: %q", pfClip(raw, 600))
		return portfolioResult{}, false, err
	}
	res.Mode, res.Codes, res.Facts, res.InputHash = mode, used, facts, hash
	res.GeneratedAt = s.now().In(sched.Zone).Format("2006-01-02 15:04")
	store.put(key, res)
	return res, false, nil
}

func (s *Server) replyPortfolio(ctx context.Context, mode string, codes []string, question string) (portfolioReply, error) {
	facts, used := s.portfolioFacts(mode, codes)
	if len(used) == 0 {
		return portfolioReply{}, errors.New("没有可以分析的股票")
	}
	raw, err := s.portfolioCall(ctx, portfolioSystem(true), factsPrompt(mode, used, question, facts))
	if err != nil {
		return portfolioReply{}, err
	}
	text := strings.TrimSpace(speakArgIDs(raw))
	if text == "" || forbiddenHit(text, actionBanned) {
		return portfolioReply{}, errors.New("这次的回答没有通过检查，请换个问法")
	}
	return portfolioReply{Answer: text, GeneratedAt: s.now().In(sched.Zone).Format("2006-01-02 15:04")}, nil
}

func splitCodes(q string) []string { return cleanCodes(strings.Split(q, ",")) }

func (s *Server) registerPortfolioRoutes(mux *http.ServeMux) {
	// GET gives what was made last for this set of stocks, without calling the model
	mux.HandleFunc("GET /api/portfolio/analyst", func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		if mode != "compare" {
			mode = "check"
		}
		codes := splitCodes(r.URL.Query().Get("codes"))
		key := portfolioKey(mode, codes)
		if mode == "check" {
			held, wish := s.qualitySubjects()
			key = portfolioKey("check", append(held, wish...))
		}
		if res, ok := s.portfolioResults().get(key); ok {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "result": res})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "none"})
	})
	mux.HandleFunc("POST /api/portfolio/analyst", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mode     string   `json:"mode"`
			Codes    []string `json:"codes"`
			Question string   `json:"question"`
			Force    bool     `json:"force"`
		}
		_ = readJSON(r, &body)
		mode := body.Mode
		if mode == "" {
			mode = "check"
		}
		if strings.TrimSpace(body.Question) != "" {
			rep, err := s.replyPortfolio(r.Context(), mode, body.Codes, body.Question)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]any{"status": "error", "error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "reply": rep})
			return
		}
		res, cached, err := s.analysePortfolio(r.Context(), mode, body.Codes, body.Force)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"status": "error", "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "result": res, "cached": cached})
	})
}

// portfolioReview runs the check-up reading each evening after the nightly conclusions, so the card is fresh when
// the user opens it. Nothing changed since the last reading means no model call.
func (s *Server) portfolioReview(c *sched.JobCtx) (any, error) {
	if s.llm == nil && s.portfolioFn == nil {
		return map[string]any{"skipped": "no model"}, nil
	}
	if held, wish := s.qualitySubjects(); len(held)+len(wish) == 0 {
		return map[string]any{"skipped": "nothing to look at"}, nil
	}
	_, cached, err := s.analysePortfolio(c.Ctx, "check", nil, false)
	if err != nil {
		return nil, sched.Transient(err.Error())
	}
	return map[string]any{"cached": cached}, nil
}

func (s *Server) registerPortfolioTask() {
	s.sch.Register(&sched.Spec{
		ID: "portfolio.review", Title: "持仓分析师", Category: sched.CatScan, Note: "盘后对整个持仓组合做一轮综述",
		Trace: map[string]any{
			"apis":    []any{"本地持仓事实汇总", "LLM 持仓综述"},
			"purpose": "盘后对整个持仓组合做一轮综述并缓存",
		},
		Queue:   "llm",
		Trigger: sched.Trigger{Kind: "daily", At: "21:30", WindowMin: 180, Days: "trading"}, MaxAttempts: 2, Backoff: []time.Duration{10 * time.Minute, 40 * time.Minute},
		CatchupTTL: 10 * time.Hour, Timeout: 6 * time.Minute, BudgetCalls: 3, Handler: s.portfolioReview,
	})
}

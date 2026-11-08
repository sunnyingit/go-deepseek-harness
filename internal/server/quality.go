package server

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"go-harness/internal/inbox"
	"go-harness/internal/personal"
	"go-harness/internal/sched"
)

// The quality screen is two questions asked of the data service, for the stocks the user holds or wants to buy:
// has the company stepped on a red line (ST, two loss-making years, an investigation or penalty, extreme debt), and
// which companies in a wished-for stock's industry have the best quality by plain rules. Rules, not predictions.

type qualityRow struct {
	Code  string         `json:"ts_code"`
	Name  string         `json:"name"`
	Kind  string         `json:"kind"` // hold | wish
	Level string         `json:"level"`
	Flags []any          `json:"flags"`
	Basis map[string]any `json:"metrics"`
}

type qualityPick struct {
	Code     string `json:"for_code"`
	Name     string `json:"for_name"`
	Industry string `json:"industry"`
	Status   string `json:"status"` // ok | computing | empty | error
	Items    []any  `json:"items"`
	Checked  int    `json:"checked"`
	Note     string `json:"note,omitempty"`
}

// qualitySubjects are the stocks worth screening: what the user holds and what they want to buy.
func (s *Server) qualitySubjects() (held []string, wish []string) {
	pos := personal.Held(s.personal.Trades())
	for code, p := range pos {
		if p != nil && p.Qty > 0 {
			held = append(held, code)
		}
	}
	sort.Strings(held)
	seen := map[string]bool{}
	for _, w := range s.personal.Wishes() {
		if w.Status == "open" && !seen[w.Code] && pos[w.Code] == nil {
			seen[w.Code] = true
			wish = append(wish, w.Code)
		}
	}
	return held, wish
}

func (s *Server) qualityView() map[string]any {
	held, wish := s.qualitySubjects()
	rows := make([]qualityRow, len(held)+len(wish))
	picks := make([]qualityPick, len(wish))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fn()
		}()
	}
	for i, code := range append(append([]string{}, held...), wish...) {
		i, code := i, code
		kind := "hold"
		if i >= len(held) {
			kind = "wish"
		}
		run(func() {
			row := qualityRow{Code: code, Name: s.stockName(code), Kind: kind, Level: "unknown", Flags: []any{}}
			var body map[string]any
			if err := s.stockGetJSON("/api/v1/shared/quality/"+url.PathEscape(code), &body); err == nil && body["status"] == "ok" {
				row.Level, _ = body["level"].(string)
				if f, ok := body["flags"].([]any); ok {
					row.Flags = f
				}
				row.Basis, _ = body["metrics"].(map[string]any)
			}
			rows[i] = row
		})
	}
	for i, code := range wish {
		i, code := i, code
		run(func() {
			pk := qualityPick{Code: code, Name: s.stockName(code), Status: "error", Items: []any{}}
			var body map[string]any
			if err := s.stockGetJSON("/api/v1/shared/quality-peers/"+url.PathEscape(code)+"?limit=3", &body); err == nil {
				pk.Status, _ = body["status"].(string)
				pk.Industry, _ = body["industry"].(string)
				pk.Note, _ = body["note"].(string)
				if items, ok := body["items"].([]any); ok {
					pk.Items = items
				}
				if n, ok := body["checked"].(float64); ok {
					pk.Checked = int(n)
				}
			}
			picks[i] = pk
		})
	}
	wg.Wait()
	red, attention, pending := 0, 0, false
	for _, r := range rows {
		switch r.Level {
		case "red":
			red++
		case "attention":
			attention++
		}
	}
	for _, p := range picks {
		pending = pending || p.Status == "computing"
	}
	return map[string]any{
		"rows": rows, "picks": picks, "pending": pending, "red": red, "attention": attention,
		"generated_at": s.now().In(sched.Zone).Format("2006-01-02 15:04"),
	}
}

func (s *Server) handleQuality(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.qualityView())
}

// qualityWatch is the daily look: it tells the user when a stock they hold or want has just stepped on a red
// line, and when the companies worth a look beside a stock they want have changed. Each is said once.
func (s *Server) qualityWatch(c *sched.JobCtx) (any, error) {
	var view map[string]any
	for try := 0; try < 12; try++ {
		view = s.qualityView()
		if pending, _ := view["pending"].(bool); !pending {
			break
		}
		select {
		case <-c.Ctx.Done():
			return nil, c.Ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	redline, picks := 0, 0
	for _, r := range view["rows"].([]qualityRow) {
		if r.Level != "red" {
			continue
		}
		var flags []string
		for _, f := range r.Flags {
			if m, ok := f.(map[string]any); ok && m["level"] == "red" {
				flags = append(flags, fmt.Sprint(m["text"]))
			}
		}
		sum := sha1.Sum([]byte(strings.Join(flags, "|")))
		kind := map[string]string{"hold": "持有", "wish": "想买"}[r.Kind]
		_, created, err := s.inbox.Publish("quality.redline", "stock", r.Code, map[string]any{"ts_code": r.Code, "name": r.Name, "kind_text": kind, "flags": flags},
			"quality.redline:"+r.Code+":"+hex.EncodeToString(sum[:4]), inbox.Options{})
		if err == nil && created {
			redline++
		}
	}
	for _, p := range view["picks"].([]qualityPick) {
		if p.Status != "ok" || len(p.Items) == 0 {
			continue
		}
		var lines, codes []string
		for _, it := range p.Items {
			if m, ok := it.(map[string]any); ok {
				lines = append(lines, fmt.Sprintf("%s · %v", m["name"], m["why"]))
				codes = append(codes, fmt.Sprint(m["ts_code"]))
			}
		}
		_, created, err := s.inbox.Publish("quality.picks", "stock", p.Code, map[string]any{"ts_code": p.Code, "name": p.Name, "industry": p.Industry, "items": lines},
			"quality.picks:"+p.Code+":"+strings.Join(codes, ","), inbox.Options{})
		if err == nil && created {
			picks++
		}
	}
	return map[string]any{"red": view["red"], "attention": view["attention"], "told_redline": redline, "told_picks": picks}, nil
}

func (s *Server) registerQualityTask() {
	s.sch.Register(&sched.Spec{
		ID: "quality.watch", Title: "质地筛查", Queue: "data",
		Trigger: sched.Trigger{Kind: "daily", At: "20:05", WindowMin: 240, Days: "trading"}, MaxAttempts: 2, Backoff: []time.Duration{15 * time.Minute, time.Hour},
		CatchupTTL: 20 * time.Hour, Timeout: 8 * time.Minute, Handler: s.qualityWatch,
	})
	s.registerPortfolioTask()
}

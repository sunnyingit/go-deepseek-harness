package server

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go-harness/internal/holdings"
	"go-harness/internal/personal"
	"go-harness/internal/sched"
)

func levelOfVerdict(verdict string) int {
	switch {
	case strings.Contains(verdict, "比较明显"):
		return 2
	case strings.Contains(verdict, "稍微"):
		return 1
	case strings.Contains(verdict, "明显"):
		return 3
	}
	return 0
}

// sharedCache keeps answers of the shared data service for a while.
type sharedCache struct {
	mu   sync.Mutex
	data map[string]cachedBody
}

type cachedBody struct {
	at   time.Time
	body map[string]any
}

// shared fetches one of the data service's shared endpoints, with a short cache. A failure returns nil.
func (s *Server) shared(path string, ttl time.Duration) map[string]any {
	s.sharedMu.Lock()
	if s.sharedCache == nil {
		s.sharedCache = map[string]cachedBody{}
	}
	if hit, ok := s.sharedCache[path]; ok && s.now().Sub(hit.at) < ttl {
		s.sharedMu.Unlock()
		return hit.body
	}
	s.sharedMu.Unlock()
	var body map[string]any
	if err := s.stockGetJSON("/api/v1/shared"+path, &body); err != nil {
		return nil
	}
	s.sharedMu.Lock()
	s.sharedCache[path] = cachedBody{at: s.now(), body: body}
	s.sharedMu.Unlock()
	return body
}

func fnum(v any) *float64 {
	if f, ok := v.(float64); ok {
		return &f
	}
	return nil
}

// holdingsData is what the holdings page reads from outside. The market and sector inputs come from the
// shared data service; the AI's conclusions come from this machine's own bundles.
type holdingsData struct{ s *Server }

func (d holdingsData) Quotes(names map[string]string) map[string]personal.Quote {
	codes := make([]string, 0, len(names))
	for c := range names {
		codes = append(codes, c)
	}
	got, _ := d.s.quotes.Fetch(codes)
	out := map[string]personal.Quote{}
	for c, q := range got {
		out[c] = personal.Quote{Price: q.Price, Change: q.Change, ChangePct: q.ChangePct}
	}
	return out
}

func seriesOf(body map[string]any) holdings.Series {
	var out holdings.Series
	items, _ := body["items"].([]any)
	for _, it := range items {
		pair, _ := it.([]any)
		if len(pair) == 2 {
			day, _ := pair[0].(string)
			if cl, ok := pair[1].(float64); ok && day != "" {
				out = append(out, holdings.Bar{Day: day, Close: cl})
			}
		}
	}
	return out
}

func (d holdingsData) Closes(code string) holdings.Series {
	return seriesOf(d.s.shared("/closes/"+url.PathEscape(code), 30*time.Minute))
}
func (d holdingsData) IndexCloses() holdings.Series {
	return seriesOf(d.s.shared("/index-closes", 30*time.Minute))
}

func (d holdingsData) Flow(code string) (int, int) {
	// 持仓页会并行预热；缓存稍长，避免 20 秒轮询又打冷路径。
	b := d.s.shared("/flow/"+url.PathEscape(code), 15*time.Minute)
	in, _ := b["in"].(float64)
	out, _ := b["out"].(float64)
	return int(in), int(out)
}

func (d holdingsData) AI(code string) holdings.AI {
	b := d.s.readBundle(code)
	if b == nil || b.Chief == nil {
		return holdings.AI{}
	}
	verdict, _ := b.Chief["verdict"].(string)
	text, _ := b.Chief["text"].(string)
	ai := holdings.AI{Has: true, Lean: leanOf(verdict), Level: levelOfVerdict(verdict), Label: verdict, Headline: text, JudgedAt: b.GeneratedAt,
		Stale: d.s.staleReason(b, d.s.now()) != "", TradeDate: b.TradeDate}
	if ai.Lean == "neutral" {
		ai.Lean = "unclear"
	}
	if acts, ok := b.Chief["actions"].(map[string]any); ok {
		ai.Hold, _ = acts["hold"].(string)
		ai.Buy, _ = acts["buy"].(string)
	}
	for _, row := range b.Analysts {
		voice := holdings.AnalystVoice{
			ID: row.Dimension, Name: row.Name, Short: row.Label, State: row.State,
		}
		if h := row.Headline; h != nil {
			if t, ok := h["text"].(string); ok {
				voice.Text = strings.TrimSpace(t)
			}
			st, _ := h["status_text"].(string)
			if st == "" {
				st, _ = h["status"].(string)
				if mapped := statusTextOf[st]; mapped != "" {
					st = mapped
				}
			}
			voice.Label = strings.TrimSpace(st)
			voice.Lean = leanOf(voice.Label)
			if voice.Lean == "neutral" {
				voice.Lean = "unclear"
			}
		}
		if voice.Text == "" && row.Reason != "" {
			voice.Text = row.Reason
		}
		if voice.Label == "" {
			if row.State == "ok" {
				voice.Label = "—"
			} else if row.State == "failed" || row.State == "missing" {
				voice.Label = "暂无"
				voice.Lean = "unclear"
			}
		}
		ai.Analysts = append(ai.Analysts, voice)
	}
	hist := d.s.verdictHistory(code)
	if n := len(hist); n >= 2 {
		before, after := hist[n-2], hist[n-1]
		if after["lean"] != before["lean"] && fmt.Sprint(after["trade_date"]) >= sched.Day(d.s.cal.LastClose(d.s.now())) {
			names := map[string]string{"bullish": "偏多", "bearish": "偏空", "neutral": "不明"}
			ai.Change = fmt.Sprintf("从%s变为%s", names[fmt.Sprint(before["lean"])], names[fmt.Sprint(after["lean"])])
		}
	}
	return ai
}

func (d holdingsData) Judgments(code string) []holdings.Judgment {
	var out []holdings.Judgment
	for _, h := range d.s.verdictHistory(code) {
		lean := fmt.Sprint(h["lean"])
		if lean == "neutral" {
			lean = "unclear"
		}
		level, _ := h["level"].(int)
		out = append(out, holdings.Judgment{TradeDate: fmt.Sprint(h["trade_date"]), Lean: lean, Level: level})
	}
	return out
}

func (d holdingsData) RequestJudgment(code string) {
	_, _ = d.s.requestConclusion(code, "holdings", false)
}

func (d holdingsData) Candidates() []holdings.Candidate {
	files, _ := filepath.Glob(filepath.Join(d.s.cfg.DataDir, "bundles", "*.json"))
	var out []holdings.Candidate
	for _, f := range files {
		code := strings.ToUpper(strings.TrimSuffix(filepath.Base(f), ".json"))
		b := d.s.readBundle(code)
		if b == nil || b.Chief == nil {
			continue
		}
		verdict, _ := b.Chief["verdict"].(string)
		if leanOf(verdict) != "bullish" {
			continue
		}
		out = append(out, holdings.Candidate{Code: code, Name: d.s.stockName(code), Label: verdict, TradeDate: b.TradeDate})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].TradeDate > out[b].TradeDate })
	if len(out) > 40 {
		out = out[:40]
	}
	_ = os.PathSeparator
	return out
}

func (d holdingsData) Market() *holdings.Market {
	m, _ := d.s.shared("/market", time.Minute)["market"].(map[string]any)
	if m == nil {
		return nil
	}
	return &holdings.Market{Level: fmt.Sprint(m["level"]), Tilt: strings.TrimPrefix(fmt.Sprint(m["tilt"]), "<nil>"), Action: strings.TrimPrefix(fmt.Sprint(m["action"]), "<nil>"),
		Text: fmt.Sprint(m["text"]), Stance: fmt.Sprint(m["stance"])}
}

func (d holdingsData) MarketSince() string {
	since, _ := d.s.shared("/market-since", 10*time.Minute)["since"].(string)
	return since
}

func sectorRow(m map[string]any) holdings.SectorRow {
	name, _ := m["name"].(string)
	verdict, _ := m["verdict"].(string)
	source, _ := m["source"].(string)
	tracked, _ := m["tracked"].(bool)
	return holdings.SectorRow{Name: name, Verdict: verdict, Flow: fnum(m["flow"]), Heat: fnum(m["heat"]), Source: source, Tracked: tracked}
}

func (d holdingsData) TrackedSectors() []holdings.SectorRow {
	items, _ := d.s.shared("/tracked-sectors", 10*time.Minute)["items"].([]any)
	var out []holdings.SectorRow
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, sectorRow(m))
		}
	}
	return out
}

func (d holdingsData) Sector(code string) holdings.SectorInfo {
	b := d.s.shared("/sector/"+url.PathEscape(code), 10*time.Minute)
	if b == nil {
		return holdings.SectorInfo{}
	}
	r := sectorRow(b)
	return holdings.SectorInfo{Name: r.Name, Verdict: r.Verdict, Flow: r.Flow, Heat: r.Heat, Source: r.Source, Tracked: r.Tracked}
}

func (d holdingsData) Calendar(names map[string]string) []holdings.CalRow {
	if len(names) == 0 {
		return nil
	}
	var parts []string
	for c, n := range names {
		parts = append(parts, c+":"+n)
	}
	sort.Strings(parts)
	items, _ := d.s.shared("/calendar?codes="+url.QueryEscape(strings.Join(parts, ",")), 30*time.Minute)["items"].([]any)
	var out []holdings.CalRow
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			date, _ := m["date"].(string)
			text, _ := m["text"].(string)
			tag, _ := m["tag"].(string)
			out = append(out, holdings.CalRow{Date: date, Text: text, Tag: tag})
		}
	}
	return out
}

func (d holdingsData) Resolve(query string) (string, string, bool) {
	code, name, err := d.s.resolveStock(query)
	return code, name, err == nil
}

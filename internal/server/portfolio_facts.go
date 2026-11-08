package server

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"go-harness/internal/holdings"
	"go-harness/internal/personal"
)

// The portfolio analyst reads a table of facts, not the raw data: every number in it is worked out here, each
// fact has an id, and the analyst may only cite them. The table is for a set of stocks, the user's own
// (holdings and wishes) or any two to six the user wants compared.

type pfFact struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

const maxCompare = 6

var codePattern = regexp.MustCompile(`^[0-9]{5,6}\.(SH|SZ|BJ|HK)$`)

// cleanCodes keeps valid, distinct codes in the order given.
func cleanCodes(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range in {
		c = strings.ToUpper(strings.TrimSpace(c))
		if codePattern.MatchString(c) && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

func pfClip(s string, n int) string {
	s = strings.TrimSpace(strings.NewReplacer("\n", " ", "**", "").Replace(s))
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// trendFact says where the price is: the last 20 and 60 days, the 20 and 60 day lines and the year's high.
func trendFact(name string, bars holdings.Series) string {
	n := len(bars)
	if n < 25 {
		return name + "：日线不够，看不了走势"
	}
	last := bars[n-1].Close
	ret := func(days int) string {
		if n <= days {
			return "—"
		}
		return fmt.Sprintf("%+.1f%%", (last/bars[n-1-days].Close-1)*100)
	}
	avg := func(days int) float64 {
		if n < days {
			return 0
		}
		sum := 0.0
		for _, b := range bars[n-days:] {
			sum += b.Close
		}
		return sum / float64(days)
	}
	high := 0.0
	for _, b := range bars {
		high = math.Max(high, b.Close)
	}
	text := fmt.Sprintf("%s：现价 %.2f；近 20 日 %s，近 60 日 %s", name, last, ret(20), ret(60))
	if m := avg(20); m > 0 {
		text += fmt.Sprintf("；相对 20 日线 %+.1f%%", (last/m-1)*100)
	}
	if m := avg(60); m > 0 {
		text += fmt.Sprintf("，相对 60 日线 %+.1f%%", (last/m-1)*100)
	}
	return text + fmt.Sprintf("；距近一年高点 %+.1f%%", (last/high-1)*100)
}

func corrWord(v float64) string {
	switch {
	case v >= 0.7:
		return "高度同向，跌的时候会一起跌"
	case v >= 0.4:
		return "有一定联动"
	default:
		return "联动不强"
	}
}

// portfolioFacts builds the table. mode is "check" (the user's holdings and wishes) or "compare" (the codes given).
func (s *Server) portfolioFacts(mode string, codes []string) ([]pfFact, []string) {
	if mode == "check" {
		held, wish := s.qualitySubjects()
		codes = append(append([]string{}, held...), wish...)
	}
	codes = cleanCodes(codes)
	if mode == "compare" && len(codes) > maxCompare {
		codes = codes[:maxCompare]
	}
	hctx := s.holdingsCtx()
	rowOf := map[string]personal.Row{}
	for _, r := range hctx.Rows() {
		rowOf[r.Code] = r
	}
	wishOf := map[string]personal.Wish{}
	for _, w := range s.personal.Wishes() {
		if w.Status == "open" {
			wishOf[w.Code] = w
		}
	}
	data := holdingsData{s}
	type stockFacts struct {
		lines []pfFact
		bars  holdings.Series
	}
	per := make([]stockFacts, len(codes))
	var wg sync.WaitGroup
	for i, code := range codes {
		i, code := i, code
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := fmt.Sprintf("s%d", i+1)
			name := s.stockName(code)
			add := func(key, text string) { per[i].lines = append(per[i].lines, pfFact{p + "." + key, text}) }
			who := fmt.Sprintf("%s（%s）", name, code)
			switch r, held := rowOf[code]; {
			case held:
				add("who", fmt.Sprintf("%s：你持有 %d 股，成本 %.2f，现价 %.2f，浮盈亏 %+.1f%%，占你持仓市值的 %.1f%%", who, r.Qty, r.Avg, r.Price, r.PnLPct*100, r.Weight*100))
			case wishOf[code].ID != 0:
				w := wishOf[code]
				note := ""
				if w.PlanPct > 0 {
					note = fmt.Sprintf("，计划仓位 %.0f%%", w.PlanPct)
				}
				add("who", fmt.Sprintf("%s：你想买，理由「%s」%s", who, pfClip(w.Reason, 40), note))
			default:
				add("who", who+"：不在你的持仓和想买里，只作对比")
			}
			bars := data.Closes(code)
			per[i].bars = bars
			add("trend", trendFact(name, bars))
			if b := s.readBundle(code); b != nil && b.Chief != nil {
				s.ensureLevels(b)
				verdict, _ := b.Chief["verdict"].(string)
				text, _ := b.Chief["text"].(string)
				stale := ""
				if why := s.staleReason(b, s.now()); why != "" {
					stale = "（" + why + "）"
				}
				add("verdict", fmt.Sprintf("%s：主分析师 %s 的裁决是「%s」%s。%s", name, b.GeneratedDay, verdict, stale, pfClip(text, 90)))
				if acts, ok := b.Chief["actions"].(map[string]any); ok {
					hold, _ := acts["hold"].(string)
					buy, _ := acts["buy"].(string)
					if hold != "" || buy != "" {
						add("actions", fmt.Sprintf("%s：主分析师的操作提醒——已持有：%s；想买：%s", name, orDash(hold), orDash(buy)))
					}
				}
				if l := levelOf(b.Levels, "down"); l != nil {
					add("down", name+"："+l.text("下方价（转弱价位）"))
				}
				if l := levelOf(b.Levels, "up"); l != nil {
					add("up", name+"："+l.text("上方价（突破价位）"))
				}
				var voices []string
				for _, a := range b.Analysts {
					if t, _ := a.Headline["text"].(string); t != "" {
						st, _ := a.Headline["status_text"].(string)
						voices = append(voices, fmt.Sprintf("%s %s：%s", a.Label, st, pfClip(t, 50)))
					}
				}
				if len(voices) > 0 {
					add("analysts", name+"：各分析师——"+strings.Join(voices, "；"))
				}
			} else {
				add("verdict", name+"：还没有主分析师的结论")
			}
			if sec := data.Sector(code); sec.Name != "" {
				add("sector", fmt.Sprintf("%s：所在板块 %s，资金和热度的状态「%s」", name, sec.Name, orDash(sec.Verdict)))
			}
			var q map[string]any
			if err := s.stockGetJSON("/api/v1/shared/quality/"+url.PathEscape(code), &q); err == nil && q["status"] == "ok" {
				add("quality", name+"："+qualityText(q))
			}
		}()
	}
	wg.Wait()
	var facts []pfFact
	for _, sf := range per {
		facts = append(facts, sf.lines...)
	}
	if mode == "compare" {
		for i := 0; i < len(codes); i++ {
			for j := i + 1; j < len(codes); j++ {
				if v := holdings.Corr(per[i].bars, per[j].bars, 60); v != nil {
					facts = append(facts, pfFact{fmt.Sprintf("c.%d.%d", i+1, j+1), fmt.Sprintf("%s 与 %s 近 60 日每日涨跌的相关系数 %.2f（%s）", s.stockName(codes[i]), s.stockName(codes[j]), *v, corrWord(*v))})
				}
			}
		}
	} else {
		facts = append(facts, s.checkupFacts()...)
	}
	return facts, codes
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func qualityText(q map[string]any) string {
	level := map[string]string{"red": "踩红线", "attention": "有注意项", "clean": "没发现问题", "unknown": "没数据"}[fmt.Sprint(q["level"])]
	parts := []string{"质地筛查 " + level}
	if flags, ok := q["flags"].([]any); ok {
		for _, f := range flags {
			if m, ok := f.(map[string]any); ok {
				parts = append(parts, fmt.Sprint(m["text"]))
			}
		}
	}
	if m, ok := q["metrics"].(map[string]any); ok {
		var nums []string
		if v, ok := m["roe"].(float64); ok {
			nums = append(nums, fmt.Sprintf("ROE %.1f%%", v*100))
		}
		if v, ok := m["debt"].(float64); ok {
			nums = append(nums, fmt.Sprintf("负债率 %.0f%%", v*100))
		}
		if v, ok := m["growth"].(float64); ok {
			nums = append(nums, fmt.Sprintf("营收同比 %+.0f%%", v*100))
		}
		if v, ok := m["cash"].(float64); ok {
			nums = append(nums, fmt.Sprintf("现金/利润 %.1f", v))
		}
		if len(nums) > 0 {
			parts = append(parts, strings.Join(nums, "、"))
		}
	}
	return strings.Join(parts, "；")
}

// checkupFacts are the portfolio-level facts from the check-up: where the money sits, what the rules flagged and
// what the worst cases cost.
func (s *Server) checkupFacts() []pfFact {
	raw, err := json.Marshal(s.holdingsCtx().TabCheck(false))
	if err != nil {
		return nil
	}
	var d map[string]any
	if json.Unmarshal(raw, &d) != nil || d["empty"] == true {
		return nil
	}
	var facts []pfFact
	add := func(id, text string) { facts = append(facts, pfFact{id, text}) }
	if st, ok := d["structure"].(map[string]any); ok {
		if secs, ok := st["sectors"].([]any); ok {
			for i, x := range secs {
				if m, ok := x.(map[string]any); ok && i < 5 {
					add(fmt.Sprintf("p.sector.%d", i+1), fmt.Sprintf("持仓里 %v 占 %.1f%%（%v）", m["name"], fnumOr(m["weight"])*100, joinAny(m["stocks"])))
				}
			}
		}
		if flags, ok := st["flags"].([]any); ok {
			for i, x := range flags {
				if m, ok := x.(map[string]any); ok && m["level"] == "注意" {
					add(fmt.Sprintf("p.flag.%d", i+1), fmt.Sprintf("体检提醒：%v——%v", m["title"], m["text"]))
				}
			}
		}
	}
	if st, ok := d["stress"].([]any); ok {
		for i, x := range st {
			if m, ok := x.(map[string]any); ok {
				add(fmt.Sprintf("p.stress.%d", i+1), fmt.Sprintf("最坏情形「%v」：%v，估计亏 %.0f 元，占持仓 %.1f%%", m["title"], m["text"], math.Abs(fnumOr(m["loss"])), math.Abs(fnumOr(m["pct"]))*100))
			}
		}
	}
	if f, ok := d["fit"].(map[string]any); ok {
		if lines, ok := f["lines"].([]any); ok {
			for i, x := range lines {
				if m, ok := x.(map[string]any); ok && i < 3 {
					add(fmt.Sprintf("p.fit.%d", i+1), fmt.Sprintf("和大盘的关系：%v（%v）", m["text"], m["level"]))
				}
			}
		}
	}
	if rs, ok := d["reasons"].([]any); ok {
		for i, x := range rs {
			if m, ok := x.(map[string]any); ok {
				add(fmt.Sprintf("p.reason.%d", i+1), fmt.Sprintf("%v 买入理由现在：%v——%v", m["name"], m["status_text"], m["line"]))
			}
		}
	}
	return facts
}

func fnumOr(v any) float64 {
	f, _ := v.(float64)
	return f
}

func joinAny(v any) string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		out = append(out, fmt.Sprint(x))
	}
	return strings.Join(out, "、")
}

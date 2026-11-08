package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-harness/internal/personal"
	"go-harness/internal/sched"
)

// A trade plan is what the verdict means for the two people who read it: someone who holds the stock and someone
// who wants to buy it. It is made by rules from the chief's verdict, its operation reminders and its two price
// lines: conditions ("if…then…"), no target price, no amount. It is made again when a newer conclusion arrives.

type planLine struct {
	Kind  string  `json:"kind"` // now | watch | trigger | giveup | size | review
	Text  string  `json:"text"`
	Side  string  `json:"side,omitempty"` // up | down for watch lines
	Price float64 `json:"price,omitempty"`
}

type planSection struct {
	Key   string     `json:"key"` // hold | buy
	Title string     `json:"title"`
	Lines []planLine `json:"lines"`
}

type tradePlan struct {
	Code        string        `json:"ts_code"`
	Name        string        `json:"name"`
	BundleID    int64         `json:"bundle_id"`
	Verdict     string        `json:"verdict"`
	Window      string        `json:"window"`
	Basis       string        `json:"basis"`
	GeneratedAt string        `json:"generated_at"`
	Held        bool          `json:"held"`
	Wish        bool          `json:"wish"`
	Sections    []planSection `json:"sections"`
	Note        string        `json:"note"`
}

type planLevel struct {
	Price                       float64
	Name, Condition, Consequent string
}

type planInput struct {
	Code, Name, Verdict, Day, Stamp string
	BundleID                        int64
	HoldNote, BuyNote               string
	Down, Up                        *planLevel
	Price                           float64
	Cost                            float64 // 0 when the user does not hold it
	Held                            bool
	WishPct                         float64
	Wish                            bool
}

func levelOf(levels map[string]any, side string) *planLevel {
	l, _ := levels[side].(map[string]any)
	price, ok := l["price"].(float64)
	if !ok || price <= 0 {
		return nil
	}
	name, _ := l["name"].(string)
	cond, _ := l["condition"].(string)
	cons, _ := l["consequence"].(string)
	return &planLevel{Price: price, Name: name, Condition: cond, Consequent: cons}
}

func (l *planLevel) text(label string) string {
	t := fmt.Sprintf("%s %.2f", label, l.Price)
	if l.Name != "" {
		t += "（" + l.Name + "）"
	}
	if l.Condition != "" {
		t += "：" + l.Condition
		if l.Consequent != "" {
			t += "，" + l.Consequent
		}
	}
	return t
}

// buildTradePlan is pure: the same input gives the same plan.
func buildTradePlan(in planInput) tradePlan {
	lean := leanOf(in.Verdict)
	hold := planSection{Key: "hold", Title: "已持有"}
	buy := planSection{Key: "buy", Title: "想买"}

	now := in.HoldNote
	if now == "" {
		now = map[string]string{
			"bearish": "现在不加仓；先看下面的转弱价守不守得住。",
			"bullish": "可以继续拿着，不追高加仓；下面的转弱价是复核的线。",
			"neutral": "先不动，等方向明确再说。",
		}[lean]
	}
	hold.Lines = append(hold.Lines, planLine{Kind: "now", Text: now})
	if in.Down != nil {
		text := in.Down.text("转弱价")
		if in.Cost > 0 {
			text += fmt.Sprintf("。到这个价时相对你的成本 %.2f 是 %+.1f%%", in.Cost, (in.Down.Price/in.Cost-1)*100)
		}
		hold.Lines = append(hold.Lines, planLine{Kind: "watch", Side: "down", Price: in.Down.Price, Text: text + "。到了先复核这轮判断，再决定要不要减，不要等到更低。"})
	}
	if in.Up != nil {
		hold.Lines = append(hold.Lines, planLine{Kind: "watch", Side: "up", Price: in.Up.Price, Text: in.Up.text("突破价") + "。站上说明判断在变强，但不是追高的理由。"})
	}

	buyNow := in.BuyNote
	if buyNow == "" {
		buyNow = map[string]string{
			"bearish": "现在先等，不急着介入。",
			"bullish": "可以分批试探，不一次买满。",
			"neutral": "先不动，等方向明确。",
		}[lean]
	}
	buy.Lines = append(buy.Lines, planLine{Kind: "now", Text: buyNow})
	switch {
	case lean == "bullish" && in.Down != nil:
		buy.Lines = append(buy.Lines, planLine{Kind: "trigger", Text: fmt.Sprintf("试探可以分几次，每次之间留出几个交易日看走势；收盘跌破 %.2f 就停止试探。", in.Down.Price)})
	case in.Up != nil:
		buy.Lines = append(buy.Lines, planLine{Kind: "trigger", Side: "up", Price: in.Up.Price, Text: fmt.Sprintf("出现这两条再考虑分批：收盘站上 %.2f，并且 AI 的判断转为偏多。只出现一条不动手。", in.Up.Price)})
	default:
		buy.Lines = append(buy.Lines, planLine{Kind: "trigger", Text: "等 AI 的判断转为偏多，再考虑分批。"})
	}
	if in.Down != nil {
		buy.Lines = append(buy.Lines, planLine{Kind: "giveup", Side: "down", Price: in.Down.Price, Text: fmt.Sprintf("收盘跌破 %.2f，说明这轮理由不成立：放弃这一轮，不要越跌越补。", in.Down.Price)})
	}
	if in.Wish && in.WishPct > 0 {
		buy.Lines = append(buy.Lines, planLine{Kind: "size", Text: fmt.Sprintf("仓位按你在「想买」里自己写的计划（占总资金 %.0f%%）走，这份计划不改变它。", in.WishPct)})
	}

	review := planLine{Kind: "review", Text: "5 个交易日后看这轮判断的结算；这期间每晚的夜间任务也会更新结论，计划会跟着重新生成。"}
	hold.Lines = append(hold.Lines, review)
	buy.Lines = append(buy.Lines, review)

	return tradePlan{
		Code: in.Code, Name: in.Name, BundleID: in.BundleID, Verdict: in.Verdict, Window: "5～20 个交易日",
		Basis:       fmt.Sprintf("%s 主分析师的裁决「%s」和结论里的两条价格线", in.Day, in.Verdict),
		GeneratedAt: in.Stamp, Held: in.Held, Wish: in.Wish, Sections: []planSection{hold, buy},
		Note: "条件式计划，由规则按本轮裁决和价格线生成：不含目标价和买卖金额，不是确定的买卖指令；条件变了做法就变。",
	}
}

// ---- keeping the latest plan per stock

type planStore struct {
	mu    sync.Mutex
	path  string
	plans map[string]tradePlan
}

func openPlans(path string) *planStore {
	p := &planStore{path: path, plans: map[string]tradePlan{}}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(raw, &p.plans)
		}
	}
	return p
}

func (p *planStore) get(code string) (tradePlan, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, ok := p.plans[code]
	return plan, ok
}

func (p *planStore) put(plan tradePlan) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.plans[plan.Code] = plan
	if p.path == "" {
		return
	}
	if raw, err := json.Marshal(p.plans); err == nil {
		tmp := p.path + ".tmp"
		if os.WriteFile(tmp, raw, 0o644) == nil {
			_ = os.Rename(tmp, p.path)
		}
	}
}

var planStores sync.Map // data dir -> *planStore

func (s *Server) tradePlans() *planStore {
	path := ""
	if strings.TrimSpace(s.cfg.DataDir) != "" {
		path = filepath.Join(s.cfg.DataDir, "client", "trade_plans.json")
	}
	if v, ok := planStores.Load(path); ok && path != "" {
		return v.(*planStore)
	}
	st := openPlans(path)
	if path != "" {
		v, _ := planStores.LoadOrStore(path, st)
		return v.(*planStore)
	}
	return st
}

// planInputFor reads what the plan is made from: the stock's latest conclusion and what the user holds or wants.
func (s *Server) planInputFor(code string) (planInput, bool) {
	b := s.readBundle(code)
	if b == nil || b.Chief == nil {
		return planInput{}, false
	}
	s.ensureLevels(b)
	verdict, _ := b.Chief["verdict"].(string)
	in := planInput{
		Code: code, Name: s.stockName(code), Verdict: verdict, BundleID: b.ID, Day: b.GeneratedDay,
		Stamp: s.now().In(sched.Zone).Format("2006-01-02 15:04"),
		Down:  levelOf(b.Levels, "down"), Up: levelOf(b.Levels, "up"),
	}
	if acts, ok := b.Chief["actions"].(map[string]any); ok {
		in.HoldNote, _ = acts["hold"].(string)
		in.BuyNote, _ = acts["buy"].(string)
	}
	if bars := barsOf(s.shared("/closes/"+url.PathEscape(code), 30*time.Minute)); len(bars) > 0 {
		in.Price = bars[len(bars)-1].Close
	}
	if pos := personal.Held(s.personal.Trades())[code]; pos != nil && pos.Qty > 0 {
		in.Held, in.Cost = true, pos.Avg
	}
	for _, w := range s.personal.Wishes() {
		if w.Code == code && w.Status == "open" {
			in.Wish, in.WishPct = true, w.PlanPct
		}
	}
	return in, true
}

// handleTradePlan: GET gives the saved plan and makes a new one when a newer conclusion has come in since;
// POST makes it again now.
func (s *Server) handleTradePlan(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !tsCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("股票代码无法识别: %s", code))
		return
	}
	in, ok := s.planInputFor(code)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"status": "empty", "error": "这只股票还没有主分析师的结论，等结论出来再生成计划"})
		return
	}
	store := s.tradePlans()
	saved, have := store.get(code)
	if r.Method == http.MethodPost || !have || saved.BundleID != in.BundleID || saved.Held != in.Held || saved.Wish != in.Wish {
		saved = buildTradePlan(in)
		store.put(saved)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "plan": saved})
}

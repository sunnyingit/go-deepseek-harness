// Package custom holds the tasks the user makes up: analyse chosen stocks on a schedule, ask a question of
// their own on a schedule, or scan the followed stocks for a condition. This file is the model, the checks on
// what the user typed, the file they are kept in and the rules that decide whether a stock matches.
package custom

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Dimensions are the dimension analysts that each look at one side; the chief weighs them.
var Dimensions = []string{"volume_flow", "market", "sector", "news", "finance", "fundamental"}

// AnalystLabel is how an analyst is named in the lists.
var AnalystLabel = map[string]string{"volume_flow": "资金流", "market": "大盘", "sector": "板块", "news": "新闻", "finance": "财报", "fundamental": "基本面", "chief": "主分析师"}

func inList(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Task kinds.
const (
	Analyze = "analyze" // make a new conclusion for each stock, written down as a prediction
	Ask     = "ask"     // ask the chief analyst a question about each stock; the answer goes to the inbox
	Scan    = "scan"    // check each stock against conditions; matches go to the inbox
)

// Task is one custom task.
type Task struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Kind      string   `json:"kind"`
	Scope     string   `json:"scope"` // all | hold | wish | watch | codes
	Codes     []string `json:"codes,omitempty"`
	At        string   `json:"at"`              // 14:30; empty when the task repeats every N minutes
	Every     int      `json:"every,omitempty"` // minutes between runs; 0 = once a day at At
	Days      string   `json:"days"`            // trading | any: for a daily task the days it runs, for an interval the hours it runs in (trading = market sessions only)
	Question  string   `json:"question,omitempty"`
	Analysts  []string `json:"analysts,omitempty"` // analyze: only these dimensions (empty = the whole round); ask: who is asked (empty = the chief)
	Rules     []Rule   `json:"rules,omitempty"`
	Match     string   `json:"match,omitempty"` // all | any
	Enabled   bool     `json:"enabled"`
	CreatedAt int64    `json:"created_at"`
}

// Rule is one condition of a scan.
type Rule struct {
	Kind  string  `json:"kind"`
	Value float64 `json:"value,omitempty"`
	Text  string  `json:"text,omitempty"` // sector_is: the sector verdict
}

// Kind describes one rule the user can pick.
type Kind struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`   // as it reads in the list: 日涨幅达到
	Unit    string `json:"unit"`    // what the number is: %, 日, 元; "" when there is none
	Default string `json:"default"` // a sensible number to start from
	Hint    string `json:"hint,omitempty"`
}

// Kinds is every rule the scan knows, in the order the form shows them.
var Kinds = []Kind{
	{"change_up", "日涨幅达到", "%", "5", "今天涨幅不低于这个数"},
	{"change_down", "日跌幅达到", "%", "5", "今天跌幅不低于这个数"},
	{"price_below", "现价低于", "元", "", "现价不高于这个价"},
	{"price_above", "现价高于", "元", "", "现价不低于这个价"},
	{"below_ma", "收盘跌破均线", "日", "20", "最近一个收盘价低于 N 日均线"},
	{"above_ma", "收盘站上均线", "日", "20", "最近一个收盘价高于 N 日均线"},
	{"new_high", "收盘创新高", "日", "60", "最近一个收盘价是近 N 日最高"},
	{"new_low", "收盘创新低", "日", "60", "最近一个收盘价是近 N 日最低"},
	{"flow_in_days", "大资金连续净流入", "日", "3", "大资金连续 N 天净流入"},
	{"flow_out_days", "大资金连续净流出", "日", "3", "大资金连续 N 天净流出"},
	{"ai_bullish", "AI 结论偏多", "", "", "主分析师最新结论是偏多"},
	{"ai_bearish", "AI 结论偏空", "", "", "主分析师最新结论是偏空"},
	{"sector_is", "板块结论是", "", "", "所在板块的结论等于所选"},
}

// SectorVerdicts are the words the sector conclusion can take.
var SectorVerdicts = []string{"三层同向", "有钱没热度", "只热不增", "在流出"}

var kindOf = func() map[string]Kind {
	m := map[string]Kind{}
	for _, k := range Kinds {
		m[k.Kind] = k
	}
	return m
}()

// InputError is something the user typed that cannot be used; the message is shown to them as it is.
type InputError string

func (e InputError) Error() string { return string(e) }

var clock = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
var codePattern = regexp.MustCompile(`^([0-9]{6}\.(SH|SZ|BJ)|[0-9]{5}\.HK)$`)

// MaxEvery is the longest gap between two runs of an interval task, in minutes.
const MaxEvery = 720

// MinEvery is the shortest gap: a scan only reads prices, the other two use the user's model on every stock.
func MinEvery(kind string) int {
	if kind == Scan {
		return 5
	}
	return 30
}

// Clean checks a task and tidies it up. It never keeps what the user cannot have meant.
func Clean(t Task) (Task, error) {
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
		return t, InputError("给任务起个名字")
	}
	if n := len([]rune(t.Title)); n > 30 {
		return t, InputError("名字最多 30 个字")
	}
	switch t.Kind {
	case Analyze, Ask, Scan:
	default:
		return t, InputError("任务类型不对")
	}
	if t.Days != "trading" && t.Days != "any" {
		t.Days = "trading"
	}
	if t.Every < 0 {
		return t, InputError("间隔不能是负数")
	}
	if t.Every == 0 {
		if !clock.MatchString(t.At) {
			return t, InputError("时间写成 14:30 这样")
		}
	} else {
		min := MinEvery(t.Kind)
		switch {
		case t.Every < min:
			return t, InputError(fmt.Sprintf("「%s」最短每 %d 分钟跑一次", kindText[t.Kind], min))
		case t.Every > MaxEvery:
			return t, InputError("间隔最长 12 小时")
		case t.Every%5 != 0:
			return t, InputError("间隔要是 5 分钟的整数倍")
		}
		t.At = ""
	}
	switch t.Scope {
	case "all", "hold", "wish", "watch":
		t.Codes = nil
	case "codes":
		var codes []string
		seen := map[string]bool{}
		for _, c := range t.Codes {
			c = strings.ToUpper(strings.TrimSpace(c))
			if !codePattern.MatchString(c) {
				return t, InputError("股票代码不对：" + c)
			}
			if !seen[c] {
				seen[c] = true
				codes = append(codes, c)
			}
		}
		if len(codes) == 0 {
			return t, InputError("至少选一只股票")
		}
		if len(codes) > 50 {
			return t, InputError("一个任务最多 50 只股票")
		}
		t.Codes = codes
	default:
		return t, InputError("范围不对")
	}
	{
		seen := map[string]bool{}
		var picked []string
		for _, a := range t.Analysts {
			a = strings.TrimSpace(a)
			if seen[a] || a == "" {
				continue
			}
			seen[a] = true
			if !inList(Dimensions, a) && !(t.Kind == Ask && a == "chief") {
				return t, InputError("没有这位分析师：" + a)
			}
			picked = append(picked, a)
		}
		switch t.Kind {
		case Scan:
			picked = nil
		case Analyze:
			if len(picked) == len(Dimensions) {
				picked = nil // all five is the whole round
			}
		case Ask:
			if len(picked) == 1 && picked[0] == "chief" {
				picked = nil
			}
		}
		t.Analysts = picked
	}
	switch t.Kind {
	case Ask:
		t.Question = strings.TrimSpace(t.Question)
		if t.Question == "" {
			return t, InputError("写下要问的问题")
		}
		if len([]rune(t.Question)) > 300 {
			return t, InputError("问题最多 300 个字")
		}
		t.Rules, t.Match = nil, ""
	case Scan:
		if len(t.Rules) == 0 {
			return t, InputError("至少设一个条件")
		}
		if len(t.Rules) > 6 {
			return t, InputError("一个任务最多 6 个条件")
		}
		var rules []Rule
		for _, r := range t.Rules {
			k, ok := kindOf[r.Kind]
			if !ok {
				return t, InputError("没有这个条件：" + r.Kind)
			}
			switch {
			case r.Kind == "sector_is":
				ok := false
				for _, v := range SectorVerdicts {
					ok = ok || v == r.Text
				}
				if !ok {
					return t, InputError("板块结论要从列表里选")
				}
				rules = append(rules, Rule{Kind: r.Kind, Text: r.Text})
			case k.Unit == "":
				rules = append(rules, Rule{Kind: r.Kind})
			default:
				if r.Value <= 0 || math.IsNaN(r.Value) || math.IsInf(r.Value, 0) {
					return t, InputError(k.Label + "要填一个大于 0 的数")
				}
				if k.Unit == "日" {
					if r.Value != math.Trunc(r.Value) || r.Value < 2 || r.Value > 250 {
						return t, InputError(k.Label + "的天数要是 2 到 250 之间的整数")
					}
				}
				if k.Unit == "%" && r.Value > 30 {
					return t, InputError(k.Label + "最大 30%")
				}
				rules = append(rules, Rule{Kind: r.Kind, Value: r.Value})
			}
		}
		t.Rules = rules
		if t.Match != "any" {
			t.Match = "all"
		}
		t.Question = ""
	default:
		t.Question, t.Rules, t.Match = "", nil, ""
	}
	return t, nil
}

// ---- describing a task in words

var kindText = map[string]string{Analyze: "定时分析", Ask: "定时提问", Scan: "条件扫描"}
var scopeText = map[string]string{"all": "我的全部标的", "hold": "我的持仓", "wish": "我想买的", "watch": "我的自选"}

func KindText(k string) string { return kindText[k] }

// ScopeText says which stocks a task covers.
func (t Task) ScopeText(names map[string]string) string {
	if t.Scope != "codes" {
		return scopeText[t.Scope]
	}
	var parts []string
	for _, c := range t.Codes {
		if n := names[c]; n != "" && n != c {
			parts = append(parts, n)
		} else {
			parts = append(parts, c)
		}
	}
	if len(parts) > 4 {
		return strings.Join(parts[:4], "、") + fmt.Sprintf(" 等 %d 只", len(parts))
	}
	return strings.Join(parts, "、")
}

// AnalystsText says what an analyze or ask task asks of whom.
func (t Task) AnalystsText() string {
	label := func() string {
		var parts []string
		for _, a := range t.Analysts {
			parts = append(parts, AnalystLabel[a])
		}
		return strings.Join(parts, "、")
	}
	switch {
	case t.Kind == Analyze && len(t.Analysts) == 0:
		return "完整一轮：五位分析师、看多看空、主分析师裁决"
	case t.Kind == Analyze:
		return "只分析：" + label()
	case t.Kind == Ask && len(t.Analysts) == 0:
		return "问：主分析师"
	case t.Kind == Ask:
		return "问：" + label()
	}
	return ""
}

func (t Task) ScheduleText() string {
	if t.Every > 0 {
		gap := fmt.Sprintf("%d 分钟", t.Every)
		if t.Every%60 == 0 {
			gap = fmt.Sprintf("%d 小时", t.Every/60)
		}
		if t.Days == "any" {
			return "每 " + gap + "（全天）"
		}
		return "交易时段每 " + gap
	}
	if t.Days == "any" {
		return "每天 " + t.At
	}
	return "每个交易日 " + t.At
}

// RuleText is a rule as a phrase: 收盘跌破 20 日均线.
func RuleText(r Rule) string {
	k := kindOf[r.Kind]
	switch {
	case r.Kind == "sector_is":
		return "板块结论是「" + r.Text + "」"
	case r.Kind == "below_ma" || r.Kind == "above_ma":
		return fmt.Sprintf("%s %s 日均线", strings.TrimSuffix(k.Label, "均线"), num(r.Value))
	case r.Kind == "new_high" || r.Kind == "new_low":
		return fmt.Sprintf("%s（近 %s 日）", k.Label, num(r.Value))
	case k.Unit == "":
		return k.Label
	}
	if k.Unit == "日" {
		return k.Label + " " + num(r.Value) + " 日"
	}
	return k.Label + " " + num(r.Value) + k.Unit
}

func (t Task) RulesText() string {
	var parts []string
	for _, r := range t.Rules {
		parts = append(parts, RuleText(r))
	}
	joiner := "，且 "
	if t.Match == "any" {
		joiner = "，或 "
	}
	return strings.Join(parts, joiner)
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// ---- the rules

// Bar is one day's close.
type Bar struct {
	Day   string
	Close float64
}

// Facts is what one stock looks like right now. A zero or empty field means it is not known.
type Facts struct {
	Price      float64
	ChangePct  float64 // today, percent
	HasChange  bool
	Closes     []Bar // oldest first
	FlowIn     int   // days of net inflow in a row
	FlowOut    int
	HasFlow    bool
	AILean     string // bullish | bearish | unclear | "" (no conclusion)
	SectorVerd string
}

// Eval says whether a rule holds, with a phrase that tells why. known is false when the data to decide is missing.
func Eval(r Rule, f Facts) (hit bool, why string, known bool) {
	ma := func(n int) (float64, bool) {
		if len(f.Closes) < n {
			return 0, false
		}
		sum := 0.0
		for _, b := range f.Closes[len(f.Closes)-n:] {
			sum += b.Close
		}
		return sum / float64(n), true
	}
	last := func() (float64, bool) {
		if len(f.Closes) == 0 {
			return 0, false
		}
		return f.Closes[len(f.Closes)-1].Close, true
	}
	n := int(r.Value)
	switch r.Kind {
	case "change_up":
		if !f.HasChange {
			return false, "", false
		}
		return f.ChangePct >= r.Value, fmt.Sprintf("今天涨 %.2f%%", f.ChangePct), true
	case "change_down":
		if !f.HasChange {
			return false, "", false
		}
		return -f.ChangePct >= r.Value, fmt.Sprintf("今天跌 %.2f%%", -f.ChangePct), true
	case "price_below":
		if f.Price <= 0 {
			return false, "", false
		}
		return f.Price <= r.Value, fmt.Sprintf("现价 %.2f，低于 %s", f.Price, num(r.Value)), true
	case "price_above":
		if f.Price <= 0 {
			return false, "", false
		}
		return f.Price >= r.Value, fmt.Sprintf("现价 %.2f，高于 %s", f.Price, num(r.Value)), true
	case "below_ma", "above_ma":
		l, ok1 := last()
		m, ok2 := ma(n)
		if !ok1 || !ok2 {
			return false, "", false
		}
		if r.Kind == "below_ma" {
			return l < m, fmt.Sprintf("收盘 %.2f，跌破 %d 日均线 %.2f", l, n, m), true
		}
		return l > m, fmt.Sprintf("收盘 %.2f，站上 %d 日均线 %.2f", l, n, m), true
	case "new_high", "new_low":
		if len(f.Closes) < n || n < 2 {
			return false, "", false
		}
		l, _ := last()
		window := f.Closes[len(f.Closes)-n:]
		best := window[0].Close
		for _, b := range window {
			if (r.Kind == "new_high" && b.Close > best) || (r.Kind == "new_low" && b.Close < best) {
				best = b.Close
			}
		}
		if r.Kind == "new_high" {
			return l >= best, fmt.Sprintf("收盘 %.2f，创近 %d 日新高", l, n), true
		}
		return l <= best, fmt.Sprintf("收盘 %.2f，创近 %d 日新低", l, n), true
	case "flow_in_days":
		if !f.HasFlow {
			return false, "", false
		}
		return f.FlowIn >= n, fmt.Sprintf("大资金已连续 %d 天净流入", f.FlowIn), true
	case "flow_out_days":
		if !f.HasFlow {
			return false, "", false
		}
		return f.FlowOut >= n, fmt.Sprintf("大资金已连续 %d 天净流出", f.FlowOut), true
	case "ai_bullish":
		if f.AILean == "" {
			return false, "", false
		}
		return f.AILean == "bullish", "AI 结论偏多", true
	case "ai_bearish":
		if f.AILean == "" {
			return false, "", false
		}
		return f.AILean == "bearish", "AI 结论偏空", true
	case "sector_is":
		if f.SectorVerd == "" {
			return false, "", false
		}
		return f.SectorVerd == r.Text, "板块结论「" + f.SectorVerd + "」", true
	}
	return false, "", false
}

// Match applies all the rules of a task. why lists what held; unknown lists what could not be decided.
func Match(t Task, f Facts) (hit bool, why []string, unknown int) {
	held := 0
	for _, r := range t.Rules {
		ok, text, known := Eval(r, f)
		if !known {
			unknown++
			continue
		}
		if ok {
			held++
			why = append(why, text)
		}
	}
	if t.Match == "any" {
		return held > 0, why, unknown
	}
	return held == len(t.Rules) && len(t.Rules) > 0, why, unknown
}

// NeedsCloses etc. say which data a task needs, so only that is fetched.
func (t Task) Needs(kind ...string) bool {
	for _, r := range t.Rules {
		for _, k := range kind {
			if r.Kind == k {
				return true
			}
		}
	}
	return false
}

// ---- the file

// Store keeps the tasks in one JSON file on this computer.
type Store struct {
	mu    sync.Mutex
	path  string
	now   func() time.Time
	Items []Task `json:"items"`
	Seq   int    `json:"seq"`
}

func Open(path string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	st := &Store{path: path, now: now}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			var loaded struct {
				Items []Task `json:"items"`
				Seq   int    `json:"seq"`
			}
			if json.Unmarshal(raw, &loaded) == nil {
				st.Items, st.Seq = loaded.Items, loaded.Seq
			}
		}
	}
	return st
}

func (st *Store) save() {
	if st.path == "" {
		return
	}
	raw, err := json.Marshal(struct {
		Items []Task `json:"items"`
		Seq   int    `json:"seq"`
	}{st.Items, st.Seq})
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(st.path), 0o755)
	tmp := st.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, st.path)
	}
}

// MaxTasks keeps the list readable and the schedule light.
const MaxTasks = 30

func (st *Store) Add(t Task) (Task, error) {
	t, err := Clean(t)
	if err != nil {
		return t, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.Items) >= MaxTasks {
		return t, InputError(fmt.Sprintf("最多 %d 个自定义任务，先删掉不用的", MaxTasks))
	}
	st.Seq++
	t.ID, t.CreatedAt, t.Enabled = "c"+strconv.Itoa(st.Seq), st.now().UnixMilli(), true
	st.Items = append(st.Items, t)
	st.save()
	return t, nil
}

// Update rewrites a task. Its id, creation time and on/off state stay.
func (st *Store) Update(id string, t Task) (Task, error) {
	t, err := Clean(t)
	if err != nil {
		return t, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.Items {
		if st.Items[i].ID == id {
			t.ID, t.CreatedAt, t.Enabled = id, st.Items[i].CreatedAt, st.Items[i].Enabled
			st.Items[i] = t
			st.save()
			return t, nil
		}
	}
	return t, InputError("没有这个任务")
}

func (st *Store) SetEnabled(id string, on bool) (Task, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.Items {
		if st.Items[i].ID == id {
			st.Items[i].Enabled = on
			st.save()
			return st.Items[i], true
		}
	}
	return Task{}, false
}

func (st *Store) Remove(id string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.Items {
		if st.Items[i].ID == id {
			st.Items = append(st.Items[:i], st.Items[i+1:]...)
			st.save()
			return true
		}
	}
	return false
}

func (st *Store) Get(id string) (Task, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, t := range st.Items {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// All returns copies in the order they were made.
func (st *Store) All() []Task {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := append([]Task(nil), st.Items...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].CreatedAt < out[b].CreatedAt })
	return out
}

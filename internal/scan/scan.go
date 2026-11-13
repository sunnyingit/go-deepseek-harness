// Package scan keeps the AI's predictions and checks them later. Every conclusion the client makes is
// written down once, with its direction, strength and the day it started from; five and twenty trading days
// on, the stock is compared with its sector, and the prediction counts as right, wrong or a draw. A written
// prediction's content is never edited; a newer conclusion for the same stock supersedes any still-open
// older one so that only the latest call stays in tracking.
package scan

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Horizons are the two checks of every prediction, in trading days.
var Horizons = []int{5, 20}

// Flat is the gap between stock and sector below which a prediction counts for neither side.
const Flat = 0.005

const (
	Bullish = "bullish"
	Bearish = "bearish"
	Unclear = "unclear"
)

// Analyst is what one analyst said when the prediction was made.
type Analyst struct {
	Dimension string `json:"dimension"`
	Name      string `json:"name"`
	Lean      string `json:"lean"` // bullish | bearish | neutral
	Text      string `json:"text,omitempty"`
}

// Result is how a prediction came out at one horizon. Once written it stays.
type Result struct {
	Day       string            `json:"day"` // the trading day it was settled on
	StockRet  float64           `json:"stock_ret"`
	SectorRet float64           `json:"sector_ret"`
	Excess    float64           `json:"excess"`
	Outcome   string            `json:"outcome"` // hit | miss | flat
	SettledAt int64             `json:"settled_at"`
	Analysts  map[string]string `json:"analysts,omitempty"` // dimension -> hit | miss | flat
	WhyDim    string            `json:"why_dim,omitempty"`
	Why       string            `json:"why,omitempty"`
}

// Prediction is one written-down call.
type Prediction struct {
	ID           string             `json:"id"`
	Code         string             `json:"ts_code"`
	Name         string             `json:"name"`
	Source       string             `json:"source"` // scan | diagnose
	Direction    string             `json:"direction"`
	Level        int                `json:"level"`
	Verdict      string             `json:"verdict"`
	Evidence     string             `json:"evidence"`
	MadeAt       int64              `json:"made_at"`
	BaseDay      string             `json:"base_day"` // the last close the prediction starts from
	BaseClose    float64            `json:"base_close,omitempty"`
	Sector       string             `json:"sector,omitempty"`
	Market       string             `json:"market,omitempty"` // 上涨市 | 震荡市 | 下跌市
	BundleID     int64              `json:"bundle_id,omitempty"`
	Analysts     []Analyst          `json:"analysts,omitempty"`
	Results      map[string]*Result `json:"results,omitempty"`
	SupersededAt int64              `json:"superseded_at,omitempty"` // newer call for the same stock replaced this one
}

func (p *Prediction) Result(h int) *Result { return p.Results[strconv.Itoa(h)] }

// Active reports whether this call still counts for tracking and settlement.
func (p *Prediction) Active() bool { return p != nil && p.SupersededAt == 0 }

// openHorizons is true while any 5/20-day check has not been written (unclear never settles, so it stays open until replaced).
func (p *Prediction) openHorizons() bool {
	if p.Direction == Unclear {
		return true
	}
	for _, h := range Horizons {
		if p.Result(h) == nil {
			return true
		}
	}
	return false
}

// Store is an append-only list kept in one JSON file.
type Store struct {
	mu    sync.Mutex
	path  string
	now   func() time.Time
	Items []*Prediction `json:"items"`
	Seq   int           `json:"seq"`
}

func Open(path string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	st := &Store{path: path, now: now}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			var loaded struct {
				Items []*Prediction `json:"items"`
				Seq   int           `json:"seq"`
			}
			if json.Unmarshal(raw, &loaded) == nil {
				st.Items, st.Seq = loaded.Items, loaded.Seq
			}
		}
	}
	st.collapseOpenLocked()
	return st
}

func (st *Store) save() {
	if st.path == "" {
		return
	}
	raw, err := json.Marshal(struct {
		Items []*Prediction `json:"items"`
		Seq   int           `json:"seq"`
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

// Add writes a prediction unless the same conclusion was already written for the same source.
// A new call for the same stock supersedes any older call that still has open horizons, so only the latest stays in tracking.
func (st *Store) Add(p Prediction) (*Prediction, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, x := range st.Items {
		if p.BundleID != 0 && x.BundleID == p.BundleID && x.Source == p.Source && x.Code == p.Code {
			return x, false
		}
	}
	st.Seq++
	p.ID = "p" + strconv.Itoa(st.Seq)
	if p.MadeAt == 0 {
		p.MadeAt = st.now().UnixMilli()
	}
	p.Results = map[string]*Result{}
	cp := p
	st.Items = append(st.Items, &cp)
	st.supersedeOlderLocked(cp.Code, cp.ID)
	st.save()
	return &cp, true
}

// supersedeOlderLocked keeps only keepID as the open call for code; older unfinished calls are marked superseded.
func (st *Store) supersedeOlderLocked(code, keepID string) {
	now := st.now().UnixMilli()
	for _, x := range st.Items {
		if x.Code != code || x.ID == keepID || x.SupersededAt != 0 || !x.openHorizons() {
			continue
		}
		x.SupersededAt = now
	}
}

// collapseOpenLocked is for stores loaded from disk: per stock, only the latest unfinished call stays active.
func (st *Store) collapseOpenLocked() {
	latest := map[string]*Prediction{}
	for _, p := range st.Items {
		if p.SupersededAt != 0 || !p.openHorizons() {
			continue
		}
		if cur, ok := latest[p.Code]; !ok || p.MadeAt >= cur.MadeAt || (p.MadeAt == cur.MadeAt && p.ID > cur.ID) {
			latest[p.Code] = p
		}
	}
	if len(latest) == 0 {
		return
	}
	now := st.now().UnixMilli()
	changed := false
	for _, p := range st.Items {
		keep := latest[p.Code]
		if keep == nil || p.ID == keep.ID || p.SupersededAt != 0 || !p.openHorizons() {
			continue
		}
		p.SupersededAt = now
		changed = true
	}
	if changed {
		st.save()
	}
}

// All returns copies, oldest first.
func (st *Store) All() []Prediction {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]Prediction, 0, len(st.Items))
	for _, p := range st.Items {
		cp := *p
		cp.Results = map[string]*Result{}
		for k, v := range p.Results {
			r := *v
			cp.Results[k] = &r
		}
		out = append(out, cp)
	}
	return out
}

// SetResult records an outcome once. A second attempt for the same horizon is refused.
func (st *Store) SetResult(id string, horizon int, r Result) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, p := range st.Items {
		if p.ID != id {
			continue
		}
		key := strconv.Itoa(horizon)
		if p.Results == nil {
			p.Results = map[string]*Result{}
		}
		if p.Results[key] != nil {
			return false
		}
		if r.SettledAt == 0 {
			r.SettledAt = st.now().UnixMilli()
		}
		p.Results[key] = &r
		st.save()
		return true
	}
	return false
}

// ---- prices

// Bar is one day's close.
type Bar struct {
	Day   string
	Close float64
}

func indexOn(bars []Bar, day string) int {
	found := -1
	for i, b := range bars {
		if b.Day <= day {
			found = i
		} else {
			break
		}
	}
	return found
}

func indexOf(bars []Bar, day string) int {
	for i, b := range bars {
		if b.Day == day {
			return i
		}
	}
	return -1
}

// Tracking is where a prediction stands against its sector.
type Tracking struct {
	Day       int // trading days elapsed, at most the horizon
	Horizon   int
	EndDay    string // the close it is measured at so far
	StockRet  float64
	SectorRet float64
	Excess    float64
	Done      bool // the horizon day has passed and both series cover it
	Have      bool // both series cover the start, so the numbers mean something
}

// Track measures a prediction from its base day to today (or to the horizon day), for the stock and for its sector.
func Track(p Prediction, stock, sector []Bar, horizon int) Tracking {
	t := Tracking{Horizon: horizon}
	si := indexOn(stock, p.BaseDay)
	if si < 0 || len(stock) == 0 {
		return t
	}
	baseDay := stock[si].Day
	qi := indexOf(sector, baseDay)
	if qi < 0 {
		return t
	}
	k := len(stock) - 1 - si
	if k > horizon {
		k = horizon
	}
	// the sector must reach the same day as the stock does, or the comparison would mix two dates
	for k > 0 {
		if indexOf(sector, stock[si+k].Day) >= 0 {
			break
		}
		k--
	}
	t.Have, t.Day = true, k
	if k == 0 {
		t.EndDay = baseDay
		return t
	}
	end := stock[si+k]
	ei := indexOf(sector, end.Day)
	t.EndDay = end.Day
	t.StockRet = end.Close/stock[si].Close - 1
	t.SectorRet = sector[ei].Close/sector[qi].Close - 1
	t.Excess = t.StockRet - t.SectorRet
	t.Done = k == horizon
	return t
}

// Judge says whether a call came out right: a bullish call is right when the stock beat its sector, a
// bearish call when it fell behind. A gap smaller than half a point counts for neither.
func Judge(direction string, excess float64) string {
	if direction != Bullish && direction != Bearish {
		return ""
	}
	if math.Abs(excess) < Flat {
		return "flat"
	}
	if (direction == Bullish) == (excess > 0) {
		return "hit"
	}
	return "miss"
}

// Settle builds the result of a finished tracking, with the verdict of each analyst on the same numbers and,
// for a wrong call, which analyst's reading it rested on.
func Settle(p Prediction, t Tracking, now time.Time) Result {
	r := Result{Day: t.EndDay, StockRet: t.StockRet, SectorRet: t.SectorRet, Excess: t.Excess, Outcome: Judge(p.Direction, t.Excess), SettledAt: now.UnixMilli()}
	r.Analysts = map[string]string{}
	for _, a := range p.Analysts {
		if o := Judge(leanDirection(a.Lean), t.Excess); o != "" {
			r.Analysts[a.Dimension] = o
		}
	}
	if r.Outcome == "miss" {
		r.WhyDim, r.Why = why(p, t)
	}
	return r
}

func leanDirection(lean string) string {
	switch lean {
	case "support", Bullish:
		return Bullish
	case "warn", Bearish:
		return Bearish
	}
	return ""
}

func word(direction string) string {
	if direction == Bullish {
		return "偏多"
	}
	return "偏空"
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

// why names the analysts who leaned the same way as the prediction and were wrong with it. It is built
// from what was written down at the time, not guessed afterwards.
func why(p Prediction, t Tracking) (string, string) {
	var agreed []Analyst
	for _, a := range p.Analysts {
		if leanDirection(a.Lean) == p.Direction {
			agreed = append(agreed, a)
		}
	}
	move := "跑赢"
	if t.Excess < 0 {
		move = "跑输"
	}
	tail := fmt.Sprintf("%d 天后本股%s板块 %.1f 个点", t.Day, move, math.Abs(t.Excess)*100)
	if len(agreed) == 0 {
		return "", fmt.Sprintf("这条预测%s，但当时没有哪位分析师明确支持这个方向；%s。", word(p.Direction), tail)
	}
	lead := agreed[0]
	name := strings.TrimSuffix(lead.Name, "分析师")
	text := fmt.Sprintf("%s分析师%s", name, word(p.Direction))
	if lead.Text != "" {
		text += "（" + clip(lead.Text, 40) + "）"
	}
	return lead.Dimension, text + "，但" + tail + "。"
}

// ---- statistics

// Rate is hits over decided calls (draws are left out).
type Rate struct {
	Hit  int     `json:"hit"`
	Miss int     `json:"miss"`
	Flat int     `json:"flat"`
	N    int     `json:"n"`
	Pct  float64 `json:"pct"` // hit / (hit+miss); -1 when there is nothing to say
}

func (r *Rate) add(outcome string) {
	switch outcome {
	case "hit":
		r.Hit++
	case "miss":
		r.Miss++
	case "flat":
		r.Flat++
	default:
		return
	}
	r.N++
}

func (r *Rate) finish() {
	if d := r.Hit + r.Miss; d > 0 {
		r.Pct = float64(r.Hit) / float64(d)
	} else {
		r.Pct = -1
	}
}

// Row is a breakdown line: a rate and how it compares with picking one direction at random.
type Row struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Rate  Rate    `json:"rate"`
	Edge  float64 `json:"edge"` // percentage points above the baseline; 0 when unknown
}

// Day is one settle day: how many calls came due and how they went.
type Day struct {
	Day     string  `json:"day"`
	Rate    Rate    `json:"rate"`
	Rolling float64 `json:"rolling"` // hit rate over the trailing window of settle days, -1 when empty
}

// Accuracy answers "is it any good?" for one horizon over the last days.
type Accuracy struct {
	Horizon  int     `json:"horizon"`
	Days     int     `json:"days"`
	Overall  Rate    `json:"overall"`
	Baseline float64 `json:"baseline"` // always picking the same direction, -1 when unknown
	Edge     float64 `json:"edge"`
	Unclear  float64 `json:"unclear_rate"`
	Made     int     `json:"made"`
	ByDay    []Day   `json:"by_day"`
	Source   []Row   `json:"by_source"`
	Level    []Row   `json:"by_level"`
	Dir      []Row   `json:"by_direction"`
	Market   []Row   `json:"by_market"`
	Analysts []Row   `json:"by_analyst"`
}

var levelLabel = map[int]string{1: "稍微", 2: "比较明显", 3: "明显"}
var sourceLabel = map[string]string{"scan": "定时扫描", "diagnose": "你自己诊断的"}

// Stats works out the accuracy of the settled predictions of the last days at one horizon.
func Stats(preds []Prediction, horizon int, days int, today time.Time) Accuracy {
	since := today.AddDate(0, 0, -days).Format("2006-01-02")
	key := strconv.Itoa(horizon)
	a := Accuracy{Horizon: horizon, Days: days, Baseline: -1}
	by := map[string]*Rate{}
	order := map[string][]string{}
	put := func(kind, k string, outcome string) {
		id := kind + "|" + k
		if by[id] == nil {
			by[id] = &Rate{}
			order[kind] = append(order[kind], k)
		}
		by[id].add(outcome)
	}
	perDay := map[string]*Rate{}
	var excesses []float64
	for _, p := range preds {
		if time.UnixMilli(p.MadeAt).Format("2006-01-02") >= since {
			a.Made++
			if p.Direction == Unclear {
				a.Unclear++
			}
		}
		r := p.Results[key]
		if r == nil || r.Day < since || r.Outcome == "" {
			continue
		}
		a.Overall.add(r.Outcome)
		if perDay[r.Day] == nil {
			perDay[r.Day] = &Rate{}
		}
		perDay[r.Day].add(r.Outcome)
		if r.Outcome != "flat" {
			excesses = append(excesses, r.Excess)
		}
		put("source", p.Source, r.Outcome)
		put("level", strconv.Itoa(p.Level), r.Outcome)
		put("dir", p.Direction, r.Outcome)
		if p.Market != "" {
			put("market", p.Market, r.Outcome)
		}
		for dim, o := range r.Analysts {
			put("analyst", dim, o)
		}
	}
	a.Overall.finish()
	if a.Made > 0 {
		a.Unclear /= float64(a.Made)
	}
	if n := len(excesses); n > 0 {
		up := 0
		for _, e := range excesses {
			if e > 0 {
				up++
			}
		}
		p := float64(up) / float64(n)
		a.Baseline = math.Max(p, 1-p)
		if a.Overall.Pct >= 0 {
			a.Edge = (a.Overall.Pct - a.Baseline) * 100
		}
	}
	edge := func(r *Rate) float64 {
		if r.Pct < 0 || a.Baseline < 0 {
			return 0
		}
		return (r.Pct - a.Baseline) * 100
	}
	rows := func(kind string, label func(string) string, sortKeys func([]string)) []Row {
		keys := order[kind]
		if sortKeys != nil {
			sortKeys(keys)
		}
		var out []Row
		for _, k := range keys {
			r := by[kind+"|"+k]
			r.finish()
			out = append(out, Row{Key: k, Label: label(k), Rate: *r, Edge: edge(r)})
		}
		return out
	}
	a.Source = rows("source", func(k string) string { return sourceLabel[k] }, func(k []string) { sort.Strings(k) })
	a.Level = rows("level", func(k string) string { n, _ := strconv.Atoi(k); return levelLabel[n] }, func(k []string) {
		sort.Slice(k, func(i, j int) bool { return k[i] > k[j] })
	})
	a.Dir = rows("dir", func(k string) string { return word(k) }, func(k []string) { sort.Strings(k) })
	a.Market = rows("market", func(k string) string { return k }, nil)
	a.Analysts = rows("analyst", func(k string) string { return k }, func(k []string) { sort.Strings(k) })
	var dayKeys []string
	for d := range perDay {
		dayKeys = append(dayKeys, d)
	}
	sort.Strings(dayKeys)
	window := 60
	for i, d := range dayKeys {
		r := *perDay[d]
		r.finish()
		var roll Rate
		for _, e := range dayKeys[max(0, i-window+1) : i+1] {
			x := perDay[e]
			roll.Hit += x.Hit
			roll.Miss += x.Miss
		}
		roll.finish()
		a.ByDay = append(a.ByDay, Day{Day: d, Rate: r, Rolling: roll.Pct})
	}
	return a
}

// Market maps the market page's conclusion to the three words the statistics use.
func Market(level string) string {
	switch level {
	case "顺风":
		return "上涨市"
	case "逆风":
		return "下跌市"
	case "中性":
		return "震荡市"
	}
	return ""
}

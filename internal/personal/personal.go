// Package personal keeps what the user wrote down: the watchlist, price alerts, trades and wishes.
// It lives on this machine only. Positions are never stored; they are worked out from the trades.
package personal

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Watch struct {
	Code    string  `json:"ts_code"`
	Name    string  `json:"name"`
	AddedAt int64   `json:"added_at"`
	Order   float64 `json:"sort_order"`
}

type Alert struct {
	ID        int     `json:"id"`
	Code      string  `json:"ts_code"`
	Side      string  `json:"side"` // up | down
	Price     float64 `json:"price"`
	Confirm   string  `json:"confirm"` // touch | close
	Name      string  `json:"base_name"`
	CreatedAt int64   `json:"created_at"`
}

// InputError is a mistake in what the user typed; the API answers it with 400 and the message as it is.
type InputError string

func (e InputError) Error() string { return string(e) }

// Item is one entry of a free-form list such as a trade's "invalid" conditions.
type Item = map[string]any

type Trade struct {
	ID        int     `json:"id"`
	Code      string  `json:"ts_code"`
	Name      string  `json:"name"`
	Side      string  `json:"side"` // buy | sell
	Price     float64 `json:"price"`
	Qty       int     `json:"qty"`
	Date      string  `json:"date"`
	Reason    string  `json:"reason,omitempty"`
	Invalid   []Item  `json:"invalid,omitempty"`
	Context   Item    `json:"context,omitempty"`
	Note      string  `json:"note,omitempty"`
	WishID    *int    `json:"wish_id,omitempty"`
	CreatedAt int64   `json:"created_at"`
}

type Wish struct {
	ID         int     `json:"id"`
	Code       string  `json:"ts_code"`
	Name       string  `json:"name"`
	Source     string  `json:"source"`
	Reason     string  `json:"reason"`
	Conditions []Item  `json:"conditions"`
	Match      string  `json:"match"`
	PlanPct    float64 `json:"plan_pct"`
	Invalid    []Item  `json:"invalid"`
	Status     string  `json:"status"` // open | bought | dropped
	CreatedAt  int64   `json:"created_at"`
	ClosedAt   int64   `json:"closed_at,omitempty"`
}

// Reason is why a stock was bought and what would prove the user wrong.
type Reason struct {
	Code      string `json:"ts_code"`
	Text      string `json:"text"`
	Invalid   []Item `json:"invalid"`
	WrittenAt string `json:"written_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type Rule struct {
	ID    int     `json:"id"`
	Code  string  `json:"ts_code"`
	Kind  string  `json:"kind"`
	Value float64 `json:"value"`
}

type Checkup struct {
	ID        int   `json:"id"`
	CreatedAt int64 `json:"created_at"`
	Data      Item  `json:"data"`
}

type data struct {
	Seq      int                        `json:"seq"`
	Watch    []Watch                    `json:"watch"`
	Alerts   []Alert                    `json:"alerts"`
	Trades   []Trade                    `json:"trades"`
	Wishes   []Wish                     `json:"wishes"`
	Reasons  map[string]Reason          `json:"reasons"`
	Rules    []Rule                     `json:"rules"`
	Checkups []Checkup                  `json:"checkups"`
	KV       map[string]json.RawMessage `json:"kv"`
}

// Store is one JSON file guarded by a mutex.
type Store struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
	d    data
}

func Open(path string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	st := &Store{path: path, now: now}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(raw, &st.d)
		}
	}
	return st
}

func (st *Store) save() {
	if st.path == "" {
		return
	}
	raw, err := json.Marshal(st.d)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(st.path), 0o755)
	tmp := st.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, st.path)
	}
}

func (st *Store) id() int { st.d.Seq++; return st.d.Seq }

func zone() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}

// ---- watchlist

func (st *Store) WatchList() []Watch {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := append([]Watch(nil), st.d.Watch...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].Order < out[b].Order })
	return out
}

// WatchAdd returns false when the stock was already on the list.
func (st *Store) WatchAdd(code, name string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, w := range st.d.Watch {
		if w.Code == code {
			return false
		}
	}
	now := st.now().UnixMilli()
	st.d.Watch = append(st.d.Watch, Watch{Code: code, Name: name, AddedAt: now, Order: float64(now)})
	st.save()
	return true
}

func (st *Store) WatchRemove(code string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, w := range st.d.Watch {
		if w.Code == code {
			st.d.Watch = append(st.d.Watch[:i], st.d.Watch[i+1:]...)
			st.save()
			return true
		}
	}
	return false
}

// ---- alerts

// AlertAdd stores one alert; the same stock, side and price returns the existing one.
func (st *Store) AlertAdd(code, side string, price float64, confirm, name string) (Alert, error) {
	if side != "up" && side != "down" {
		return Alert{}, errors.New("方向只能是 up 或 down")
	}
	if confirm != "touch" && confirm != "close" {
		confirm = "close"
	}
	if price <= 0 {
		return Alert{}, errors.New("价格要大于 0")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, a := range st.d.Alerts {
		if a.Code == code && a.Side == side && math.Abs(a.Price-price) < 0.005 {
			return a, nil
		}
	}
	a := Alert{ID: st.id(), Code: code, Side: side, Price: price, Confirm: confirm, Name: name, CreatedAt: st.now().UnixMilli()}
	st.d.Alerts = append(st.d.Alerts, a)
	st.save()
	return a, nil
}

func (st *Store) Alerts(code string) []Alert {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Alert
	for _, a := range st.d.Alerts {
		if code == "" || a.Code == code {
			out = append(out, a)
		}
	}
	return out
}

func (st *Store) AlertRemove(id int) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, a := range st.d.Alerts {
		if a.ID == id {
			st.d.Alerts = append(st.d.Alerts[:i], st.d.Alerts[i+1:]...)
			st.save()
			return true
		}
	}
	return false
}

// Subject is a stock the nightly work covers, with how much the user cares.
type Subject struct {
	Code string
	Tier string // hold | wish | other
}

// Subjects lists the stocks to analyse: holdings first, then wishes, then the rest of the watchlist.
func (st *Store) Subjects() []Subject {
	held := Held(st.Trades())
	seen := map[string]bool{}
	var out []Subject
	add := func(code, tier string) {
		if !seen[code] {
			seen[code] = true
			out = append(out, Subject{code, tier})
		}
	}
	codes := make([]string, 0, len(held))
	for code := range held {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		add(code, "hold")
	}
	for _, w := range st.Wishes() {
		add(w.Code, "wish")
	}
	for _, w := range st.WatchList() {
		add(w.Code, "other")
	}
	return out
}

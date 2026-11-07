package personal

import (
	"sort"
	"time"
)

// Position is what is held now, worked out from the trades with a moving weighted average cost.
type Position struct {
	Code     string
	Name     string
	Qty      int
	Avg      float64
	Since    string
	Realized float64
	Buys     []Trade // the buys of the current round, oldest first
}

// TradeNote annotates one trade: what it did to the position.
type TradeNote struct {
	Label     string // 建仓 加仓 减仓 清仓
	AvgBefore float64
	QtyBefore int
	PnL       float64 // sells only
	PnLPct    float64
	Entry     *Trade // sells only: the first buy of this round
	Since     string // sells only: when the round began
}

// Positions walks the trades in date order. It returns what is still held and a note per trade id.
func Positions(trades []Trade) (map[string]*Position, map[int]TradeNote) {
	sorted := append([]Trade(nil), trades...)
	sort.SliceStable(sorted, func(a, b int) bool {
		if sorted[a].Date != sorted[b].Date {
			return sorted[a].Date < sorted[b].Date
		}
		return sorted[a].ID < sorted[b].ID
	})
	book := map[string]*Position{}
	notes := map[int]TradeNote{}
	for _, t := range sorted {
		pos := book[t.Code]
		if pos == nil {
			pos = &Position{Code: t.Code}
			book[t.Code] = pos
		}
		pos.Name = t.Name
		note := TradeNote{AvgBefore: pos.Avg, QtyBefore: pos.Qty}
		if t.Side == "buy" {
			note.Label = "加仓"
			if pos.Qty == 0 {
				note.Label, pos.Since, pos.Buys = "建仓", t.Date, nil
			}
			pos.Buys = append(pos.Buys, t)
			total := pos.Avg*float64(pos.Qty) + t.Price*float64(t.Qty)
			pos.Qty += t.Qty
			pos.Avg = total / float64(pos.Qty)
		} else {
			pnl := (t.Price - pos.Avg) * float64(t.Qty)
			note.PnL = pnl
			if len(pos.Buys) > 0 {
				first := pos.Buys[0]
				note.Entry = &first
			}
			note.Since = pos.Since
			if pos.Avg != 0 {
				note.PnLPct = t.Price/pos.Avg - 1
			}
			pos.Qty -= t.Qty
			pos.Realized += pnl
			note.Label = "减仓"
			if pos.Qty <= 0 {
				pos.Qty, pos.Avg, note.Label = 0, 0, "清仓"
			}
		}
		notes[t.ID] = note
	}
	for code, pos := range book {
		if pos.Qty <= 0 {
			delete(book, code)
		}
	}
	return book, notes
}

// Held is Positions without the notes.
func Held(trades []Trade) map[string]*Position {
	held, _ := Positions(trades)
	return held
}

// Quote is the live price of a stock.
type Quote struct {
	Price     float64
	Change    float64
	ChangePct float64
}

// Row is one valued holding.
type Row struct {
	Code      string   `json:"ts_code"`
	Name      string   `json:"name"`
	Qty       int      `json:"qty"`
	Avg       float64  `json:"avg"`
	Price     float64  `json:"price"`
	Live      bool     `json:"live"`
	Change    float64  `json:"change"`
	ChangePct *float64 `json:"change_pct"`
	MV        float64  `json:"mv"`
	DayPnL    float64  `json:"day_pnl"`
	PnL       float64  `json:"pnl"`
	PnLPct    float64  `json:"pnl_pct"`
	Weight    float64  `json:"weight"`
	Since     string   `json:"since"`
	Days      *int     `json:"days"`
}

// Valuation values each holding. A missing quote values it at cost and says so; it is not a move today.
func Valuation(held map[string]*Position, quotes map[string]Quote, today time.Time) []Row {
	codes := make([]string, 0, len(held))
	for code := range held {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	var rows []Row
	total := 0.0
	for _, code := range codes {
		pos := held[code]
		q, live := quotes[code]
		live = live && q.Price > 0
		price, change := pos.Avg, 0.0
		var pct *float64
		if live {
			price, change = q.Price, q.Change
			p := q.ChangePct
			pct = &p
		}
		row := Row{Code: code, Name: pos.Name, Qty: pos.Qty, Avg: pos.Avg, Price: price, Live: live, Change: change, ChangePct: pct,
			MV: price * float64(pos.Qty), DayPnL: change * float64(pos.Qty), PnL: (price - pos.Avg) * float64(pos.Qty), Since: pos.Since}
		if pos.Avg != 0 {
			row.PnLPct = price/pos.Avg - 1
		}
		if since, err := time.ParseInLocation("2006-01-02", pos.Since, today.Location()); err == nil {
			d := int(today.Sub(since).Hours() / 24)
			row.Days = &d
		}
		total += row.MV
		rows = append(rows, row)
	}
	for i := range rows {
		if total != 0 {
			rows[i].Weight = rows[i].MV / total
		}
	}
	return rows
}

// Totals sums valued rows.
type Totals struct {
	MV     float64 `json:"mv"`
	DayPnL float64 `json:"day_pnl"`
	DayPct float64 `json:"day_pct"`
	PnL    float64 `json:"pnl"`
	PnLPct float64 `json:"pnl_pct"`
	Cost   float64 `json:"cost"`
}

func SumTotals(rows []Row) Totals {
	var t Totals
	for _, r := range rows {
		t.MV += r.MV
		t.DayPnL += r.DayPnL
		t.PnL += r.PnL
		t.Cost += r.Avg * float64(r.Qty)
	}
	if base := t.MV - t.DayPnL; base != 0 {
		t.DayPct = t.DayPnL / base
	}
	if t.Cost != 0 {
		t.PnLPct = t.PnL / t.Cost
	}
	return t
}

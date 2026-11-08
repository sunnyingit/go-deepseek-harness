package server

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go-harness/internal/scan"
)

const chartDays = 250 // about one trading year

// maSeries is the simple moving average of the closes over n days; days with fewer than n closes before
// them are nil, so the line starts where it can be computed.
func maSeries(bars []scan.Bar, n int) []*float64 {
	out := make([]*float64, len(bars))
	sum := 0.0
	for i, b := range bars {
		sum += b.Close
		if i >= n {
			sum -= bars[i-n].Close
		}
		if i >= n-1 {
			v := math.Round(sum/float64(n)*100) / 100
			out[i] = &v
		}
	}
	return out
}

// levelLine turns one of the conclusion's price lines into what the chart draws.
func levelLine(levels map[string]any, side, label string) map[string]any {
	l, _ := levels[side].(map[string]any)
	price, ok := l["price"].(float64)
	if !ok || price <= 0 {
		return nil
	}
	name, _ := l["name"].(string)
	verified, _ := l["verified"].(bool)
	return map[string]any{"price": price, "label": label, "base_name": name, "verified": verified}
}

// chartTrend is the last year of closes with the 7/20/60-day lines and the conclusion's upper and lower price
// lines, in the shape the diagnosis page's chart reads.
func chartTrend(bars []scan.Bar, levels map[string]any) map[string]any {
	if len(bars) > chartDays {
		bars = bars[len(bars)-chartDays:]
	}
	if len(bars) < 2 {
		return nil
	}
	ma7, ma20, ma60 := maSeries(bars, 7), maSeries(bars, 20), maSeries(bars, 60)
	points := make([]map[string]any, 0, len(bars))
	rows := make([]map[string]any, 0, len(bars))
	for i, b := range bars {
		rows = append(rows, map[string]any{"date": b.Day, "close": b.Close})
		p := map[string]any{"date": b.Day}
		for k, series := range map[string][]*float64{"ma7": ma7, "ma20": ma20, "ma60": ma60} {
			if series[i] != nil {
				p[k] = *series[i]
			}
		}
		points = append(points, p)
	}
	last := len(bars) - 1
	latest := func(s []*float64) any {
		if s[last] == nil {
			return nil
		}
		return *s[last]
	}
	var ticks []string
	for i := 0; i < 6; i++ {
		ticks = append(ticks, bars[i*last/5].Day)
	}
	lines := map[string]any{}
	if v := levelLine(levels, "up", "突破价位"); v != nil {
		lines["up"] = v
	}
	if v := levelLine(levels, "down", "转弱价位"); v != nil {
		lines["down"] = v
	}
	return map[string]any{
		"bars": rows, "ma": map[string]any{"points": points, "ma7": latest(ma7), "ma20": latest(ma20), "ma60": latest(ma60)},
		"lines": lines, "window": len(bars), "unit": "日线", "x_ticks": ticks,
	}
}

// handleStockChart answers GET /api/stocks/{code}/chart.
func (s *Server) handleStockChart(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !tsCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("股票代码无法识别: %s", code))
		return
	}
	bars := barsOf(s.shared("/closes/"+url.PathEscape(code), 30*time.Minute))
	var levels map[string]any
	if b := s.readBundle(code); b != nil {
		s.ensureLevels(b)
		levels = b.Levels
	}
	trend := chartTrend(bars, levels)
	if trend == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "empty", "error": "没有取到这只股票的日线"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "ts_code": code, "trend": trend})
}

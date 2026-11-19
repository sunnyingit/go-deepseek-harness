package server

import (
	"fmt"
	"sort"
	"time"

	"go-harness/internal/personal"
	"go-harness/internal/scan"
	"go-harness/internal/sched"
)

func shortDay(d string) string {
	if len(d) >= 10 {
		return d[5:]
	}
	return d
}

// nextTradingDay is the first trading day after now.
func (s *Server) nextTradingDay(now time.Time) time.Time {
	d := now.In(sched.Zone)
	for i := 1; i < 15; i++ {
		c := d.AddDate(0, 0, i)
		if s.cal.IsTradingDay(c) {
			return c
		}
	}
	return d.AddDate(0, 0, 1)
}

// nextRunText is the line under the page title: when the next nightly run is.
func (s *Server) nextRunText() string {
	text := "每个交易日 20:30 自动跑 · 盘后结算"
	if sp, ok := s.sch.Spec(nightlyID); ok {
		if at, ok := s.nextDaily(sp.Trigger, s.now()); ok {
			text += fmt.Sprintf(" · 下次 %s %s %s", at.Format("01-02"), weekdayName[at.Weekday()], at.Format("15:04"))
		}
	}
	return text
}

// scanDayPanel feeds the right-hand panel: how the day's settling went, what it means for you, and what comes tomorrow.
func (s *Server) scanDayPanel() map[string]any {
	preds := s.scan.All()
	today := sched.Day(s.now())
	latest := ""
	for _, p := range preds {
		for _, r := range p.Results {
			if r != nil && r.Outcome != "" && r.Day > latest {
				latest = r.Day
			}
		}
	}
	day := latest
	if day == "" {
		day = today
	}
	related := s.relatedCodes()
	held := personal.Held(s.personal.Trades())
	type row struct {
		p scan.Prediction
		h int
		r *scan.Result
	}
	var rows []row
	for _, p := range preds {
		if !related[p.Code] {
			continue
		}
		for _, h := range scan.Horizons {
			if r := p.Result(h); r != nil && r.Day == day && r.Outcome != "" {
				rows = append(rows, row{p, h, r})
			}
		}
	}
	out := map[string]any{"day": day, "is_today": day == today, "has": len(rows) > 0, "notify_text": "这份摘要结算后会推送到你设置的渠道。"}
	{
		tm := sched.Day(s.nextTradingDay(s.now()))
		cache := map[string]priced{}
		nowRight, mine, n := 0, 0, 0
		made := map[string]bool{}
		for _, p := range preds {
			if !p.Active() || p.Direction == scan.Unclear || !related[p.Code] {
				continue
			}
			for _, h := range scan.Horizons {
				// Calendar estimate first — avoid closes fanout for every open prediction.
				if p.Result(h) != nil || s.dueDay(p.BaseDay, h, nil) != tm {
					continue
				}
				n++
				made[p.BaseDay] = true
				mine++
				px, ok := cache[p.Code]
				if !ok {
					px = s.pricesOf(p.Code)
					cache[p.Code] = px
				}
				if t := scan.Track(p, px.stock, px.sector, h); t.Have && scan.Judge(p.Direction, t.Excess) == "hit" {
					nowRight++
				}
			}
		}
		var days []string
		for d := range made {
			days = append(days, d)
		}
		sort.Strings(days)
		madeText := ""
		if len(days) > 0 {
			madeText = shortDay(days[0])
			if len(days) > 1 {
				madeText += " 起"
			}
		}
		out["tomorrow"] = map[string]any{"day": tm, "count": n, "made_text": madeText, "now_right": nowRight, "mine": mine}
	}
	if len(rows) == 0 {
		return out
	}
	var rate scan.Rate
	var squares []string
	var mineRows []map[string]any
	reasons := map[string]int{}
	horizons := map[int]bool{}
	mades := map[string]bool{}
	dueMine := map[string]bool{}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].r.Outcome < rows[b].r.Outcome })
	for _, x := range rows {
		horizons[x.h] = true
		mades[x.p.BaseDay] = true
		switch x.r.Outcome {
		case "hit":
			rate.Hit++
		case "miss":
			rate.Miss++
		default:
			rate.Flat++
		}
		rate.N++
		squares = append(squares, x.r.Outcome)
		if x.r.Outcome == "miss" {
			label := "没有明确依据"
			if x.r.WhyDim != "" {
				label = dimLabel[x.r.WhyDim] + "分析师的判断没跟上"
			}
			reasons[label]++
		}
		if related[x.p.Code] {
			dueMine[x.p.Code] = true
			mineRows = append(mineRows, map[string]any{"name": x.p.Name, "ts_code": x.p.Code, "source_text": map[string]string{"scan": "扫描", "diagnose": "你诊断"}[x.p.Source], "direction": x.p.Direction,
				"direction_text": word(x.p.Direction), "excess": x.r.Excess, "outcome": x.r.Outcome, "horizon": x.h})
		}
	}
	if d := rate.Hit + rate.Miss; d > 0 {
		rate.Pct = float64(rate.Hit) / float64(d)
	} else {
		rate.Pct = -1
	}
	acc := scan.Stats(s.ownedPredictions(), 5, 60, s.now())
	var rs []map[string]any
	for label, n := range reasons {
		rs = append(rs, map[string]any{"label": label, "count": n})
	}
	sort.SliceStable(rs, func(a, b int) bool {
		if rs[a]["count"].(int) != rs[b]["count"].(int) {
			return rs[a]["count"].(int) > rs[b]["count"].(int)
		}
		return rs[a]["label"].(string) < rs[b]["label"].(string)
	})
	otherHeld := 0
	for code := range held {
		if !dueMine[code] {
			otherHeld++
		}
	}
	var hs, ms []string
	for h := range horizons {
		hs = append(hs, fmt.Sprint(h))
	}
	sort.Strings(hs)
	for m := range mades {
		ms = append(ms, m)
	}
	sort.Strings(ms)
	label := ""
	if len(ms) > 0 {
		label = shortDay(ms[0]) + " 发出的"
		if len(ms) > 1 {
			label = shortDay(ms[0]) + " 起发出的"
		}
		for i, h := range hs {
			if i == 0 {
				label += " "
			} else {
				label += " 和 "
			}
			label += h
		}
		label += " 天预测"
	}
	out["label"], out["rate"], out["baseline"], out["squares"] = label, rate, acc.Baseline, squares
	out["mine"], out["held_other"], out["wrong_reasons"], out["wrong"] = mineRows, otherHeld, rs, rate.Miss
	return out
}

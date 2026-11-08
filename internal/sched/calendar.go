package sched

import (
	"encoding/json"
	"os"
	"time"
)

// Zone is China Standard Time, the clock every A-share schedule is written in.
var Zone = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}()

// Calendar knows which days the market is open: weekdays minus the holidays listed in a JSON file
// ({"holidays": ["2026-10-01", ...]}). The file is optional.
type Calendar struct {
	holidays map[string]bool
}

func LoadCalendar(path string) *Calendar {
	cal := &Calendar{holidays: map[string]bool{}}
	if path == "" {
		return cal
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cal
	}
	var doc struct {
		Holidays []string `json:"holidays"`
	}
	if json.Unmarshal(raw, &doc) == nil {
		for _, day := range doc.Holidays {
			cal.holidays[day] = true
		}
	}
	return cal
}

func (c *Calendar) IsTradingDay(t time.Time) bool {
	t = t.In(Zone)
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}
	return !c.holidays[Day(t)]
}

// Day formats the calendar day in China time.
func Day(t time.Time) string { return t.In(Zone).Format("2006-01-02") }

// at returns the instant of "HH:MM" on the given day (China time).
func at(day time.Time, hhmm string) time.Time {
	t, err := time.ParseInLocation("15:04", hhmm, Zone)
	if err != nil {
		return day
	}
	d := day.In(Zone)
	return time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, Zone)
}

// InSession reports whether the continuous-trading sessions are open at t (09:30-11:30, 13:00-15:00).
func InSession(t time.Time) bool {
	t = t.In(Zone)
	m := t.Hour()*60 + t.Minute()
	return (m >= 9*60+30 && m <= 11*60+30) || (m >= 13*60 && m <= 15*60)
}

// LastClose is the trading day whose close is the latest one that has already happened: today after
// 15:00 on a trading day, otherwise the previous trading day.
func (c *Calendar) LastClose(now time.Time) time.Time {
	d := now.In(Zone)
	if c.IsTradingDay(d) && d.Hour() >= 15 {
		return d
	}
	for i := 1; i < 20; i++ {
		prev := d.AddDate(0, 0, -i)
		if c.IsTradingDay(prev) {
			return prev
		}
	}
	return d
}

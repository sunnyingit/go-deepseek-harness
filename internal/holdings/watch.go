package holdings

import (
	"fmt"

	"go-harness/internal/personal"
)

// ReasonState is where a held stock's reason stands.
type ReasonState struct {
	Code, Name, Status, StatusText, Line string
}

// ReasonStates lists every holding's reason check; the caller compares it with the last time.
func (c *Ctx) ReasonStates() []ReasonState {
	var out []ReasonState
	for _, r := range c.Rows() {
		rs := c.Reason(r.Code)
		out = append(out, ReasonState{r.Code, r.Name, str(rs["status"]), str(rs["status_text"]), str(rs["line"])})
	}
	return out
}

// WishReady is a wish whose conditions are all met.
type WishReady struct {
	Wish personal.Wish
	Met  []string
}

// WishesReady lists the open wishes whose conditions have come true.
func (c *Ctx) WishesReady() []WishReady {
	var out []WishReady
	for _, w := range c.Wishes() {
		state := WishState(w, c.wishFacts(w))
		if state["done"] != true {
			continue
		}
		var met []string
		for _, cond := range state["conditions"].([]Map) {
			if cond["met"] == true {
				met = append(met, fmt.Sprint(cond["label"]))
			}
		}
		out = append(out, WishReady{w, met})
	}
	return out
}

// Settled is a past call (a buy, or a change of the AI's mind) that has been checked five days on.
type Settled struct {
	Code, Name, Date, Text string
	Hit                    bool
	Ret                    float64
}

// SettledEntries lists the calls on held and wished stocks that were settled in the last days.
func (c *Ctx) SettledEntries(days int) []Settled {
	cut := c.Today.AddDate(0, 0, -days).Format("2006-01-02")
	var out []Settled
	for _, code := range c.sortedNames() {
		for _, e := range c.entries(code) {
			o := e["outcome"].(Map)
			if st := o["state"]; (st == "hit" || st == "miss") && e["date"].(string) >= cut {
				ret, _ := o["ret"].(float64)
				hit, _ := o["hit"].(bool)
				out = append(out, Settled{code, c.Names()[code], e["date"].(string), e["text"].(string), hit, ret})
			}
		}
	}
	return out
}

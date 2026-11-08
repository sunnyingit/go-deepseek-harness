package server

import "strings"

// chiefActions is the chief's reminder of what to do next: one line for someone who already holds the stock,
// one for someone who wants to buy it. Both are conditions ("now…; if… then…"), never orders.
type chiefActions struct {
	Hold string `json:"hold"`
	Buy  string `json:"buy"`
}

const maxActionRunes = 120

// actionBanned are the words an operation reminder must not use: a target price, an all-in or all-out order,
// or a promise. A line that has one is dropped (the verdict itself is kept) rather than shown.
var actionBanned = []string{"目标价", "满仓", "全仓", "清仓", "梭哈", "稳赚", "必涨", "必跌", "保证"}

func cleanAction(text string) string {
	text = speakArgIDs(strings.TrimSpace(text))
	if text == "" || forbiddenHit(text, actionBanned) {
		return ""
	}
	if r := []rune(text); len(r) > maxActionRunes {
		text = strings.TrimRight(string(r[:maxActionRunes]), "，、；, ") + "…"
	}
	return text
}

func (a chiefActions) clean() chiefActions {
	return chiefActions{Hold: cleanAction(a.Hold), Buy: cleanAction(a.Buy)}
}

// payload is what the page and the bundle carry; a side with no line is left out.
func (a chiefActions) payload() map[string]any {
	out := map[string]any{}
	if a.Hold != "" {
		out["hold"] = a.Hold
	}
	if a.Buy != "" {
		out["buy"] = a.Buy
	}
	return out
}

package server

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// looseSpeak is the last try at an analyst's opening: the object is valid JSON but a field has a shape the strict
// reader did not expect (a fact id as a bare string or a number, suggested questions as plain strings instead of
// {"text": …}, a number where a word was meant). Take what can be read and keep going rather than fail the opening.
func looseSpeak(text string) (analystSpeak, bool) {
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(strings.NewReader(text[i:])).Decode(&raw); err != nil {
			continue
		}
		str := func(key string) string {
			var s string
			if json.Unmarshal(raw[key], &s) == nil {
				return s
			}
			return ""
		}
		sp := analystSpeak{Text: str("text"), Status: str("status"), StatusText: str("status_text"), Stance: str("stance"), Strength: str("strength"), Summary: str("summary")}
		if v, ok := raw["fact_ids"]; ok {
			sp.FactIDs = looseStrings(v)
		}
		if v, ok := raw["suggested_questions"]; ok {
			sp.SuggestedQuestions = looseQuestions(v)
		}
		normalizeSpeak(&sp)
		if looksLikeSpeak(sp) {
			return sp, true
		}
	}
	return analystSpeak{}, false
}

// looseStrings reads a list of ids written as strings or numbers, or one id written alone.
func looseStrings(v json.RawMessage) []string {
	out := []string{}
	var one any
	if json.Unmarshal(v, &one) != nil {
		return out
	}
	add := func(x any) {
		switch t := x.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				out = append(out, s)
			}
		case float64:
			out = append(out, fmt.Sprint(t))
		}
	}
	if list, ok := one.([]any); ok {
		for _, x := range list {
			add(x)
		}
	} else {
		add(one)
	}
	return out
}

// looseQuestions reads suggested questions written as objects ({"text": …}) or as plain strings.
func looseQuestions(v json.RawMessage) []map[string]any {
	var list []any
	if json.Unmarshal(v, &list) != nil {
		return nil
	}
	var out []map[string]any
	for _, x := range list {
		switch t := x.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				out = append(out, map[string]any{"text": s})
			}
		case map[string]any:
			out = append(out, t)
		}
	}
	return out
}

// logUnparsedSpeak keeps what the analyst actually wrote when its opening could not be used, so the next failure
// can be looked at instead of guessed at.
func logUnparsedSpeak(agent, code, raw string) {
	r := []rune(strings.TrimSpace(raw))
	if len(r) > 800 {
		r = append(r[:800], '…')
	}
	log.Printf("analyst %s on %s: opening could not be read as the JSON conclusion; it wrote: %q", agent, code, string(r))
}

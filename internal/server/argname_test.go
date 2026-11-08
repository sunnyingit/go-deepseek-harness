package server

import (
	"strings"
	"testing"
)

func TestTheChiefsProseNeverShowsArgumentIds(t *testing.T) {
	got := speakArgIDs("| bull.1 单季归母 +325.5% | 看空论据 bear.12 与 bull.1 | 不动 sub.bull.1x |")
	want := "| 看多分析师的第 1 条 单季归母 +325.5% | 看空论据 看空分析师的第 12 条 与 看多分析师的第 1 条 | 不动 sub.bull.1x |"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	file := &debateFile{
		Bull: []debatePoint{{ID: "bull.1"}},
		Bear: []debatePoint{{ID: "bear.1"}},
	}
	raw := `{"verdict":"偏空","text":"依据 bear.1，不采信 bull.1","adopted":["bear.1"],"rejected":[{"id":"bull.1","reason":"bull.1 只是弱旁证"}],"however":"bull.1 仍存在","if_wrong":"若 bear.1 的条件消失"}`
	d, err := parseChief(raw, file, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{d.Text, d.Rejected[0].Reason, d.However, d.IfWrong} {
		if strings.Contains(text, "bull.") || strings.Contains(text, "bear.") {
			t.Fatalf("an id is still in the prose: %q", text)
		}
	}
	if d.Adopted[0] != "bear.1" || d.Rejected[0].ID != "bull.1" {
		t.Fatalf("the id fields were changed: %+v %+v", d.Adopted, d.Rejected)
	}
}

func TestTheChiefsOperationRemindersAreKeptShortConditionalAndFreeOfOrders(t *testing.T) {
	file := &debateFile{Bull: []debatePoint{{ID: "bull.1"}}, Bear: []debatePoint{{ID: "bear.1"}}}
	raw := `{"verdict":"偏空","text":"偏空","adopted":["bear.1"],"rejected":[],"however":"","if_wrong":"",
		"actions":{"hold":"现在不加仓；如果板块站上 20 日线的成分回到过半，再考虑拿住。依据 bear.1","buy":"现在先等，目标价 50 元再买"}}`
	d, err := parseChief(raw, file, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Actions.Hold != "现在不加仓；如果板块站上 20 日线的成分回到过半，再考虑拿住。依据 看空分析师的第 1 条" {
		t.Fatalf("hold = %q", d.Actions.Hold)
	}
	if d.Actions.Buy != "" {
		t.Fatalf("a line with a target price was kept: %q", d.Actions.Buy)
	}
	if got := d.Actions.payload(); len(got) != 1 || got["hold"] == nil {
		t.Fatalf("payload = %v", got)
	}
	// no actions at all is fine: the verdict stands and the page shows nothing
	d, err = parseChief(`{"verdict":"不明","text":"不明","adopted":[],"rejected":[]}`, file, nil)
	if err != nil || len(d.Actions.payload()) != 0 {
		t.Fatalf("err=%v actions=%v", err, d.Actions.payload())
	}
	long := strings.Repeat("先观望，", 60)
	if got := cleanAction(long); len([]rune(got)) > maxActionRunes+1 {
		t.Fatalf("not cut: %d", len([]rune(got)))
	}
}

func TestHoldingsSeeTheChiefsOperationReminders(t *testing.T) {
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00"))
	s.writeBundle(&conclusionBundle{
		ID: 1, Code: "600036.SH", Source: "agent", GeneratedDay: "2026-11-09",
		Chief: chiefPayload(agentConfig{ID: "chief", Name: "主分析师"}, &chiefDecision{Verdict: "偏空", Text: "偏空", Actions: chiefActions{Hold: "现在不加仓；如果板块转强再拿", Buy: "先等"}}, &debateFile{}),
	})
	ai := (holdingsData{s}).AI("600036.SH")
	if !ai.Has || ai.Hold != "现在不加仓；如果板块转强再拿" || ai.Buy != "先等" {
		t.Fatalf("ai = %+v", ai)
	}
}

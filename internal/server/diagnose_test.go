package server

import "testing"

func TestRoundFreshNeedsEveryAnalystOnTheSameDay(t *testing.T) {
	s := &Server{}
	day := "2026-11-07"
	for _, id := range diagnosisOrder {
		line := groupLine{Agent: id, Kind: "analysis", Text: "说过", StatusText: "偏空", Day: day}
		s.appendGroup("600000.SH", line)
	}
	if !s.roundFresh("600000.SH", day) {
		t.Fatal("完整的一轮应该可以直接复用")
	}
	if s.roundFresh("600000.SH", "2026-11-06") {
		t.Fatal("不是这一天的裁决不能复用")
	}
	s.appendGroup("600001.SH", groupLine{Agent: "bull", Kind: "analysis", Text: "旧的看多", Day: "2026-11-06"})
	s.appendGroup("600001.SH", groupLine{Agent: "bear", Kind: "analysis", Text: "旧的看空", Day: "2026-11-06"})
	s.appendGroup("600001.SH", groupLine{Agent: "chief", Kind: "analysis", Text: "旧裁决", StatusText: "偏多", Day: "2026-11-06"})
	for _, id := range diagnosisOrder[:5] {
		s.appendGroup("600001.SH", groupLine{Agent: id, Kind: "analysis", Text: "今天说的", StatusText: "偏空", Day: "2026-11-07"})
	}
	s.appendGroup("600001.SH", groupLine{Agent: "chief", Kind: "analysis", Text: "今天的裁决", StatusText: "偏空", Day: "2026-11-07"})
	if s.roundFresh("600001.SH", "2026-11-07") {
		t.Fatal("看多看空还停在上一轮时，不能当成今天已经诊完")
	}
}

func TestVerdictHistoryKeepsTheLastChiefOfEachDay(t *testing.T) {
	s := &Server{}
	s.appendGroup("600000.SH", groupLine{Agent: "chief", Kind: "analysis", Text: "先看多", StatusText: "偏多", Day: "2026-11-06"})
	s.appendGroup("600000.SH", groupLine{Agent: "chief", Kind: "analysis", Text: "改看空", StatusText: "偏空", Day: "2026-11-06"})
	s.appendGroup("600000.SH", groupLine{Agent: "chief", Kind: "analysis", Text: "今天不明", StatusText: "不明", Day: "2026-11-07"})
	history := s.verdictHistory("600000.SH")
	if len(history) != 2 {
		t.Fatalf("got %#v", history)
	}
	if history[0]["lean"] != "bearish" || history[1]["lean"] != "neutral" {
		t.Fatalf("got %#v", history)
	}
}

func TestChiefDetailReadsTheSavedDebate(t *testing.T) {
	s := &Server{}
	s.updateDebate("600000.SH", func(file *debateFile) {
		file.Bull = []debatePoint{{ID: "bull.1", Text: "钱在进", Dimension: "volume_flow", Weight: 2}}
		file.Bear = []debatePoint{{ID: "bear.1", Text: "板块弱", Dimension: "sector", Weight: 3}}
		file.Chief = &chiefDecision{Verdict: "偏空", Text: "板块拖着", Adopted: []string{"bear.1"}, Rejected: []debateReject{{ID: "bull.1", Reason: "证据不足"}}, IfWrong: "板块转强"}
	})
	s.appendGroup("600000.SH", groupLine{Agent: "volume_flow", Kind: "analysis", Text: "资金偏多", StatusText: "偏多", Day: "2026-11-07"})
	s.appendGroup("600000.SH", groupLine{Agent: "chief", Kind: "analysis", Text: "板块拖着", StatusText: "偏空", Day: "2026-11-07"})
	body := s.chiefDetail("600000.SH")
	direction := body["direction"].(map[string]any)
	if direction["verdict"] != "偏空" || direction["text"] != "板块拖着" {
		t.Fatalf("direction %#v", direction)
	}
	points := body["points"].(map[string]any)
	bull := points["bull"].(map[string]any)["points"].([]map[string]any)
	bear := points["bear"].(map[string]any)["points"].([]map[string]any)
	if bull[0]["state"] != "rejected" || bear[0]["state"] != "adopted" {
		t.Fatalf("bull %#v bear %#v", bull, bear)
	}
}

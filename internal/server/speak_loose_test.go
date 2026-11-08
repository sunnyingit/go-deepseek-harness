package server

import "testing"

func TestAnOpeningWithOddlyShapedFieldsIsStillRead(t *testing.T) {
	// suggested questions as plain strings, fact ids as numbers and a bare string, a number for strength
	cases := map[string]string{
		"plain-string questions": `{"stance":"偏空","strength":"弱","text":"近 3 日没有新的公司事件","fact_ids":["ev.1"],"suggested_questions":["热搜里有没有相关的？","今天有没有公告？"]}`,
		"numeric fact ids":       `{"stance":"中性","strength":"弱","text":"近 3 日没有取到","fact_ids":[1,2],"suggested_questions":[]}`,
		"one bare fact id":       `{"stance":"中性","strength":"弱","text":"x","fact_ids":"ev.1"}`,
		"prose around it":        "好的，结论如下：\n{\"stance\":\"偏多\",\"strength\":\"强\",\"text\":\"订单公告\",\"fact_ids\":[\"ev.2\"],\"suggested_questions\":[]}\n以上。",
	}
	for name, raw := range cases {
		sp, err := parseSpeak(raw)
		if err != nil || sp.Text == "" || sp.Stance == "" || sp.FactIDs == nil {
			t.Fatalf("%s: err=%v speak=%+v", name, err, sp)
		}
	}
	sp, _ := parseSpeak(cases["plain-string questions"])
	if len(sp.SuggestedQuestions) != 2 || sp.SuggestedQuestions[0]["text"] != "热搜里有没有相关的？" {
		t.Fatalf("questions = %+v", sp.SuggestedQuestions)
	}
	sp, _ = parseSpeak(cases["numeric fact ids"])
	if len(sp.FactIDs) != 2 || sp.FactIDs[0] != "1" {
		t.Fatalf("ids = %+v", sp.FactIDs)
	}
	// text that is not a conclusion at all is still refused
	if _, err := parseSpeak("没有 JSON，只有一段话。"); err == nil {
		t.Fatal("prose was read as a conclusion")
	}
	if _, err := parseSpeak(`{"text":"只有 text"}`); err == nil {
		t.Fatal("an object with only text was read as an opening")
	}
}

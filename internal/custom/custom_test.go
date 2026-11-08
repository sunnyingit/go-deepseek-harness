package custom

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func bars(closes ...float64) []Bar {
	out := make([]Bar, len(closes))
	for i, c := range closes {
		out[i] = Bar{Day: "2026-11-" + string(rune('0'+i/10)) + string(rune('0'+i%10)), Close: c}
	}
	return out
}

func TestCleanRefusesWhatCannotBeMeant(t *testing.T) {
	good := Task{Title: "每天看一眼", Kind: Analyze, Scope: "hold", At: "14:30", Days: "trading"}
	if _, err := Clean(good); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(Task) Task{
		"给任务起个名字":    func(x Task) Task { x.Title = "  "; return x },
		"时间写成 14:30": func(x Task) Task { x.At = "25:00"; return x },
		"任务类型不对":     func(x Task) Task { x.Kind = "x"; return x },
		"范围不对":       func(x Task) Task { x.Scope = "?"; return x },
		"至少选一只股票":    func(x Task) Task { x.Scope = "codes"; return x },
		"股票代码不对":     func(x Task) Task { x.Scope = "codes"; x.Codes = []string{"abc"}; return x },
		"写下要问的问题":    func(x Task) Task { x.Kind = Ask; return x },
		"至少设一个条件":    func(x Task) Task { x.Kind = Scan; return x },
		"天数要是 2 到":   func(x Task) Task { x.Kind = Scan; x.Rules = []Rule{{Kind: "below_ma", Value: 1}}; return x },
		"要填一个大于 0":   func(x Task) Task { x.Kind = Scan; x.Rules = []Rule{{Kind: "change_up"}}; return x },
		"没有这个条件":     func(x Task) Task { x.Kind = Scan; x.Rules = []Rule{{Kind: "zzz"}}; return x },
		"板块结论要从列表里选": func(x Task) Task { x.Kind = Scan; x.Rules = []Rule{{Kind: "sector_is", Text: "乱写"}}; return x },
	}
	for want, mutate := range cases {
		if _, err := Clean(mutate(good)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v", want, err)
		}
	}
	// a task that repeats every few minutes needs no time of day, and the gap has limits
	every := Task{Title: "盘中看", Kind: Scan, Scope: "all", Every: 30, Days: "trading", Rules: []Rule{{Kind: "change_up", Value: 5}}}
	if got, err := Clean(every); err != nil || got.At != "" || got.ScheduleText() != "交易时段每 30 分钟" {
		t.Fatalf("interval = %+v %v", got, err)
	}
	for want, mutate := range map[string]func(Task) Task{
		"最短每 5 分钟":   func(x Task) Task { x.Every = 3; return x },
		"最短每 30 分钟":  func(x Task) Task { x.Kind = Analyze; x.Rules = nil; x.Every = 15; return x },
		"间隔最长 12 小时": func(x Task) Task { x.Every = 725; return x },
		"5 分钟的整数倍":   func(x Task) Task { x.Every = 33; return x },
		"间隔不能是负数":    func(x Task) Task { x.Every = -5; return x },
	} {
		if _, err := Clean(mutate(every)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v", want, err)
		}
	}
	if (Task{Every: 120, Days: "any"}).ScheduleText() != "每 2 小时（全天）" {
		t.Error("hour wording")
	}
	got, err := Clean(Task{Title: " 看板块 ", Kind: Scan, Scope: "codes", Codes: []string{"600036.sh", "600036.SH", "02050.HK"}, At: "15:40", Days: "bad",
		Rules: []Rule{{Kind: "below_ma", Value: 20}, {Kind: "ai_bearish", Value: 9}}})
	if err != nil || got.Title != "看板块" || len(got.Codes) != 2 || got.Days != "trading" || got.Match != "all" || got.Rules[1].Value != 0 {
		t.Fatalf("clean = %+v %v", got, err)
	}
}

func TestRulesDecideFromWhatIsKnown(t *testing.T) {
	f := Facts{Price: 33.5, ChangePct: -5.2, HasChange: true, Closes: bars(35, 34, 33, 32, 31, 36, 40, 34, 33, 33.5), FlowOut: 4, HasFlow: true, AILean: "bearish", SectorVerd: "在流出"}
	task := Task{Rules: []Rule{{Kind: "change_down", Value: 5}, {Kind: "below_ma", Value: 5}, {Kind: "flow_out_days", Value: 3}, {Kind: "ai_bearish"}, {Kind: "sector_is", Text: "在流出"}}, Match: "all"}
	hit, why, unknown := Match(task, f)
	if !hit || unknown != 0 || len(why) != 5 {
		t.Fatalf("hit=%v why=%v unknown=%d", hit, why, unknown)
	}
	// one condition that does not hold stops an "all", and not an "any"
	task.Rules = append(task.Rules, Rule{Kind: "new_high", Value: 5})
	if hit, _, _ := Match(task, f); hit {
		t.Fatal("all matched with a failed condition")
	}
	task.Match = "any"
	if hit, _, _ := Match(task, f); !hit {
		t.Fatal("any did not match")
	}
	// missing data is not a match and is counted
	hit, _, unknown = Match(Task{Match: "all", Rules: []Rule{{Kind: "flow_in_days", Value: 3}}}, Facts{})
	if hit || unknown != 1 {
		t.Fatalf("empty facts: hit=%v unknown=%d", hit, unknown)
	}
	for kind, want := range map[string]bool{"price_below": true, "price_above": false} {
		if got, _, _ := Eval(Rule{Kind: kind, Value: 34}, f); got != want {
			t.Errorf("%s = %v", kind, got)
		}
	}
	if got, _, _ := Eval(Rule{Kind: "new_low", Value: 10}, Facts{Closes: bars(5, 4, 3, 2, 1, 2, 3, 4, 5, 0.5)}); !got {
		t.Error("new low missed")
	}
}

func TestStoreKeepsTasksAndTheirState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.json")
	st := Open(path, func() time.Time { return time.Unix(100, 0) })
	a, err := st.Add(Task{Title: "分析持仓", Kind: Analyze, Scope: "hold", At: "14:30"})
	if err != nil || a.ID != "c1" || !a.Enabled {
		t.Fatalf("add = %+v %v", a, err)
	}
	if _, ok := st.SetEnabled(a.ID, false); !ok {
		t.Fatal("not found")
	}
	b, _ := st.Update(a.ID, Task{Title: "分析持仓（改）", Kind: Ask, Scope: "wish", At: "15:00", Question: "有什么新消息"})
	if b.Enabled || b.ID != a.ID || b.Question == "" {
		t.Fatalf("update = %+v", b)
	}
	again := Open(path, nil)
	got, ok := again.Get(a.ID)
	if !ok || got.Title != "分析持仓（改）" || got.Enabled {
		t.Fatalf("reopened = %+v", got)
	}
	if !again.Remove(a.ID) || again.Remove(a.ID) || len(again.All()) != 0 {
		t.Fatal("remove misbehaved")
	}
	for i := 0; i < MaxTasks; i++ {
		if _, err := again.Add(Task{Title: "x", Kind: Analyze, Scope: "all", At: "10:00"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := again.Add(Task{Title: "x", Kind: Analyze, Scope: "all", At: "10:00"}); err == nil {
		t.Fatal("more than the limit was accepted")
	}
}

func TestWordsForATask(t *testing.T) {
	task := Task{Scope: "codes", Codes: []string{"600036.SH", "000001.SZ"}, At: "09:40", Days: "any", Match: "any",
		Rules: []Rule{{Kind: "below_ma", Value: 20}, {Kind: "flow_in_days", Value: 3}, {Kind: "ai_bullish"}}}
	if got := task.ScopeText(map[string]string{"600036.SH": "招商银行"}); got != "招商银行、000001.SZ" {
		t.Fatalf("scope = %q", got)
	}
	if task.ScheduleText() != "每天 09:40" || task.RulesText() != "收盘跌破 20 日均线，或 大资金连续净流入 3 日，或 AI 结论偏多" {
		t.Fatalf("%q | %q", task.ScheduleText(), task.RulesText())
	}
}

func TestAnalystsOfATaskAreCheckedAndTidied(t *testing.T) {
	base := Task{Title: "看资金和新闻", Kind: Analyze, Scope: "hold", At: "14:30", Analysts: []string{"volume_flow", "news", "news"}}
	got, err := Clean(base)
	if err != nil || len(got.Analysts) != 2 || got.AnalystsText() != "只分析：资金流、新闻" {
		t.Fatalf("partial = %+v %v", got, err)
	}
	// all five is the whole round; the chief cannot be picked for a bare analysis
	full := base
	full.Analysts = append([]string{}, Dimensions...)
	if got, _ := Clean(full); got.Analysts != nil || !strings.HasPrefix(got.AnalystsText(), "完整一轮") {
		t.Fatalf("full = %+v", got)
	}
	bad := base
	bad.Analysts = []string{"chief"}
	if _, err := Clean(bad); err == nil || !strings.Contains(err.Error(), "没有这位分析师") {
		t.Fatalf("chief in analyze: %v", err)
	}
	ask := Task{Title: "问问", Kind: Ask, Scope: "hold", At: "11:00", Question: "有新公告吗", Analysts: []string{"chief"}}
	if got, err := Clean(ask); err != nil || got.Analysts != nil || got.AnalystsText() != "问：主分析师" {
		t.Fatalf("ask chief = %+v %v", got, err)
	}
	ask.Analysts = []string{"news", "chief"}
	if got, err := Clean(ask); err != nil || got.AnalystsText() != "问：新闻、主分析师" {
		t.Fatalf("ask two = %+v %v", got, err)
	}
	scan := Task{Title: "扫", Kind: Scan, Scope: "all", At: "15:40", Analysts: []string{"news"}, Rules: []Rule{{Kind: "ai_bullish"}}}
	if got, _ := Clean(scan); got.Analysts != nil {
		t.Fatalf("a scan kept analysts: %+v", got)
	}
}

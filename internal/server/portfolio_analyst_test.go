package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go-harness/internal/personal"
)

// portfolioService plays the data service for the portfolio analyst: closes for a few stocks and the quality screen.
func portfolioService(t *testing.T) {
	closes := map[string][][2]any{
		"600036.SH": series("2025-11-03", 250, 30, 0.02),
		"000001.SZ": series("2025-11-03", 250, 10, 0.01),
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/api/v1/shared/closes/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"items": closes[strings.TrimPrefix(p, "/api/v1/shared/closes/")]})
		case strings.HasPrefix(p, "/api/v1/shared/quality/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "level": "clean", "flags": []any{}, "metrics": map[string]any{"roe": 0.15, "debt": 0.4}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		}
	}))
	t.Cleanup(api.Close)
	t.Setenv("STOCK_API_BASE", api.URL)
}

func TestThePortfolioAnalystReadsAFactTableAndOnlyFactsFromItSurvive(t *testing.T) {
	portfolioService(t)
	s, _ := newClientServer(t, zoneTime("2026-11-09", "21:40"))
	s.personal.TradeAdd(personal.Trade{Code: "600036.SH", Name: "招商银行", Side: "buy", Price: 30, Qty: 100, Date: "2026-10-01"})
	if _, err := s.personal.WishAdd(personal.Wish{Code: "000001.SZ", Name: "平安银行", Reason: "同行里便宜", PlanPct: 5, Conditions: []personal.Item{{"kind": "price_below", "value": 10.0}}}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var seenSystem, seenUser string
	s.portfolioFn = func(_ context.Context, system, user string) (string, error) {
		calls.Add(1)
		seenSystem, seenUser = system, user
		return `好的：{"text":"两只都是银行，逻辑几乎是同一个。","items":[` +
			`{"code":"600036.SH","title":"先看集中度","why":"持仓里只有它","action":"现在不加仓；如果跌破下方价再复核","fact_ids":["s1.who","s9.nope"]},` +
			`{"code":"000001.SZ","title":"想买的先等","why":"和招商银行同板块","action":"目标价 15 元再买","fact_ids":["s2.who"]},` +
			`{"code":"999999.SH","title":"不在这一组里","why":"x","action":"y","fact_ids":[]}],"fact_ids":["s1.trend","zzz"],"suggested_questions":["两只为什么算同一个逻辑？"]}`, nil
	}
	a := newCustomAPI(t, s)
	code, out := a.call("POST", "/api/portfolio/analyst", `{"mode":"check"}`)
	if code != 200 || out["status"] != "ok" || out["cached"] != false {
		t.Fatalf("first reading = %d %v", code, out)
	}
	res := out["result"].(map[string]any)
	items := res["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["name"] != "招商银行" {
		t.Fatalf("items = %v", items) // the stock that is not in the set is dropped
	}
	if ids := items[0].(map[string]any)["fact_ids"].([]any); len(ids) != 1 || ids[0] != "s1.who" {
		t.Fatalf("an id that is not in the table survived: %v", ids)
	}
	if act := items[1].(map[string]any)["action"]; act != "" {
		t.Fatalf("a line with a target price was kept: %v", act)
	}
	if ids := res["fact_ids"].([]any); len(ids) != 1 || ids[0] != "s1.trend" {
		t.Fatalf("top ids = %v", ids)
	}
	// what the model was given: the rules' prompt and a table with the stocks' own facts
	for _, want := range []string{"持仓分析师", "只能引用这张表", "不给目标价"} {
		if !strings.Contains(seenSystem, want) {
			t.Fatalf("system prompt lacks %q:\n%s", want, seenSystem)
		}
	}
	for _, want := range []string{"mode=check", "- s1.who: 招商银行（600036.SH）：你持有 100 股，成本 30.00", "- s1.trend: 招商银行：现价", "- s2.who: 平安银行（000001.SZ）：你想买，理由「同行里便宜」，计划仓位 5%", "质地筛查 没发现问题；ROE 15.0%"} {
		if !strings.Contains(seenUser, want) {
			t.Fatalf("fact table lacks %q:\n%s", want, seenUser)
		}
	}
	// the same facts: the saved reading comes back, no second call; force asks again
	_, out = a.call("POST", "/api/portfolio/analyst", `{"mode":"check"}`)
	if out["cached"] != true || calls.Load() != 1 {
		t.Fatalf("second reading = %v (calls %d)", out["cached"], calls.Load())
	}
	a.call("POST", "/api/portfolio/analyst", `{"mode":"check","force":true}`)
	if calls.Load() != 2 {
		t.Fatalf("force made %d calls in all", calls.Load())
	}
	// GET gives the saved reading without a call
	_, out = a.call("GET", "/api/portfolio/analyst?mode=check", "")
	if out["status"] != "ok" || calls.Load() != 2 {
		t.Fatalf("GET = %v", out)
	}
}

func TestComparingStocksAddsHowTheyMoveTogetherAndFollowUpsAreAnswered(t *testing.T) {
	portfolioService(t)
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00"))
	var user string
	s.portfolioFn = func(_ context.Context, system, u string) (string, error) {
		user = u
		if !strings.Contains(system, "正在回答用户对一组股票的追问") {
			return `{"text":"能比","items":[],"fact_ids":[],"suggested_questions":[]}`, nil
		}
		return "两只涨跌方向一致，先看招商银行。可以接着问：质地谁更好？", nil
	}
	a := newCustomAPI(t, s)
	if code, out := a.call("POST", "/api/portfolio/analyst", `{"mode":"compare","codes":["600036.SH"]}`); code != 502 || !strings.Contains(out["error"].(string), "至少要选两只") {
		t.Fatalf("one stock = %d %v", code, out)
	}
	code, out := a.call("POST", "/api/portfolio/analyst", `{"mode":"compare","codes":["600036.SH","000001.SZ","bad","600036.SH"]}`)
	if code != 200 || out["status"] != "ok" {
		t.Fatalf("compare = %d %v", code, out)
	}
	if !strings.Contains(user, "- c.1.2: 600036.SH 与 000001.SZ 近 60 日每日涨跌的相关系数") && !strings.Contains(user, "- c.1.2: ") {
		t.Fatalf("no pairwise fact:\n%s", user)
	}
	if !strings.Contains(user, "不在你的持仓和想买里，只作对比") {
		t.Fatalf("compared stocks are not marked as such:\n%s", user)
	}
	code, out = a.call("POST", "/api/portfolio/analyst", `{"mode":"compare","codes":["600036.SH","000001.SZ"],"question":"哪个先看？"}`)
	rep, _ := out["reply"].(map[string]any)
	if code != 200 || rep == nil || !strings.Contains(rep["answer"].(string), "先看招商银行") || !strings.Contains(user, "用户的问题：哪个先看？") {
		t.Fatalf("follow-up = %d %v", code, out)
	}
}

func TestAnUnreadableAnswerIsAnErrorNotAHalfResult(t *testing.T) {
	portfolioService(t)
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00"))
	s.portfolioFn = func(context.Context, string, string) (string, error) {
		return "我觉得都不错，没有 JSON。", nil
	}
	code, out := newCustomAPI(t, s).call("POST", "/api/portfolio/analyst", `{"mode":"compare","codes":["600036.SH","000001.SZ"]}`)
	if code != 502 || !strings.Contains(out["error"].(string), "没有给出可用的结论") {
		t.Fatalf("got %d %v", code, out)
	}
}

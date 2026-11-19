package inbox

import (
	"strings"
	"testing"
	"time"
)

func TestPublishDedupesRendersAndRoutesToTheDesktopByLoudness(t *testing.T) {
	in := Open(t.TempDir()+"/inbox.json", func() time.Time { return time.Unix(1_800_000_000, 0) })
	it, created, err := in.Publish("judgment.changed", "stock", "600036.SH", map[string]any{"ts_code": "600036.SH", "name": "招商银行", "headline": "从偏多变为不明"}, "600036.SH:2026-11-05", Options{})
	if err != nil || !created || it.Title != "招商银行 判断变了" || it.Body != "招商银行：从偏多变为不明" || it.Desktop != "pending" {
		t.Fatalf("item = %+v err=%v", it, err)
	}
	if _, created, _ := in.Publish("judgment.changed", "stock", "600036.SH", map[string]any{"ts_code": "600036.SH"}, "600036.SH:2026-11-05", Options{}); created {
		t.Fatal("duplicate key was published twice")
	}
	rec, _, _ := in.Publish("task.completed", "system", "stock.nightly", map[string]any{
		"task_id": "stock.nightly", "title": "每晚个股结论", "state": "succeeded", "state_text": "成功", "trigger_text": "定时",
		"duration_ms": 2500.0, "jobs": 3.0, "succeeded": 3.0,
		"apis": []any{"POST /api/v1/stocks/{code}/analyze"}, "detail": "写了 3 条结论",
	}, "client:r1", Options{})
	if rec.Desktop != "" || rec.Tone != "good" || !strings.Contains(rec.Body, "单元 3/3") || !strings.Contains(rec.Body, "2.5s") ||
		!strings.Contains(rec.Body, "写了 3 条结论") || strings.Contains(rec.Body, "调用：") {
		t.Fatalf("receipt = %+v", rec)
	}
	if rec.Link["page"] != "tasks" {
		t.Fatalf("link = %+v", rec.Link)
	}
	if got := in.DesktopPending(); len(got) != 1 || got[0]["delivery_id"] != it.ID {
		t.Fatalf("pending = %v", got)
	}
	if in.DesktopAck([]string{it.ID}) != 1 || len(in.DesktopPending()) != 0 {
		t.Fatal("ack did not clear the pop-up")
	}
	if in.Unread() != 2 || in.MarkRead([]string{it.ID}, false) != 1 || in.Unread() != 1 || in.MarkRead(nil, true) != 1 {
		t.Fatal("read marks are wrong")
	}
	if got := in.List("system", false, 10); len(got) != 1 || got[0].Type != "task.completed" {
		t.Fatalf("system list = %+v", got)
	}
}

func TestUnknownTypesAreRejectedAndTheFileSurvivesARestart(t *testing.T) {
	path := t.TempDir() + "/inbox.json"
	in := Open(path, nil)
	if _, _, err := in.Publish("nope", "", "", nil, "", Options{}); err == nil {
		t.Fatal("accepted an unknown type")
	}
	in.Publish("market.brief", "market", "market", map[string]any{"title": "市场 · 盘前简报", "text": "档位 偏强"}, "k", Options{})
	again := Open(path, nil)
	if got := again.List("market", false, 5); len(got) != 1 || got[0].Title != "市场 · 盘前简报" {
		t.Fatalf("after restart = %+v", got)
	}
	if _, created, _ := again.Publish("market.brief", "market", "market", map[string]any{"title": "x"}, "k", Options{}); created {
		t.Fatal("dedupe lost across restart")
	}
}

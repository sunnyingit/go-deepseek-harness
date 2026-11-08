package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func runBoardScript(t *testing.T, base string, args ...string) (string, error) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	cmd := exec.Command(py, append([]string{"../../web/skills/baidu-board/scripts/fetch_baidu_board.py"}, args...)...)
	cmd.Env = append(os.Environ(), "STOCK_API_BASE="+base)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestBaiduBoardScriptShapesTheServiceAnswerIntoFactsAndRows(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"status":"ok","board":"finance","board_name":"财经榜","updated_at":"2026-11-08 13:18","fetched_at":"2026-11-08 13:19:00","stale":false,
			"items":[{"rank":1,"title":"央企中国国新：将举报","hot":4842081,"hot_text":"484.2万","tag":"新","desc":""},{"rank":2,"title":"工作人员回应电动鞋售价7999元","hot":3901820,"hot_text":"390.2万","tag":"热","desc":"简介"}]}`))
	}))
	defer srv.Close()
	out, err := runBoardScript(t, srv.URL, "finance", "2")
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	if asked != "board=finance&limit=2" {
		t.Fatalf("asked the service %q", asked)
	}
	var doc struct {
		BoardName string `json:"board_name"`
		Count     int    `json:"count"`
		Stale     bool   `json:"stale"`
		Facts     []struct{ ID, Text string }
		Rows      []map[string]any
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if doc.BoardName != "财经榜" || doc.Count != 2 || doc.Stale || doc.Facts[0].ID != "bd.1" || !strings.Contains(doc.Facts[0].Text, "第 1 名 央企中国国新：将举报 热度 484.2万 [新]") {
		t.Fatalf("doc = %+v", doc)
	}
	if doc.Rows[1]["标签"] != "热" || doc.Rows[1]["简介"] != "简介" {
		t.Fatalf("rows = %+v", doc.Rows)
	}
}

func TestBaiduBoardScriptReportsTheServiceErrorAndRefusesBadLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"status":"error","error":"百度热搜暂时取不到"}`))
	}))
	defer srv.Close()
	if out, err := runBoardScript(t, srv.URL); err == nil || !strings.Contains(out, "百度热搜暂时取不到") {
		t.Fatalf("service error: err=%v out=%s", err, out)
	}
	if _, err := runBoardScript(t, srv.URL, "finance", "500"); err == nil {
		t.Fatal("a limit of 500 was accepted")
	}
}

func TestFundamentalPartScriptAsksTheReplyEndpointForTheChain(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	var path, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"ts_code":"002050.SZ","part":"chain","facts":[{"id":"fd.chain.position","text":"所处行业：家用电器"}],"unavailable":[]}`))
	}))
	defer srv.Close()
	cmd := exec.Command(py, "../../web/skills/fundamental-reply/scripts/fetch_part.py", "002050.SZ", "chain")
	cmd.Env = append(os.Environ(), "STOCK_API_BASE="+srv.URL)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "fd.chain.position") {
		t.Fatalf("err=%v out=%s", err, out)
	}
	if path != "/api/v1/stocks/002050.SZ/dimensions/fundamental/reply" || query != "part=chain" {
		t.Fatalf("asked %s?%s", path, query)
	}
}

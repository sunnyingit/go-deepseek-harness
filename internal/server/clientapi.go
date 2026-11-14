package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-harness/internal/inbox"
	"go-harness/internal/personal"
	"go-harness/internal/quotes"
	"go-harness/internal/sched"
)

func (s *Server) registerClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /notify/inbox", s.handleInbox)
	mux.HandleFunc("GET /notify/unread", s.handleUnread)
	mux.HandleFunc("POST /notify/inbox/read", s.handleInboxRead)
	mux.HandleFunc("POST /notify/inbox/delete", s.handleInboxDelete)
	mux.HandleFunc("GET /notify/desktop/pending", s.handleDesktopPending)
	mux.HandleFunc("POST /notify/desktop/ack", s.handleDesktopAck)
	s.registerHoldingsRoutes(mux)
	s.registerScanRoutes(mux)
	s.registerCustomRoutes(mux)
	s.registerSysRoutes(mux)
	s.registerServiceRoutes(mux)
	s.registerNotifySettingsRoutes(mux)
	mux.HandleFunc("GET /notify/relay", s.handleRelayStatus)
	mux.HandleFunc("POST /notify/relay", s.handleRelayEnable)
	mux.HandleFunc("DELETE /notify/relay", s.handleRelayDisable)
	mux.HandleFunc("GET /sched/tasks", s.handleSchedTasks)
	mux.HandleFunc("GET /sched/runs", s.handleSchedRuns)
	mux.HandleFunc("POST /sched/tasks/{id}/run", s.handleSchedRun)
	mux.HandleFunc("POST /sched/tick", s.handleSchedTick)
	mux.HandleFunc("GET /api/stocks/{code}/conclusion", s.handleConclusion)
	mux.HandleFunc("GET /api/stocks/{code}/chart", s.handleStockChart)
	mux.HandleFunc("GET /api/v1/holdings/quality", s.handleQuality)
	s.registerPortfolioRoutes(mux)
	mux.HandleFunc("GET /api/stocks/{code}/trade-plan", s.handleTradePlan)
	mux.HandleFunc("POST /api/stocks/{code}/trade-plan", s.handleTradePlan)
	mux.HandleFunc("POST /api/stocks/{code}/conclusion/refresh", s.handleConclusionRefresh)
	mux.HandleFunc("POST /api/stocks/{code}/conclusion/commit", s.handleConclusionCommit)
	mux.HandleFunc("GET /api/v1/watchlist", s.handleWatchList)
	mux.HandleFunc("POST /api/v1/watchlist", s.handleWatchAdd)
	mux.HandleFunc("DELETE /api/v1/watchlist/{code}", s.handleWatchRemove)
	mux.HandleFunc("GET /api/v1/stocks/search", s.handleStockSearch)
	mux.HandleFunc("GET /api/v1/stocks/{code}/alerts", s.handleAlertList)
	mux.HandleFunc("POST /api/v1/stocks/{code}/alerts", s.handleAlertAdd)
	mux.HandleFunc("DELETE /api/v1/alerts/{id}", s.handleAlertRemove)
}

func readJSON(r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	return json.NewDecoder(r.Body).Decode(out)
}

func pathCode(r *http.Request) (string, bool) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	return code, tsCodePattern.MatchString(code)
}

// ---- inbox

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	unreadOnly := r.URL.Query().Get("unread") == "1" || r.URL.Query().Get("unread") == "true"
	writeJSON(w, http.StatusOK, map[string]any{"items": s.inbox.List(r.URL.Query().Get("group"), unreadOnly, limit), "unread": s.inbox.Unread()})
}

func (s *Server) handleUnread(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"unread": s.inbox.Unread()})
}

func (s *Server) handleInboxRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	_ = readJSON(r, &body)
	n := s.inbox.MarkRead(body.IDs, body.All)
	writeJSON(w, http.StatusOK, map[string]any{"updated": n, "unread": s.inbox.Unread()})
}

func (s *Server) handleInboxDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	_ = readJSON(r, &body)
	n := s.inbox.Delete(body.IDs, body.All)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n, "unread": s.inbox.Unread()})
}

func (s *Server) handleDesktopPending(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.inbox.DesktopPending(), "unread": s.inbox.Unread()})
}

func (s *Server) handleDesktopAck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"delivery_ids"`
	}
	_ = readJSON(r, &body)
	writeJSON(w, http.StatusOK, map[string]any{"acked": s.inbox.DesktopAck(body.IDs)})
}

// ---- scheduler

func (s *Server) handleSchedTasks(w http.ResponseWriter, _ *http.Request) {
	var out []map[string]any
	for _, sp := range s.sch.Specs() {
		item := map[string]any{"task_id": sp.ID, "title": sp.Title, "queue": sp.Queue, "trigger": sp.Trigger, "paused": s.sch.Store.Paused(sp.ID)}
		if runs := s.sch.Store.Runs(sp.ID, 1); len(runs) > 0 {
			item["last_run"] = runs[0]
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

func (s *Server) handleSchedRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": s.sch.Store.Runs(r.URL.Query().Get("task"), limit)})
}

func (s *Server) handleSchedRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Units []string       `json:"units"`
		Input map[string]any `json:"input"`
	}
	_ = readJSON(r, &body)
	var units []sched.UnitSpec
	for _, u := range body.Units {
		units = append(units, sched.UnitSpec{Key: u})
	}
	run, err := s.sch.Manual(r.PathValue("id"), units, body.Input)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	s.sch.Tick()
	writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

// handleSchedTick is called by the desktop shell when the computer wakes up, so missed tasks are
// handled by their catch-up rules right away.
func (s *Server) handleSchedTick(w http.ResponseWriter, _ *http.Request) {
	s.sch.Tick()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- conclusion

func (s *Server) handleConclusion(w http.ResponseWriter, r *http.Request) {
	code, ok := pathCode(r)
	if !ok {
		writeError(w, http.StatusBadRequest, errBadCode(code))
		return
	}
	writeJSON(w, http.StatusOK, s.conclusionView(code, r.URL.Query().Get("enqueue") != "0"))
}

func (s *Server) handleConclusionRefresh(w http.ResponseWriter, r *http.Request) {
	code, ok := pathCode(r)
	if !ok {
		writeError(w, http.StatusBadRequest, errBadCode(code))
		return
	}
	out, err := s.requestConclusion(code, "manual", true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

// handleConclusionCommit turns a finished live 研判群 into the conclusion bundle without re-running models.
func (s *Server) handleConclusionCommit(w http.ResponseWriter, r *http.Request) {
	code, ok := pathCode(r)
	if !ok {
		writeError(w, http.StatusBadRequest, errBadCode(code))
		return
	}
	unlock := s.lockCode(code)
	defer unlock()
	b, err := s.commitBundleFromGroup(code, "live")
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": b.ID, "generated_at": b.GeneratedAt, "ts_code": code,
	})
}

// ---- watchlist and alerts

var plainCode = regexp.MustCompile(`^([0-9]{6}\.(SH|SZ|BJ)|[0-9]{5}\.HK)$`)

// knownIndexByAlias maps common A-share index nicknames to ts_code.
// Stock search only covers listed equities, so 创业板指 / 上证 等宽基名必须本地解析。
var knownIndexByAlias = map[string]string{
	"上证指数": "000001.SH", "上证综指": "000001.SH", "上证": "000001.SH", "沪指": "000001.SH",
	"深证成指": "399001.SZ", "深成指": "399001.SZ", "深成": "399001.SZ", "深指": "399001.SZ",
	"创业板指": "399006.SZ", "创业板指数": "399006.SZ", "创业板": "399006.SZ", "创指": "399006.SZ",
	"沪深300": "000300.SH", "沪深 300": "000300.SH", "HS300": "000300.SH",
	"中证500": "000905.SH", "中证 500": "000905.SH",
	"中证1000": "000852.SH", "中证 1000": "000852.SH",
	"上证50": "000016.SH", "上证 50": "000016.SH",
	"科创50": "000688.SH", "科创 50": "000688.SH", "科创板指": "000688.SH",
}

var knownIndexNameByCode = map[string]string{
	"000001.SH": "上证指数",
	"399001.SZ": "深证成指",
	"399006.SZ": "创业板指",
	"000300.SH": "沪深300",
	"000905.SH": "中证500",
	"000852.SH": "中证1000",
	"000016.SH": "上证50",
	"000688.SH": "科创50",
}

// resolveStock turns what the user typed into a ts_code and a name. A ts_code is used as it is; a name or
// bare number is looked up in the shared stock service. Common index aliases resolve locally.
func (s *Server) resolveStock(q string) (string, string, error) {
	q = strings.TrimSpace(q)
	upper := strings.ToUpper(q)
	if plainCode.MatchString(upper) {
		return upper, s.lookupName(upper), nil
	}
	if code, ok := knownIndexByAlias[q]; ok {
		return code, knownIndexNameByCode[code], nil
	}
	if code, ok := knownIndexByAlias[upper]; ok {
		return code, knownIndexNameByCode[code], nil
	}
	var found struct {
		Items []struct {
			TSCode string `json:"ts_code"`
			Name   string `json:"name"`
		} `json:"items"`
	}
	if err := s.stockGetJSON("/api/v1/stocks/search?q="+url.QueryEscape(q), &found); err != nil || len(found.Items) == 0 {
		return "", "", fmt.Errorf("未找到股票: %s", q)
	}
	return strings.ToUpper(found.Items[0].TSCode), found.Items[0].Name, nil
}

func (s *Server) lookupName(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if name, ok := knownIndexNameByCode[code]; ok {
		return name
	}
	if n := s.stockName(code); n != code {
		return n
	}
	if len(code) < 6 {
		return code
	}
	var found struct {
		Items []struct {
			TSCode string `json:"ts_code"`
			Name   string `json:"name"`
		} `json:"items"`
	}
	if err := s.stockGetJSON("/api/v1/stocks/search?q="+url.QueryEscape(code[:6]), &found); err == nil {
		for _, it := range found.Items {
			if strings.EqualFold(it.TSCode, code) {
				return it.Name
			}
		}
	}
	return code
}

func (s *Server) watchItems(only string) []map[string]any {
	list := s.personal.WatchList()
	codes := make([]string, 0, len(list))
	for _, w := range list {
		if only == "" || w.Code == only {
			codes = append(codes, w.Code)
		}
	}
	q, _ := s.quotes.Fetch(codes)
	now := s.now()
	var items []map[string]any
	for _, w := range list {
		if only != "" && w.Code != only {
			continue
		}
		items = append(items, s.watchItem(w, q[w.Code], now))
	}
	rank := map[string]int{"alert": 0, "change": 1, "state": 2, "stale": 3, "empty": 3}
	sort.SliceStable(items, func(a, b int) bool {
		ka, kb := items[a]["subtitle"].(map[string]any)["kind"].(string), items[b]["subtitle"].(map[string]any)["kind"].(string)
		return rank[ka] < rank[kb]
	})
	return items
}

func (s *Server) watchItem(w personal.Watch, q quotes.Quote, now time.Time) map[string]any {
	item := map[string]any{"ts_code": w.Code, "name": w.Name, "price": nil, "change": nil, "change_pct": nil, "judged_at": nil}
	if q.Price > 0 {
		item["price"], item["change"], item["change_pct"] = q.Price, q.Change, q.ChangePct
	}
	_, computing := s.sch.Store.ActiveJob(nightlyID, w.Code)
	item["computing"] = computing
	sub := map[string]any{"kind": "empty", "text": "还没有判断", "tone": "muted"}
	if b := s.readBundle(w.Code); b != nil {
		item["judged_at"] = b.GeneratedAt
		verdict, _ := b.Chief["verdict"].(string)
		item["direction"] = map[string]any{"lean": leanOf(verdict), "level": 0, "label": verdict}
		stale := s.staleReason(b, now) != ""
		item["stale"] = stale
		switch {
		case stale:
			sub = map[string]any{"kind": "stale", "text": "判断已过期", "tone": "muted"}
		case verdict != "":
			sub = map[string]any{"kind": "state", "text": verdict, "tone": "muted"}
		}
	}
	for _, a := range s.personal.Alerts(w.Code) {
		if q.Price <= 0 {
			break
		}
		gap := (a.Price - q.Price) / q.Price
		if gap < 0 {
			gap = -gap
		}
		hit := (a.Side == "down" && q.Price <= a.Price) || (a.Side == "up" && q.Price >= a.Price)
		if hit || gap <= 0.05 {
			text := fmt.Sprintf("离提醒价 %.2f 差 %.1f%%", a.Price, gap*100)
			if hit {
				text = fmt.Sprintf("已到提醒价 %.2f", a.Price)
			}
			sub = map[string]any{"kind": "alert", "text": text, "tone": "warn", "alert_id": a.ID}
			item["alert"] = map[string]any{"id": a.ID, "side": a.Side, "price": a.Price, "gap": gap, "reached": hit}
			break
		}
	}
	item["subtitle"] = sub
	return item
}

func (s *Server) handleWatchList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "updated_at": s.now().In(sched.Zone).Format("2006-01-02 15:04:05"), "items": s.watchItems("")})
}

func (s *Server) handleWatchAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Q, Code, Name string
	}
	_ = readJSON(r, &body)
	q := strings.TrimSpace(firstNonEmpty(body.Q, body.Code, body.Name))
	if q == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少参数，请传入 q=股票名称或代码"))
		return
	}
	code, name, err := s.resolveStock(q)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	added := s.personal.WatchAdd(code, name)
	if added {
		_, _ = s.requestConclusion(code, "watch_add", false) // a new stock gets its first conclusion right away
	}
	status := http.StatusOK
	if added {
		status = http.StatusCreated
	}
	items := s.watchItems(code)
	var item any
	if len(items) > 0 {
		item = items[0]
	}
	writeJSON(w, status, map[string]any{"status": "ok", "added": added, "item": item})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (s *Server) handleWatchRemove(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !s.personal.WatchRemove(code) {
		writeError(w, http.StatusNotFound, fmt.Errorf("自选里没有: %s", code))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "ts_code": code})
}

// handleStockSearch proxies the stock service search and enriches each hit with the local diagnosis lean.
func (s *Server) handleStockSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "items": []any{}})
		return
	}
	var found struct {
		Status string           `json:"status"`
		Error  string           `json:"error"`
		Items  []map[string]any `json:"items"`
	}
	if err := s.stockGetJSON("/api/v1/stocks/search?q="+url.QueryEscape(q), &found); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"status": "error", "error": "搜索失败: " + err.Error()})
		return
	}
	if found.Items == nil {
		found.Items = []map[string]any{}
	}
	for i, row := range found.Items {
		code, _ := row["ts_code"].(string)
		code = strings.ToUpper(strings.TrimSpace(code))
		lean, label := "", "还没诊断"
		verdict := ""
		if b := s.readBundle(code); b != nil {
			verdict, _ = b.Chief["verdict"].(string)
			if verdict != "" {
				label = verdict
				lean = leanOf(verdict)
			}
		}
		row["lean"] = lean
		row["verdict"] = verdict
		row["diagnosis"] = map[string]any{"lean": lean, "label": label, "verdict": verdict, "has": verdict != ""}
		found.Items[i] = row
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "items": found.Items})
}

func alertBody(a personal.Alert) map[string]any {
	return map[string]any{"id": a.ID, "ts_code": a.Code, "side": a.Side, "price": a.Price, "confirm": a.Confirm, "base_name": a.Name}
}

func (s *Server) handleAlertList(w http.ResponseWriter, r *http.Request) {
	code, ok := pathCode(r)
	if !ok {
		writeError(w, http.StatusBadRequest, errBadCode(code))
		return
	}
	items := []map[string]any{}
	for _, a := range s.personal.Alerts(code) {
		items = append(items, alertBody(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "items": items})
}

func (s *Server) handleAlertAdd(w http.ResponseWriter, r *http.Request) {
	code, ok := pathCode(r)
	if !ok {
		writeError(w, http.StatusBadRequest, errBadCode(code))
		return
	}
	var body struct {
		Side    string   `json:"side"`
		Price   *float64 `json:"price"`
		Confirm string   `json:"confirm"`
	}
	_ = readJSON(r, &body)
	if body.Side != "up" && body.Side != "down" {
		writeError(w, http.StatusBadRequest, errors.New("side 必须是 down 或 up"))
		return
	}
	if body.Price == nil {
		writeError(w, http.StatusBadRequest, errors.New("请直接传 price"))
		return
	}
	a, err := s.personal.AlertAdd(code, body.Side, *body.Price, body.Confirm, s.stockName(code))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "ok", "alert": alertBody(a)})
}

func (s *Server) handleAlertRemove(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if !s.personal.AlertRemove(id) {
		writeError(w, http.StatusNotFound, fmt.Errorf("没有这条提醒: %d", id))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "id": id})
}

// ---- notification settings

func (s *Server) registerNotifySettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /notify/settings", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.settingsView())
	})
	mux.HandleFunc("PUT /notify/settings", func(w http.ResponseWriter, r *http.Request) {
		var patch inbox.SettingsPatch
		if err := readJSON(r, &patch); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if _, err := s.inbox.Update(patch); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, s.settingsView())
	})
	mux.HandleFunc("PUT /notify/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		var c inbox.Channel
		if err := readJSON(r, &c); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		c.ID = r.PathValue("id")
		saved, err := s.inbox.PutChannel(c)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, saved.View())
	})
	mux.HandleFunc("DELETE /notify/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !s.inbox.DeleteChannel(r.PathValue("id")) {
			writeError(w, http.StatusNotFound, fmt.Errorf("没有这个渠道，或者它不能删除: %s", r.PathValue("id")))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": r.PathValue("id")})
	})
	mux.HandleFunc("POST /notify/channels/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		if err := s.inbox.Test(r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

func (s *Server) settingsView() map[string]any {
	st := s.inbox.Settings()
	channels := []map[string]any{}
	for _, c := range st.Channels {
		channels = append(channels, c.View())
	}
	var types []map[string]any
	for name, spec := range inbox.Registry {
		item := map[string]any{"type": name, "severity": spec.Severity, "group": spec.Group, "setting": st.Types[name]}
		types = append(types, item)
	}
	sort.Slice(types, func(a, b int) bool { return types[a]["type"].(string) < types[b]["type"].(string) })
	return map[string]any{"enabled": st.Enabled, "quiet_hours": st.Quiet, "collapse_s": st.CollapseS, "digest": st.Digest, "channels": channels, "types": types}
}

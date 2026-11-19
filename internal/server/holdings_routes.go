package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"go-harness/internal/holdings"
	"go-harness/internal/personal"
	"go-harness/internal/sched"
)

// The holdings page's API, with the same paths and shapes the page was built against. The work is done
// here, on this machine, because it needs the user's trades; only the market and sector inputs come from
// the shared data service.

func (s *Server) holdingsCtx() *holdings.Ctx {
	return holdings.NewCtx(s.personal, holdingsData{s}, s.now().In(sched.Zone), s.inbox)
}

func holdingsFail(w http.ResponseWriter, err error, label string) {
	var in personal.InputError
	if errors.As(err, &in) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	log.Printf("holdings %s failed: %v", label, err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func ok(body holdings.Map) holdings.Map {
	out := holdings.Map{"status": "ok"}
	for k, v := range body {
		out[k] = v
	}
	return out
}

func readMap(r *http.Request) holdings.Map {
	var m holdings.Map
	_ = readJSON(r, &m)
	if m == nil {
		m = holdings.Map{}
	}
	return m
}

func (s *Server) registerHoldingsRoutes(mux *http.ServeMux) {
	s.registerPersonalImport(mux)
	const base = "/api/v1/holdings"
	mux.HandleFunc("GET "+base+"/summary", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ok(s.holdingsCtx().Summary()))
	})
	mux.HandleFunc("GET "+base+"/tab/{name}", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.holdingsCtx()
		switch r.PathValue("name") {
		case "buy":
			writeJSON(w, http.StatusOK, ok(ctx.TabBuy()))
		case "plan":
			writeJSON(w, http.StatusOK, ok(ctx.TabPlan(r.URL.Query().Get("date"))))
		case "chk":
			writeJSON(w, http.StatusOK, ok(ctx.TabCheck(false)))
		case "rev":
			writeJSON(w, http.StatusOK, ok(ctx.TabReview()))
		case "now":
			writeJSON(w, http.StatusOK, ok(holdings.Map{}))
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name 必须是 buy、now、plan、chk、rev"})
		}
	})
	mux.HandleFunc("GET "+base+"/stock/{code}/chart-overlay", func(w http.ResponseWriter, r *http.Request) {
		code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
		if code == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少股票代码"})
			return
		}
		writeJSON(w, http.StatusOK, ok(s.holdingsCtx().ChartOverlay(code)))
	})
	mux.HandleFunc("POST "+base+"/plans/preview", func(w http.ResponseWriter, r *http.Request) {
		body := readMap(r)
		q, _ := body["ts_code"].(string)
		if q == "" {
			q, _ = body["q"].(string)
		}
		writeJSON(w, http.StatusOK, ok(s.holdingsCtx().PlanPreview(q)))
	})
	mux.HandleFunc("POST "+base+"/plans", func(w http.ResponseWriter, r *http.Request) {
		item, err := s.holdingsCtx().AddPlanItem(readMap(r))
		if err != nil {
			holdingsFail(w, err, "plan add")
			return
		}
		writeJSON(w, http.StatusCreated, holdings.Map{"status": "ok", "item": item})
	})
	mux.HandleFunc("PUT "+base+"/plans/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		item, err := s.holdingsCtx().UpdatePlanItem(id, readMap(r))
		if err != nil {
			holdingsFail(w, err, "plan update")
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "item": item})
	})
	mux.HandleFunc("PUT "+base+"/plans/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		status, _ := readMap(r)["status"].(string)
		item, err := s.holdingsCtx().SetPlanStatus(id, status)
		if err != nil {
			holdingsFail(w, err, "plan status")
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "item": item})
	})
	mux.HandleFunc("DELETE "+base+"/plans/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		if !s.personal.PlanRemove(id) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "没有这条计划: " + r.PathValue("id")})
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "id": id})
	})
	mux.HandleFunc("POST "+base+"/checkup", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ok(s.holdingsCtx().TabCheck(true)))
	})
	mux.HandleFunc("GET "+base+"/stock/{code}", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.holdingsCtx()
		code := strings.ToUpper(r.PathValue("code"))
		if c, _, found := ctx.Data.Resolve(code); found {
			code = c
		}
		detail := ctx.StockDetail(code)
		if detail == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "持仓、想买和交易计划里没有: " + code})
			return
		}
		writeJSON(w, http.StatusOK, ok(detail))
	})
	mux.HandleFunc("GET "+base+"/stock/{code}/entries", func(w http.ResponseWriter, r *http.Request) {
		ctx := s.holdingsCtx()
		code := strings.ToUpper(r.PathValue("code"))
		if c, _, found := ctx.Data.Resolve(code); found {
			code = c
		}
		out := ctx.StockEntries(code)
		if out == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "持仓、想买和交易计划里没有: " + code})
			return
		}
		writeJSON(w, http.StatusOK, ok(out))
	})
	mux.HandleFunc("PUT "+base+"/stock/{code}/reason", func(w http.ResponseWriter, r *http.Request) {
		body := readMap(r)
		ctx := s.holdingsCtx()
		code := strings.ToUpper(r.PathValue("code"))
		if c, _, found := ctx.Data.Resolve(code); found {
			code = c
		}
		var invalid []personal.Item
		if raw, okk := body["invalid"].([]any); okk {
			for _, e := range raw {
				if m, isMap := e.(map[string]any); isMap {
					invalid = append(invalid, m)
				}
			}
		}
		text, _ := body["text"].(string)
		row, err := ctx.SetReason(code, text, invalid)
		if err != nil {
			holdingsFail(w, err, "reason")
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "reason": row})
	})
	mux.HandleFunc("POST "+base+"/stock/{code}/price-line", func(w http.ResponseWriter, r *http.Request) {
		body := readMap(r)
		price, valid := 0.0, false
		switch v := body["price"].(type) {
		case float64:
			price, valid = v, true
		case string:
			f, err := strconv.ParseFloat(v, 64)
			price, valid = f, err == nil
		}
		if !valid {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "price 必须是数字"})
			return
		}
		ctx := s.holdingsCtx()
		code := strings.ToUpper(r.PathValue("code"))
		if c, _, found := ctx.Data.Resolve(code); found {
			code = c
		}
		if err := ctx.SetPriceLine(code, price); err != nil {
			holdingsFail(w, err, "price-line")
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok"})
	})
	mux.HandleFunc("POST "+base+"/stock/{code}/rules", func(w http.ResponseWriter, r *http.Request) {
		body := readMap(r)
		kind, _ := body["kind"].(string)
		value, _ := body["value"].(float64)
		rule, err := s.personal.AddRule(strings.ToUpper(r.PathValue("code")), kind, value)
		if err != nil {
			holdingsFail(w, err, "rule add")
			return
		}
		writeJSON(w, http.StatusCreated, holdings.Map{"status": "ok", "rule": rule})
	})
	mux.HandleFunc("DELETE "+base+"/rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		if !s.personal.RemoveRule(id) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "没有这条规则: " + r.PathValue("id")})
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "id": id})
	})
	mux.HandleFunc("POST "+base+"/trades", func(w http.ResponseWriter, r *http.Request) {
		out, err := s.holdingsCtx().RecordTrade(readMap(r))
		if err != nil {
			holdingsFail(w, err, "trade add")
			return
		}
		writeJSON(w, http.StatusCreated, ok(out))
	})
	mux.HandleFunc("POST "+base+"/trades/preview", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ok(s.holdingsCtx().TradePreview(readMap(r))))
	})
	mux.HandleFunc("DELETE "+base+"/trades/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		removed, err := s.personal.TradeRemove(id)
		if err != nil {
			holdingsFail(w, err, "trade remove")
			return
		}
		if !removed {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "没有这笔记录: " + r.PathValue("id")})
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "id": id})
	})
	mux.HandleFunc("PUT "+base+"/trades/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		t, err := s.holdingsCtx().UpdateTrade(id, readMap(r))
		if err != nil {
			holdingsFail(w, err, "trade update")
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "trade": t})
	})
	mux.HandleFunc("POST "+base+"/stock/{code}/adjust", func(w http.ResponseWriter, r *http.Request) {
		t, err := s.holdingsCtx().AdjustPosition(r.PathValue("code"), readMap(r))
		if err != nil {
			holdingsFail(w, err, "position adjust")
			return
		}
		writeJSON(w, http.StatusCreated, holdings.Map{"status": "ok", "trade": t})
	})
	mux.HandleFunc("PUT "+base+"/capital", func(w http.ResponseWriter, r *http.Request) {
		out, err := s.holdingsCtx().SetCapital(readMap(r))
		if err != nil {
			holdingsFail(w, err, "capital")
			return
		}
		writeJSON(w, http.StatusOK, ok(out))
	})
	mux.HandleFunc("PUT "+base+"/wishes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		wish, err := s.holdingsCtx().UpdateWish(id, readMap(r))
		if err != nil {
			holdingsFail(w, err, "wish update")
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "wish": wish})
	})
	mux.HandleFunc("PUT "+base+"/trades/{id}/note", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		note, _ := readMap(r)["note"].(string)
		if !s.personal.SetTradeNote(id, note) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "没有这笔记录: " + r.PathValue("id")})
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "id": id})
	})
	mux.HandleFunc("POST "+base+"/import", func(w http.ResponseWriter, r *http.Request) {
		text, _ := readMap(r)["text"].(string)
		if strings.TrimSpace(text) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "text 不能为空，把券商导出的持仓贴进来"})
			return
		}
		writeJSON(w, http.StatusOK, ok(s.holdingsCtx().ImportPositions(text)))
	})
	mux.HandleFunc("GET "+base+"/candidates", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ok(s.holdingsCtx().Candidates()))
	})
	mux.HandleFunc("POST "+base+"/wishes/preview", func(w http.ResponseWriter, r *http.Request) {
		body := readMap(r)
		plan, _ := body["plan_pct"].(float64)
		q, _ := body["ts_code"].(string)
		if q == "" {
			q, _ = body["q"].(string)
		}
		writeJSON(w, http.StatusOK, ok(s.holdingsCtx().WishPreview(q, plan)))
	})
	mux.HandleFunc("POST "+base+"/wishes", func(w http.ResponseWriter, r *http.Request) {
		wish, err := s.holdingsCtx().AddWish(readMap(r))
		if err != nil {
			holdingsFail(w, err, "wish add")
			return
		}
		writeJSON(w, http.StatusCreated, holdings.Map{"status": "ok", "wish": wish})
	})
	mux.HandleFunc("DELETE "+base+"/wishes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		if !s.personal.WishClose(id) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "没有这条想买: " + r.PathValue("id")})
			return
		}
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "id": id})
	})
	mux.HandleFunc("GET "+base+"/notifications", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ok(s.holdingsCtx().Notifications()))
	})
	mux.HandleFunc("POST "+base+"/notifications/read", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = readJSON(r, &body)
		writeJSON(w, http.StatusOK, holdings.Map{"status": "ok", "count": s.holdingsCtx().MarkRead(body.IDs)})
	})
}

func (s *Server) registerPersonalImport(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/personal/import", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
		var e personal.Export
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "导出文件读不出来：" + err.Error()})
			return
		}
		done, counts := s.personal.Import(e)
		if !done {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "本机已经有成交、想买、自选或提醒，不再导入，避免重复"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "imported": counts})
	})
}

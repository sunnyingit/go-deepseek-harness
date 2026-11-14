package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	coretools "go-harness/core/tools"
)

// uiCardStash keeps structured payloads the 问问 page paints as stock-card
// events. Keyed by tool call id. take removes the entry so a replay of the
// same result does not paint the card twice.
type uiCardStash struct {
	mu sync.Mutex
	m  map[string]map[string]any
}

func newUICardStash() *uiCardStash {
	return &uiCardStash{m: map[string]map[string]any{}}
}

func (s *uiCardStash) put(callID string, card map[string]any) {
	if s == nil || callID == "" || card == nil {
		return
	}
	s.mu.Lock()
	s.m[callID] = card
	s.mu.Unlock()
}

// take returns the card without deleting it. Watch reconnects replay the
// same tool/result events; deleting on first read left later connections
// with a bare「工具 完成」row and no stock/analyst card.
func (s *uiCardStash) take(callID string) (map[string]any, bool) {
	if s == nil || callID == "" {
		return nil, false
	}
	s.mu.Lock()
	card, ok := s.m[callID]
	s.mu.Unlock()
	return card, ok
}

func (s *Server) stashCard(callID string, card map[string]any) {
	if s == nil || s.cards == nil {
		return
	}
	s.cards.put(callID, card)
}

func callIDOf(run *coretools.RunContext) string {
	if run == nil {
		return ""
	}
	return run.CallID
}

func excerptRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func wishlistCards(tab map[string]any) []map[string]any {
	raw, err := json.Marshal(tab["items"])
	if err != nil {
		return []map[string]any{}
	}
	var rows []map[string]any
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"code":   cardField(r["ts_code"]),
			"name":   cardField(r["name"]),
			"reason": cardField(r["reason"]),
		})
	}
	return out
}

func cardField(v any) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "<nil>" {
		return ""
	}
	return s
}

// sessionAnalystIDs are dimension analysts the session orchestrator may summon.
// bull/bear stay out: they need a chief debate context and are easy to misuse mid-chat.
var sessionAnalystIDs = []string{
	"chief", "sector", "market", "volume_flow", "news", "finance", "fundamental",
}

var sessionAnalystName = map[string]string{
	"chief":       "主分析师",
	"sector":      "板块分析师",
	"market":      "大盘分析师",
	"volume_flow": "资金流分析师",
	"news":        "新闻分析师",
	"finance":     "财报分析师",
	"fundamental": "基本面分析师",
}

// registerSessionTools mounts the orchestrator toolkit on the 问问 session agent:
// search stocks, summon analysts, compare a basket, manage wishlist, glance hot boards.
func (s *Server) registerSessionTools(toolRegistry *coretools.Registry) error {
	for _, def := range []coretools.ToolDefinition{
		s.toolSearchStock(),
		s.toolAskAnalyst(),
		s.toolCompareStocks(),
		s.toolListHoldings(),
		s.toolListWishlist(),
		s.toolAddWishlist(),
	} {
		if _, err := toolRegistry.Register(def); err != nil {
			return err
		}
	}
	if board, err := s.fetchBaiduBoardTool(); err == nil {
		if _, err := toolRegistry.Register(board); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) toolSearchStock() coretools.ToolDefinition {
	return coretools.ToolDefinition{
		Name:        "search_stock",
		Description: "用股票/指数名称或代码解析标的，返回 ts_code 与名称。点名分析师或对比前先调用。支持创业板指、上证、沪深300 等宽基指数别名。",
		Parameters: coretools.ParameterSchema{
			"q": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "股票/指数名或代码，如 兆易创新 / 创业板指 / 603986 / 603986.SH"}, Required: true},
		},
		Output:            coretools.Output{Schema: coretools.ValueSchema{Kind: coretools.KindString}, Render: renderStringResult},
		TimeoutMs:         15_000,
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute: func(ctx context.Context, args map[string]any, run *coretools.RunContext) (any, error) {
			q := strings.TrimSpace(fmt.Sprint(args["q"]))
			if q == "" {
				return "", fmt.Errorf("缺少 q")
			}
			code, name, err := s.resolveStock(q)
			if err != nil {
				return "", err
			}
			s.stashCard(callIDOf(run), map[string]any{"kind": "stock", "code": code, "name": name})
			return fmt.Sprintf("ts_code=%s name=%s", code, name), nil
		},
	}
}

func (s *Server) toolAskAnalyst() coretools.ToolDefinition {
	ids := strings.Join(sessionAnalystIDs, ", ")
	return coretools.ToolDefinition{
		Name: "ask_analyst",
		Description: "请一位维度分析师作答（追问级 reply）。" +
			"analyst 可选：" + ids + "。" +
			"对应关系：技术位置/走势/资金=volume_flow，基本面=fundamental，新闻=news，财报=finance，板块=sector，大盘=market。" +
			"不要用 chief 充当其中一维，也不要把它放在第一位；chief 只在用户明确点名主分析师时使用。综合结论由你自己写。" +
			"禁止默认带上会话里上一只个股代码；只有用户本句明确点了股票/指数时才传 ts_code。" +
			"volume_flow / fundamental / finance / sector：用户点了标的才传 ts_code，否则先 search_stock；不要臆造代码。" +
			"market / news 可以不带 ts_code（纯大盘、主题新闻、财经热点）。点名创业板指等指数时再 search_stock 传入。一次只请一位。",
		Parameters: coretools.ParameterSchema{
			"analyst":  {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "分析师 id：" + ids}, Required: true},
			"ts_code":  {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "仅当用户明确点名股票/指数时传入，如 603986.SH、399006.SZ 或创业板指；大盘/主题新闻可省略。禁止沿用上一只个股"}},
			"question": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "要这位分析师回答的问题，尽量具体"}, Required: true},
		},
		Output:    coretools.Output{Schema: coretools.ValueSchema{Kind: coretools.KindString}, Render: renderStringResult},
		TimeoutMs: 300_000,
		Execute: func(ctx context.Context, args map[string]any, run *coretools.RunContext) (any, error) {
			id := strings.TrimSpace(fmt.Sprint(args["analyst"]))
			code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(args["ts_code"])))
			if code == "<NIL>" || code == "NIL" || code == "NULL" || code == "-" {
				code = ""
			}
			question := strings.TrimSpace(fmt.Sprint(args["question"]))
			if sessionAnalystName[id] == "" {
				return "", fmt.Errorf("analyst 只能是 %s", ids)
			}
			if question == "" {
				return "", fmt.Errorf("缺少 question")
			}
			if code != "" && !tsCodePattern.MatchString(code) {
				// 模型有时把「创业板指」直接塞进 ts_code，先当名称解析。
				resolved, _, err := s.resolveStock(fmt.Sprint(args["ts_code"]))
				if err != nil {
					return "", fmt.Errorf("股票代码无法识别: %s", code)
				}
				code = resolved
			}
			if code == "" && id != "news" && id != "market" {
				return "", fmt.Errorf("%s 需要用户明确的 ts_code；不要沿用上一只个股，请先 search_stock", sessionAnalystName[id])
			}
			cfg, err := loadAgent(id)
			if err != nil {
				return "", err
			}
			// Codeless news/market must not share one global lock — that
			// serializes unrelated topic asks across sessions for up to 5 min.
			lockKey := code
			if lockKey == "" {
				lockKey = id + ":topic"
			}
			unlock := s.lockCode(lockKey)
			defer unlock()
			msg, err := s.runAnalyst(ctx, cfg, code, "reply", question)
			if err != nil {
				return "", fmt.Errorf("%s", analystErrText(sessionAnalystName[id], err))
			}
			text := strings.TrimSpace(fmt.Sprint(msg["text"]))
			if text == "" {
				return "", fmt.Errorf("%s 没有写出正文", sessionAnalystName[id])
			}
			name := sessionAnalystName[id]
			stock := ""
			if code != "" {
				stock = s.lookupName(code)
			}
			s.stashCard(callIDOf(run), map[string]any{
				"kind": "analyst", "analyst": id, "name": name, "code": code, "stock": stock,
				"excerpt": excerptRunes(text, 180),
			})
			label := "主题新闻"
			if code != "" {
				label = code
				if stock != "" && !strings.EqualFold(stock, code) {
					label = stock + " " + code
				}
			}
			return fmt.Sprintf("【%s · %s】\n%s", name, label, text), nil
		},
	}
}

func (s *Server) toolCompareStocks() coretools.ToolDefinition {
	return coretools.ToolDefinition{
		Name: "compare_stocks",
		Description: "持仓分析师对比 2～6 只股票：走势位置、质地、AI 态度、板块与联动。" +
			"codes 用逗号分隔的 ts_code。用于「这两只怎么选」「对比一下」。",
		Parameters: coretools.ParameterSchema{
			"codes": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "逗号分隔代码，如 603986.SH,300750.SZ"}, Required: true},
			"force": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindBoolean, Description: "true 时强制重算，默认复用同输入缓存"}},
		},
		Output:    coretools.Output{Schema: coretools.ValueSchema{Kind: coretools.KindString}, Render: renderStringResult},
		TimeoutMs: 180_000,
		Execute: func(ctx context.Context, args map[string]any, run *coretools.RunContext) (any, error) {
			codes := splitCodes(fmt.Sprint(args["codes"]))
			force, _ := args["force"].(bool)
			res, _, err := s.analysePortfolio(ctx, "compare", codes, force)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			b.WriteString(res.Text)
			uiItems := make([]map[string]any, 0, len(res.Items))
			if len(res.Items) > 0 {
				b.WriteString("\n\n分项：")
				for _, it := range res.Items {
					fmt.Fprintf(&b, "\n- %s（%s）：%s", it.Name, it.Code, strings.TrimSpace(it.Title+" "+it.Why))
					if it.Action != "" {
						fmt.Fprintf(&b, " → %s", it.Action)
					}
					uiItems = append(uiItems, map[string]any{
						"code": it.Code, "name": it.Name, "title": it.Title, "why": it.Why, "action": it.Action,
					})
				}
			}
			s.stashCard(callIDOf(run), map[string]any{"kind": "compare", "items": uiItems})
			return strings.TrimSpace(b.String()), nil
		},
	}
}

func (s *Server) toolListHoldings() coretools.ToolDefinition {
	return coretools.ToolDefinition{
		Name:              "list_holdings",
		Description:       "列出用户当前真实持仓（代码、名称、数量、成本、现价、盈亏、仓位）。问「我的持仓」「拿了哪些」「仓位重不重」时先调用。只读，不会改持仓。想买清单用 list_wishlist，不要混用。",
		Parameters:        coretools.ParameterSchema{},
		Output:            coretools.Output{Schema: coretools.ValueSchema{Kind: coretools.KindString}, Render: renderStringResult},
		TimeoutMs:         20_000,
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute: func(ctx context.Context, _ map[string]any, run *coretools.RunContext) (any, error) {
			rows := s.holdingsCtx().Rows()
			items := make([]map[string]any, 0, len(rows))
			if len(rows) == 0 {
				s.stashCard(callIDOf(run), map[string]any{"kind": "holdings", "items": items})
				return "当前没有持仓。", nil
			}
			var b strings.Builder
			b.WriteString("当前持仓：\n")
			for _, r := range rows {
				items = append(items, map[string]any{
					"code": r.Code, "name": r.Name, "qty": r.Qty,
					"avg": r.Avg, "price": r.Price, "pnl_pct": r.PnLPct, "weight": r.Weight,
				})
				fmt.Fprintf(&b, "- %s（%s）数量 %d，成本 %.2f，现价 %.2f，盈亏 %.1f%%，仓位 %.1f%%\n",
					r.Name, r.Code, r.Qty, r.Avg, r.Price, r.PnLPct*100, r.Weight*100)
			}
			s.stashCard(callIDOf(run), map[string]any{"kind": "holdings", "items": items})
			return strings.TrimSpace(b.String()), nil
		},
	}
}

func (s *Server) toolListWishlist() coretools.ToolDefinition {
	return coretools.ToolDefinition{
		Name:              "list_wishlist",
		Description:       "列出用户当前「想买」列表（代码、名称、理由、计划仓位）。加想买前可先看有没有重复。",
		Parameters:        coretools.ParameterSchema{},
		Output:            coretools.Output{Schema: coretools.ValueSchema{Kind: coretools.KindString}, Render: renderStringResult},
		TimeoutMs:         10_000,
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute: func(ctx context.Context, _ map[string]any, run *coretools.RunContext) (any, error) {
			tab := s.holdingsCtx().TabBuy()
			raw, _ := json.Marshal(tab)
			s.stashCard(callIDOf(run), map[string]any{"kind": "wishlist", "preview": false, "items": wishlistCards(tab)})
			return string(raw), nil
		},
	}
}

func (s *Server) toolAddWishlist() coretools.ToolDefinition {
	return coretools.ToolDefinition{
		Name: "add_to_wishlist",
		Description: "把一只股票写入用户的「想买」列表。这是写操作：" +
			"必须先得到用户明确同意，再以 confirm=true 调用；confirm 不为 true 时不会写入，只返回将要写入的预览。" +
			"reason 写清为什么想买（可来自对比结论）。",
		Parameters: coretools.ParameterSchema{
			"ts_code":  {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "股票代码"}, Required: true},
			"reason":   {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "想买理由"}, Required: true},
			"confirm":  {ValueSchema: coretools.ValueSchema{Kind: coretools.KindBoolean, Description: "必须为 true 才真正写入"}, Required: true},
			"plan_pct": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindNumber, Description: "计划仓位百分比，可选"}},
		},
		Output:    coretools.Output{Schema: coretools.ValueSchema{Kind: coretools.KindString}, Render: renderStringResult},
		TimeoutMs: 15_000,
		Execute: func(ctx context.Context, args map[string]any, run *coretools.RunContext) (any, error) {
			code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(args["ts_code"])))
			reason := strings.TrimSpace(fmt.Sprint(args["reason"]))
			confirm, _ := args["confirm"].(bool)
			if !tsCodePattern.MatchString(code) {
				return "", fmt.Errorf("股票代码无法识别: %s", code)
			}
			if reason == "" {
				return "", fmt.Errorf("缺少 reason")
			}
			name := s.stockName(code)
			body := map[string]any{"ts_code": code, "reason": reason, "source": "ask"}
			if v, ok := args["plan_pct"].(float64); ok && v > 0 {
				body["plan_pct"] = v
			}
			if !confirm {
				s.stashCard(callIDOf(run), map[string]any{
					"kind": "wishlist", "preview": true, "code": code, "name": name, "reason": reason,
				})
				return fmt.Sprintf("预览（未写入）：将把 %s（%s）加入想买，理由「%s」。用户确认后请再以 confirm=true 调用。", name, code, reason), nil
			}
			wish, err := s.holdingsCtx().AddWish(body)
			if err != nil {
				return "", err
			}
			s.stashCard(callIDOf(run), map[string]any{
				"kind": "wishlist", "preview": false, "code": wish.Code, "name": wish.Name, "reason": strings.TrimSpace(wish.Reason),
			})
			return fmt.Sprintf("已加入想买：%s（%s），id=%d，理由「%s」", wish.Name, wish.Code, wish.ID, strings.TrimSpace(wish.Reason)), nil
		},
	}
}

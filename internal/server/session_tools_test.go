package server

import (
	"fmt"
	"strings"
	"testing"

	coretools "go-harness/core/tools"
)

func requiredNames(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return nil
	}
}

func TestSessionAnalystIDsAreLoadable(t *testing.T) {
	for _, id := range sessionAnalystIDs {
		cfg, err := loadAgent(id)
		if err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
		if cfg.ID != id {
			t.Fatalf("id = %q want %q", cfg.ID, id)
		}
		if sessionAnalystName[id] == "" {
			t.Fatalf("missing display name for %s", id)
		}
	}
}

func TestRegisterSessionTools(t *testing.T) {
	s := &Server{}
	reg := coretools.NewRegistry()
	if err := s.registerSessionTools(reg); err != nil {
		t.Fatal(err)
	}
	schemas := reg.Schemas()
	have := map[string]bool{}
	var ask coretools.ToolSchema
	for _, sch := range schemas {
		have[sch.Name] = true
		if sch.Name == "ask_analyst" {
			ask = sch
		}
	}
	for _, name := range []string{"search_stock", "ask_analyst", "compare_stocks", "list_holdings", "list_wishlist", "add_to_wishlist"} {
		if !have[name] {
			t.Fatalf("missing tool %s", name)
		}
	}
	for _, item := range requiredNames(ask.Parameters["required"]) {
		if item == "ts_code" {
			t.Fatal("ask_analyst.ts_code must be optional for topic news")
		}
	}
	if !strings.Contains(ask.Description, "news 可以不带") && !strings.Contains(ask.Description, "可以不带 ts_code") {
		t.Fatalf("ask_analyst description should allow news without ts_code: %s", ask.Description)
	}
}

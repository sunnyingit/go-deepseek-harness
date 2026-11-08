package server

import "testing"

func TestParseAskIntentSectorThemeDoesNotReuse(t *testing.T) {
	raw := `{"intent":"sector_theme","reuse_last":true,"stock_query":"603986.SH","theme":"AI硬件","ask":"分析AI硬件板块","note":"板块","analysts":["sector"]}`
	got := parseAskIntent(raw, "603986.SH")
	if got.Intent != "sector_theme" || got.ReuseLast {
		t.Fatalf("got %+v", got)
	}
	if got.Theme != "AI硬件" || len(got.Analysts) != 1 || got.Analysts[0] != "sector" {
		t.Fatalf("theme/analysts = %+v", got)
	}
}

func TestParseAskIntentFollowupNeedsLast(t *testing.T) {
	raw := `{"intent":"stock_followup","reuse_last":true,"ask":"关于兆易创新所属产业：这个产业走下坡路吗","note":"沿用","analysts":["sector"]}`
	got := parseAskIntent(raw, "603986.SH")
	if got.Intent != "stock_followup" || !got.ReuseLast {
		t.Fatalf("with last: %+v", got)
	}
	empty := parseAskIntent(raw, "")
	if empty.Intent != "general" || empty.ReuseLast {
		t.Fatalf("without last: %+v", empty)
	}
}

func TestParseAskIntentUnknownFallsToGeneral(t *testing.T) {
	got := parseAskIntent(`{"intent":"weird"}`, "603986.SH")
	if got.Intent != "general" || got.ReuseLast {
		t.Fatalf("%+v", got)
	}
}

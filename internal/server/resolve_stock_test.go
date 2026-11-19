package server

import "testing"

func TestPreferHKQuery(t *testing.T) {
	if preferHKQuery("三花智控") {
		t.Fatal("name should prefer A-share")
	}
	if preferHKQuery("002050") {
		t.Fatal("6-digit code is A-share form")
	}
	for _, q := range []string{"02050.HK", "700.HK", "00700", "700", "腾讯港股", "三花 H股"} {
		if !preferHKQuery(q) {
			t.Fatalf("%q should prefer HK", q)
		}
	}
}

func TestPickSearchHitPrefersAShare(t *testing.T) {
	items := []struct {
		TSCode string `json:"ts_code"`
		Name   string `json:"name"`
	}{
		{TSCode: "02050.HK", Name: "三花智控"},
		{TSCode: "002050.SZ", Name: "三花智控"},
	}
	code, name := pickSearchHit("三花智控", items)
	if code != "002050.SZ" || name != "三花智控" {
		t.Fatalf("got %s/%s", code, name)
	}
	code, _ = pickSearchHit("02050.HK", items)
	if code != "02050.HK" {
		t.Fatalf("explicit HK kept %s", code)
	}
}

func TestResolveStockIndexAliases(t *testing.T) {
	s := &Server{}
	cases := []struct {
		q, code, name string
	}{
		{"创业板指", "399006.SZ", "创业板指"},
		{"创业板", "399006.SZ", "创业板指"},
		{"创指", "399006.SZ", "创业板指"},
		{"上证", "000001.SH", "上证指数"},
		{"上证指数", "000001.SH", "上证指数"},
		{"深成指", "399001.SZ", "深证成指"},
		{"沪深300", "000300.SH", "沪深300"},
		{"399006.SZ", "399006.SZ", "创业板指"},
		{"000001.SH", "000001.SH", "上证指数"},
	}
	for _, tc := range cases {
		code, name, err := s.resolveStock(tc.q)
		if err != nil {
			t.Fatalf("%q: %v", tc.q, err)
		}
		if code != tc.code || name != tc.name {
			t.Fatalf("%q => %s/%s, want %s/%s", tc.q, code, name, tc.code, tc.name)
		}
	}
}

func TestLookupNameIndex(t *testing.T) {
	s := &Server{}
	if got := s.lookupName("399006.SZ"); got != "创业板指" {
		t.Fatalf("lookupName(399006.SZ)=%q", got)
	}
}

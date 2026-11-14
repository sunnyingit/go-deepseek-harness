package server

import "testing"

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

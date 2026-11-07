package quotes

import (
	"strings"
	"testing"
)

const sample = `v_sh600036="1~招商银行~600036~39.50~39.20~39.30~1~1";
v_sz000001="1~平安银行~000001~0.00~11.20~0~0~0";
v_sz300750="1~宁德时代~300750~250.00~245.00~1~1";`

func TestSymbolAndParseSkipHaltedStocks(t *testing.T) {
	if Symbol("600036.SH") != "sh600036" || Symbol("700.HK") != "hk00700" || Symbol("bad") != "" {
		t.Fatal("symbols wrong")
	}
	got := Parse(sample)
	if len(got) != 2 {
		t.Fatalf("got = %+v", got)
	}
	if q := got["sh600036"]; q.Price != 39.5 || q.Change != 0.3 || q.ChangePct != 0.77 {
		t.Fatalf("q = %+v", q)
	}
	if _, ok := got["sz000001"]; ok {
		t.Fatal("kept a halted stock")
	}
}

func TestFetchBatchesAndCachesIncludingMisses(t *testing.T) {
	c := New()
	var urls []string
	c.Get = func(u string) (string, error) { urls = append(urls, u); return sample, nil }
	out, err := c.Fetch([]string{"600036.SH", "000001.SZ", "300750.SZ", "weird"})
	if err != nil || len(out) != 2 || out["300750.SZ"].Price != 250 {
		t.Fatalf("out = %+v err = %v", out, err)
	}
	if len(urls) != 1 || !strings.Contains(urls[0], "sh600036,sz000001,sz300750") {
		t.Fatalf("urls = %v", urls)
	}
	c.Fetch([]string{"600036.SH", "000001.SZ"})
	if len(urls) != 1 {
		t.Fatal("second call was not served from the cache")
	}
}

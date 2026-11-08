package quotes

import (
	"errors"
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

func TestQuotesAreAskedOfTheServiceFirstAndTheSourceFillsTheGaps(t *testing.T) {
	c := New()
	direct := 0
	c.Get = func(url string) (string, error) {
		direct++
		if !strings.Contains(url, "sz000001") || strings.Contains(url, "sh600036") {
			t.Fatalf("the source was asked for %s", url)
		}
		return `v_sz000001="1~平安银行~000001~11.00~10.50~1";`, nil
	}
	asked := 0
	c.Via = func(codes []string) (map[string]Quote, error) {
		asked++
		return map[string]Quote{"600036.SH": {Price: 39.5, Prev: 39.2, Change: 0.3, ChangePct: 0.77}}, nil
	}
	got, err := c.Fetch([]string{"600036.SH", "000001.SZ"})
	if err != nil || got["600036.SH"].Price != 39.5 || got["000001.SZ"].Price != 11 || asked != 1 || direct != 1 {
		t.Fatalf("got %v err %v asked %d direct %d", got, err, asked, direct)
	}
	// both are cached now: nobody is asked again
	if _, err := c.Fetch([]string{"600036.SH", "000001.SZ"}); err != nil || asked != 1 || direct != 1 {
		t.Fatalf("asked again: %d %d", asked, direct)
	}
	// a service that fails or does not have the endpoint costs nothing but a detour
	c2 := New()
	c2.Get = func(string) (string, error) { return `v_sh600036="1~招商银行~600036~39.50~39.20~1";`, nil }
	c2.Via = func([]string) (map[string]Quote, error) { return nil, errors.New("down") }
	if got, err := c2.Fetch([]string{"600036.SH"}); err != nil || got["600036.SH"].Price != 39.5 {
		t.Fatalf("fallback = %v %v", got, err)
	}
}

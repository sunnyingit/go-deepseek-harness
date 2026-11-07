// Package quotes fetches live prices for a batch of stocks from Tencent's free quote endpoint.
package quotes

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Quote struct {
	Price     float64 `json:"price"`
	Prev      float64 `json:"prev"`
	Change    float64 `json:"change"`
	ChangePct float64 `json:"change_pct"`
}

var line = regexp.MustCompile(`v_(\w+)="([^"]*)"`)

// Symbol maps a ts_code such as 600036.SH to Tencent's symbol (sh600036). Empty when unsupported.
func Symbol(code string) string {
	i := strings.LastIndex(code, ".")
	if i < 0 {
		return ""
	}
	num, suffix := code[:i], strings.ToUpper(code[i+1:])
	switch suffix {
	case "SH":
		return "sh" + num
	case "SZ":
		return "sz" + num
	case "BJ":
		return "bj" + num
	case "HK":
		return "hk" + strings.Repeat("0", max(0, 5-len(num))) + num
	}
	return ""
}

func round(v float64, n int) float64 {
	p := math.Pow10(n)
	return math.Round(v*p) / p
}

// Parse reads Tencent's response into quotes keyed by symbol. Halted or not-yet-open stocks are left out.
func Parse(text string) map[string]Quote {
	out := map[string]Quote{}
	for _, m := range line.FindAllStringSubmatch(text, -1) {
		parts := strings.Split(m[2], "~")
		if len(parts) < 5 {
			continue
		}
		price, err1 := strconv.ParseFloat(parts[3], 64)
		prev, err2 := strconv.ParseFloat(parts[4], 64)
		if err1 != nil || err2 != nil || price <= 0 || prev <= 0 {
			continue
		}
		out[m[1]] = Quote{Price: round(price, 4), Prev: round(prev, 4), Change: round(price-prev, 4), ChangePct: round((price/prev-1)*100, 2)}
	}
	return out
}

// Client fetches quotes; Get is replaceable in tests. Results are cached for a few seconds so a page
// that polls does not hammer the endpoint.
type Client struct {
	Get func(url string) (string, error)
	TTL time.Duration
	now func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at time.Time
	q  Quote
	ok bool
}

func New() *Client {
	c := &Client{TTL: 10 * time.Second, now: time.Now, cache: map[string]cached{}}
	http := &http.Client{Timeout: 6 * time.Second}
	c.Get = func(url string) (string, error) {
		resp, err := http.Get(url)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		return string(raw), err
	}
	return c
}

// Fetch returns quotes keyed by ts_code. Stocks without a quote are absent from the result.
func (c *Client) Fetch(codes []string) (map[string]Quote, error) {
	out := map[string]Quote{}
	var wanted []string
	symbolOf := map[string]string{}
	c.mu.Lock()
	for _, code := range codes {
		sym := Symbol(code)
		if sym == "" {
			continue
		}
		symbolOf[sym] = code
		if hit, ok := c.cache[sym]; ok && c.now().Sub(hit.at) < c.TTL {
			if hit.ok {
				out[code] = hit.q
			}
			continue
		}
		wanted = append(wanted, sym)
	}
	c.mu.Unlock()
	if len(wanted) == 0 {
		return out, nil
	}
	text, err := c.Get("http://qt.gtimg.cn/q=" + strings.Join(wanted, ","))
	if err != nil {
		return out, fmt.Errorf("行情请求失败：%w", err)
	}
	parsed := Parse(text)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, sym := range wanted {
		q, ok := parsed[sym]
		c.cache[sym] = cached{at: c.now(), q: q, ok: ok}
		if ok {
			out[symbolOf[sym]] = q
		}
	}
	return out, nil
}

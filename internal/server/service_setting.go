package server

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"go-harness/internal/quotes"
)

// The address of the data service (the Python service that answers details, the market page and the shared inputs).
// It can be set in the settings page and is kept on this computer. A saved address wins over the STOCK_API_BASE
// environment variable, which wins over the local default. Everything that talks to the service, the analysts'
// tools included, reads the address through stockAPIBase(), which looks at the environment; so a saved address is
// applied by putting it there.

var (
	serviceMu     sync.Mutex
	serviceSaved  string
	serviceEnv    string
	serviceEnvOk  bool
	serviceFile   string
	serviceUserID string
)

func serviceSource() string {
	serviceMu.Lock()
	defer serviceMu.Unlock()
	switch {
	case serviceSaved != "":
		return "saved"
	case serviceEnv != "":
		return "env"
	}
	return "default"
}

// loadServiceSetting reads the saved address and applies it.
func loadServiceSetting(path string) {
	serviceMu.Lock()
	serviceFile = path
	serviceUserID = ""
	if !serviceEnvOk {
		serviceEnv, serviceEnvOk = strings.TrimSpace(os.Getenv("STOCK_API_BASE")), true
	}
	serviceMu.Unlock()
	if path == "" {
		ensureClientUserID("")
		return
	}
	saved := readServiceDisk(path)
	if saved.StockBase != "" {
		applyServiceBase(saved.StockBase)
	}
	ensureClientUserID(path)
}

func applyServiceBase(base string) {
	serviceMu.Lock()
	defer serviceMu.Unlock()
	serviceSaved = base
	switch {
	case base != "":
		_ = os.Setenv("STOCK_API_BASE", base)
	case serviceEnv != "":
		_ = os.Setenv("STOCK_API_BASE", serviceEnv)
	default:
		_ = os.Unsetenv("STOCK_API_BASE")
	}
}

// cleanServiceBase checks what the user typed: http or https, a host, no path.
func cleanServiceBase(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	if !strings.Contains(text, "://") {
		text = "http://" + text
	}
	u, err := url.Parse(text)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("地址要写成 http://主机:端口 这样")
	}
	if p := strings.Trim(u.Path, "/"); p != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("地址只写到主机和端口，后面的路径不用写")
	}
	return u.Scheme + "://" + u.Host, nil
}

func saveServiceSetting(base string) error {
	applyServiceBase(base)
	serviceMu.Lock()
	path := serviceFile
	uid := serviceUserID
	serviceMu.Unlock()
	if uid == "" {
		uid = ensureClientUserID(path)
	}
	return writeServiceDisk(path, serviceDisk{StockBase: base, UserID: uid})
}

func serviceView() map[string]any {
	serviceMu.Lock()
	saved := serviceSaved
	serviceMu.Unlock()
	return map[string]any{
		"base_url":  saved,
		"effective": stockAPIBase(),
		"source":    serviceSource(),
		"user_id":   clientUserID(),
	}
}

// probe asks a service whether it answers, and whether it has the quotes endpoint.
func probe(base string) map[string]any {
	client := &http.Client{Timeout: 5 * time.Second}
	started := time.Now()
	out := map[string]any{"base": base}
	resp, err := client.Get(base + "/healthz")
	if err != nil {
		out["ok"], out["error"] = false, "连不上："+err.Error()
		return out
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound { // an older service has no /healthz: any shared endpoint will do
		if r2, err := client.Get(base + "/api/v1/shared/calendar"); err == nil {
			r2.Body.Close()
			resp = r2
		}
	}
	out["ms"] = time.Since(started).Milliseconds()
	out["ok"] = resp.StatusCode == http.StatusOK
	if resp.StatusCode != http.StatusOK {
		out["error"] = fmt.Sprintf("服务返回 %d，这个地址可能不是股票数据服务", resp.StatusCode)
		return out
	}
	if r3, err := client.Get(base + "/api/v1/shared/quotes?codes=600036.SH"); err == nil {
		r3.Body.Close()
		out["has_quotes"] = r3.StatusCode != http.StatusNotFound
	}
	return out
}

func (s *Server) registerServiceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings/service", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, serviceView())
	})
	mux.HandleFunc("PUT /api/v1/settings/service", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			BaseURL string `json:"base_url"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求内容不对"})
			return
		}
		base, err := cleanServiceBase(body.BaseURL)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if err := saveServiceSetting(base); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存失败：" + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, serviceView())
	})
	mux.HandleFunc("POST /api/v1/settings/service/test", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			BaseURL string `json:"base_url"`
		}
		_ = readJSON(r, &body)
		base, err := cleanServiceBase(body.BaseURL)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if base == "" {
			base = stockAPIBase()
		}
		writeJSON(w, http.StatusOK, probe(base))
	})
}

// ---- quotes from the data service

var (
	quotesDownMu    sync.Mutex
	quotesDownUntil time.Time
)

// backendQuotes asks the data service for the latest prices. After a failure it is left alone for half a minute,
// so a service that is down or has no such endpoint does not slow every page by its timeout.
func (s *Server) backendQuotes(codes []string) (map[string]quotes.Quote, error) {
	quotesDownMu.Lock()
	down := time.Now().Before(quotesDownUntil)
	quotesDownMu.Unlock()
	if down || len(codes) == 0 {
		return nil, fmt.Errorf("数据服务的行情暂时不用")
	}
	out := map[string]quotes.Quote{}
	for start := 0; start < len(codes); start += 60 {
		end := min(start+60, len(codes))
		var body struct {
			Items map[string]struct {
				Price     float64 `json:"price"`
				Prev      float64 `json:"prev"`
				Change    float64 `json:"change"`
				ChangePct float64 `json:"change_pct"`
			} `json:"items"`
		}
		if err := stockGetJSONWithin("/api/v1/shared/quotes?codes="+url.QueryEscape(strings.Join(codes[start:end], ",")), &body, 4*time.Second); err != nil {
			quotesDownMu.Lock()
			quotesDownUntil = time.Now().Add(30 * time.Second)
			quotesDownMu.Unlock()
			return out, err
		}
		for code, q := range body.Items {
			out[strings.ToUpper(code)] = quotes.Quote{Price: q.Price, Prev: q.Prev, Change: q.Change, ChangePct: q.ChangePct}
		}
	}
	return out, nil
}

// stockMeta is the line every page reads the data service's address from.
var stockMeta = regexp.MustCompile(`(<meta name="stock-api-base" content=")[^"]*(")`)

func withServiceBase(page []byte) []byte {
	if !strings.Contains(string(page), `name="stock-api-base"`) {
		return page
	}
	out := stockMeta.ReplaceAll(page, []byte("${1}"+strings.ReplaceAll(stockAPIBase(), "$", "$$")+"${2}"))
	uid := clientUserID()
	inject := `<meta name="stock-user-id" content="` + uid + `">` +
		`<script>(function(){var id=(document.querySelector('meta[name="stock-user-id"]')||{}).content||"";if(!id)return;` +
		`try{localStorage.setItem("stock_user_id",id)}catch(e){}` +
		`var api=((document.querySelector('meta[name="stock-api-base"]')||{}).content||"").replace(/\/$/,"");` +
		`var orig=window.fetch;window.fetch=function(input,init){` +
		`var url=typeof input==="string"?input:(input&&input.url)||"";var abs;try{abs=new URL(url,location.href).href}catch(e){return orig.apply(this,arguments)}` +
		`if(api&&abs.indexOf(api)===0){init=Object.assign({},init||{});var h=new Headers(init.headers||(input&&input.headers)||undefined);` +
		`if(!h.has("X-User-Id")&&!h.has("Authorization"))h.set("X-User-Id",id);init.headers=h}` +
		`return orig.call(this,input,init)};})();</script>`
	html := string(out)
	if i := strings.Index(html, "</head>"); i >= 0 {
		return []byte(html[:i] + inject + html[i:])
	}
	return append([]byte(inject), out...)
}

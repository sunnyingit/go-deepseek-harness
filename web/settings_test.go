package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigAPI(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg.APIKey = "sk-live-oldkey1234"
	s.cfg.Model = "deepseek-chat"
	s.cfg.Port = 8081
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
server:
  port: 8083
llm:
  api_key: "sk-secret-abcdefgh"
  model: deepseek-chat
sandbox:
  bash_mode: danger-full-access
`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.configPath = path
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)

	t.Run("get masks api key", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/config")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		var env settingsEnvelope
		if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
			t.Fatal(err)
		}
		if env.Path != path {
			t.Fatalf("path = %q", env.Path)
		}
		if strings.Contains(env.Config.LLM.APIKey, "secret") {
			t.Fatalf("api key leaked: %q", env.Config.LLM.APIKey)
		}
		if !env.Config.LLM.APIKeySet || !strings.Contains(env.Config.LLM.APIKey, "•") {
			t.Fatalf("masked key = %+v", env.Config.LLM)
		}
		if env.Config.Server.Port != 8083 {
			t.Fatalf("port = %d", env.Config.Server.Port)
		}
	})

	t.Run("put keeps key when blank and updates model", func(t *testing.T) {
		body := `{
		  "server": {"port": 8083, "data_dir": "", "shutdown_timeout": "60s"},
		  "llm": {"api_key": "", "model": "deepseek-reasoner", "base_url": "", "thinking": "", "max_tokens": 0, "retry": {"mode": "normal", "max_retries": 5, "initial_delay": "500ms", "max_delay": "10s", "retryable_codes": ["TIMEOUT"]}},
		  "sandbox": {"bash_mode": "workspace-write", "python_mode": "workspace-write", "fs_mode": "workspace-write", "bash_network": true, "auto_approve_escalation": true},
		  "python": {"bin": "python3", "run_timeout": "300s", "install_timeout": "5m"},
		  "tools": {"bash_timeout": "120s", "max_upload_bytes": 67108864, "max_parallel_tool_calls": 10},
		  "agent": {"auto_approve_plan": true, "system_prompt": ""}
		}`
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/config", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d body = %s", res.StatusCode, raw)
		}
		var env settingsEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		if !env.Saved || env.Config.LLM.Model != "deepseek-reasoner" {
			t.Fatalf("saved envelope = %+v", env)
		}
		written, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(written, []byte("sk-secret-abcdefgh")) {
			t.Fatalf("yaml lost api key:\n%s", written)
		}
		if !bytes.Contains(written, []byte("deepseek-reasoner")) {
			t.Fatalf("yaml missing model:\n%s", written)
		}
		if s.cfg.Model != "deepseek-reasoner" {
			t.Fatalf("live model = %q", s.cfg.Model)
		}
		if string(s.cfg.BashMode) != "workspace-write" {
			t.Fatalf("live bash mode = %s", s.cfg.BashMode)
		}
	})

	t.Run("put rejects bad sandbox mode", func(t *testing.T) {
		body := `{"server":{"port":8083},"llm":{"api_key":"sk-ok-12345678","model":"deepseek-chat","retry":{"mode":"normal"}},"sandbox":{"bash_mode":"explode"},"python":{},"tools":{},"agent":{}}`
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/config", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", res.StatusCode)
		}
	})

	t.Run("settings page is served", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/settings.html")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		body, _ := io.ReadAll(res.Body)
		if !bytes.Contains(body, []byte("保存到 config.yaml")) {
			t.Fatalf("page missing save button")
		}
	})
}

func TestGetConfigWithoutPath(t *testing.T) {
	s, _ := newTestServer(t)
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/config")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestMaskAPIKey(t *testing.T) {
	if got := maskAPIKey("sk-secret-abcdefgh"); !strings.HasPrefix(got, "sk-") || strings.Contains(got, "secret") {
		t.Fatalf("mask = %q", got)
	}
	if keepAPIKey("", "old") != "old" || keepAPIKey("sk-••••wxyz", "old") != "old" {
		t.Fatal("blank/masked submit should keep previous")
	}
	if keepAPIKey("sk-new-key", "old") != "sk-new-key" {
		t.Fatal("new key should replace")
	}
}

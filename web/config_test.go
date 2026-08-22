package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go-harness/llm/llmretry"
	"go-harness/sandbox/sandbox"
	"go-harness/tools/python"
)

func TestParseConfigYAMLUsesCheckedInDefaults(t *testing.T) {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	cfg, err := parseConfigYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8081 || cfg.Addr != ":8081" {
		t.Fatalf("port/addr = %d %q", cfg.Port, cfg.Addr)
	}
	if cfg.Model != "deepseek-v4-pro" {
		t.Fatalf("model = %q", cfg.Model)
	}
	if cfg.BashMode != sandbox.DangerFullAccess || !cfg.BashNetwork {
		t.Fatalf("bash sandbox = %s network=%v", cfg.BashMode, cfg.BashNetwork)
	}
	if cfg.PythonMode != sandbox.DangerFullAccess || cfg.FSMode != sandbox.DangerFullAccess {
		t.Fatalf("python/fs mode = %s / %s", cfg.PythonMode, cfg.FSMode)
	}
	if cfg.Retry.MaxRetries != 5 || cfg.Retry.Mode != llmretry.ModeNormal {
		t.Fatalf("retry = %+v", cfg.Retry)
	}
	if cfg.StreamIdleTimeout != 10*time.Minute {
		t.Fatalf("stream idle = %s", cfg.StreamIdleTimeout)
	}
	if cfg.ConnectTimeout != 100*time.Second || cfg.TLSHandshakeTimeout != 100*time.Second {
		t.Fatalf("connect/tls = %s / %s", cfg.ConnectTimeout, cfg.TLSHandshakeTimeout)
	}
	if cfg.BashTimeout != 120*time.Second || cfg.PythonRunTimeout != 300*time.Second {
		t.Fatalf("tool timeouts bash=%s python=%s", cfg.BashTimeout, cfg.PythonRunTimeout)
	}
	if cfg.MaxUploadBytes != 64<<20 {
		t.Fatalf("upload = %d", cfg.MaxUploadBytes)
	}
	if len(cfg.PythonPackages) != len(python.DefaultDocumentPackages) {
		t.Fatalf("packages = %v", cfg.PythonPackages)
	}
	if cfg.SystemPrompt != "" {
		t.Fatalf("empty yaml system_prompt should stay empty (assembled at runtime), got %q", cfg.SystemPrompt)
	}
}

func TestParseConfigYAMLOverridesAndModes(t *testing.T) {
	raw := []byte(`
server:
  port: 9090
  data_dir: /tmp/harness-data
  shutdown_timeout: 3s
llm:
  api_key: from-yaml
  base_url: https://example.invalid
  model: deepseek-reasoner
  stream_idle_timeout: 90s
  connect_timeout: 5s
  tls_handshake_timeout: 2s
  max_tokens: 1024
  temperature: 0.2
  retry:
    mode: always
    max_retries: 2
    initial_delay: 100ms
    max_delay: 1s
    jitter_ratio: 0.5
sandbox:
  bash_mode: workspace-write
  bash_network: false
  python_mode: read-only
  fs_mode: workspace-write
  auto_approve_escalation: false
python:
  cache_dir: /tmp/py
  bin: python3.12
  install_timeout: 1m
  run_timeout: 10s
  packages: [openpyxl]
tools:
  bash_timeout: 15s
  max_upload_bytes: 1024
  max_parallel_tool_calls: 3
agent:
  auto_approve_plan: false
  system_prompt: custom prompt
`)
	cfg, err := parseConfigYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "from-yaml" || cfg.Port != 9090 || cfg.DataDir != "/tmp/harness-data" {
		t.Fatalf("server/llm = %+v", cfg)
	}
	if cfg.BashMode != sandbox.WorkspaceWrite || cfg.BashNetwork || cfg.PythonMode != sandbox.ReadOnly {
		t.Fatalf("sandbox modes bash=%s net=%v python=%s", cfg.BashMode, cfg.BashNetwork, cfg.PythonMode)
	}
	if cfg.Retry.Mode != llmretry.ModeAlways || cfg.Retry.MaxRetries != 2 {
		t.Fatalf("retry = %+v", cfg.Retry)
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0.2 || cfg.MaxTokens != 1024 {
		t.Fatalf("sampling temp=%v tokens=%d", cfg.Temperature, cfg.MaxTokens)
	}
	if cfg.StreamIdleTimeout != 90*time.Second || cfg.ConnectTimeout != 5*time.Second || cfg.TLSHandshakeTimeout != 2*time.Second {
		t.Fatalf("llm timeouts idle=%s connect=%s tls=%s", cfg.StreamIdleTimeout, cfg.ConnectTimeout, cfg.TLSHandshakeTimeout)
	}
	if len(cfg.PythonPackages) != 1 || cfg.PythonPackages[0] != "openpyxl" {
		t.Fatalf("packages = %v", cfg.PythonPackages)
	}
	if cfg.AutoApprovePlan || cfg.AutoApproveEscalation {
		t.Fatal("auto-approve flags should be false")
	}
	if cfg.SystemPrompt != "custom prompt" {
		t.Fatalf("prompt = %q", cfg.SystemPrompt)
	}
	if cfg.MaxParallelToolCalls != 3 || cfg.MaxUploadBytes != 1024 {
		t.Fatalf("tools = parallel=%d upload=%d", cfg.MaxParallelToolCalls, cfg.MaxUploadBytes)
	}
}

func TestParseConfigYAMLRejectsUnknownSandboxMode(t *testing.T) {
	_, err := parseConfigYAML([]byte("sandbox:\n  bash_mode: wide-open\n"))
	if err == nil {
		t.Fatal("want an error for an unknown sandbox mode")
	}
}

func TestEnvOverridesYAML(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "from-env")
	t.Setenv("DEEPSEEK_MODEL", "env-model")
	t.Setenv("PORT", "7777")
	t.Setenv("WEB_DEMO_DATA_DIR", "/tmp/from-env")
	t.Setenv("WEB_DEMO_PYTHON_CACHE_DIR", "/tmp/py-env")
	t.Setenv("HARNESS_PYTHON_BIN", "/opt/runtime/python3")
	cfg, err := decodeConfig([]byte("llm:\n  api_key: from-yaml\n  model: yaml-model\nserver:\n  port: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	applyEnvOverrides(&cfg)
	finalizeConfig(&cfg)
	if cfg.APIKey != "from-env" || cfg.Model != "env-model" || cfg.Port != 7777 {
		t.Fatalf("env overlay failed: key=%s model=%s port=%d", cfg.APIKey, cfg.Model, cfg.Port)
	}
	if cfg.DataDir != "/tmp/from-env" || cfg.PythonCacheDir != "/tmp/py-env" {
		t.Fatalf("dirs = %s %s", cfg.DataDir, cfg.PythonCacheDir)
	}
	if cfg.PythonBin != "/opt/runtime/python3" {
		t.Fatalf("python bin = %q", cfg.PythonBin)
	}
}

func TestLoadConfigFileAndFindPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("server:\n  port: 8123\nllm:\n  api_key: k\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("DEEPSEEK_BASE_URL", "")
	t.Setenv("DEEPSEEK_MODEL", "")
	t.Setenv("PORT", "")
	t.Setenv("WEB_DEMO_DATA_DIR", "")
	t.Setenv("WEB_DEMO_PYTHON_CACHE_DIR", "")
	t.Setenv("HARNESS_PYTHON_BIN", "")
	cfg, err := loadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8123 || cfg.APIKey != "k" {
		t.Fatalf("loaded = port=%d key=%q", cfg.Port, cfg.APIKey)
	}

	t.Setenv("WEB_DEMO_CONFIG", path)
	got, err := findConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("findConfigPath = %q, want %q", got, path)
	}
}

func TestLoadConfigRequiresAPIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("WEB_DEMO_CONFIG", path)
	if _, _, err := loadConfig(path); err == nil {
		t.Fatal("want missing api key to fail")
	}
}

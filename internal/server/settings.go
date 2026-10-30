package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go-harness/internal/appconfig"
	"go-harness/llm/deepseek"

	"gopkg.in/yaml.v3"
)

type settingsEnvelope struct {
	Path          string          `json:"path"`
	EnvOverrides  []string        `json:"envOverrides,omitempty"`
	RestartHint   string          `json:"restartHint,omitempty"`
	Config        settingsPayload `json:"config"`
	Saved         bool            `json:"saved,omitempty"`
	RestartNeeded bool            `json:"restartNeeded,omitempty"`
}

type settingsPayload struct {
	Server  settingsServer   `json:"server"`
	LLM     settingsLLM      `json:"llm"`
	Sandbox *settingsSandbox `json:"sandbox,omitempty"`
	Python  *settingsPython  `json:"python,omitempty"`
	Tools   settingsTools    `json:"tools"`
	Agent   settingsAgent    `json:"agent"`
}

type settingsServer struct {
	Port            int    `json:"port"`
	DataDir         string `json:"data_dir"`
	WorkspaceDir    string `json:"workspace_dir"`
	ShutdownTimeout string `json:"shutdown_timeout"`
}

type settingsLLM struct {
	APIKey              string        `json:"api_key"`
	APIKeySet           bool          `json:"api_key_set"`
	BaseURL             string        `json:"base_url"`
	Model               string        `json:"model"`
	StreamIdleTimeout   string        `json:"stream_idle_timeout"`
	ConnectTimeout      string        `json:"connect_timeout"`
	TLSHandshakeTimeout string        `json:"tls_handshake_timeout"`
	Thinking            string        `json:"thinking"`
	MaxTokens           int           `json:"max_tokens"`
	Temperature         *float64      `json:"temperature"`
	TopP                *float64      `json:"top_p"`
	ReasoningEffort     string        `json:"reasoning_effort"`
	Retry               settingsRetry `json:"retry"`
}

type settingsRetry struct {
	Mode           string   `json:"mode"`
	MaxRetries     int      `json:"max_retries"`
	InitialDelay   string   `json:"initial_delay"`
	MaxDelay       string   `json:"max_delay"`
	JitterRatio    float64  `json:"jitter_ratio"`
	RetryableCodes []string `json:"retryable_codes"`
}

type settingsSandbox struct {
	BashMode              string `json:"bash_mode"`
	BashNetwork           bool   `json:"bash_network"`
	PythonMode            string `json:"python_mode"`
	FSMode                string `json:"fs_mode"`
	AutoApproveEscalation bool   `json:"auto_approve_escalation"`
}

type settingsPython struct {
	CacheDir       string   `json:"cache_dir"`
	Bin            string   `json:"bin"`
	InstallTimeout string   `json:"install_timeout"`
	RunTimeout     string   `json:"run_timeout"`
	Packages       []string `json:"packages"`
}

type settingsTools struct {
	BashTimeout          string `json:"bash_timeout"`
	MaxUploadBytes       int64  `json:"max_upload_bytes"`
	MaxParallelToolCalls int    `json:"max_parallel_tool_calls"`
}

type settingsAgent struct {
	AutoApprovePlan bool   `json:"auto_approve_plan"`
	SystemPrompt    string `json:"system_prompt"`
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if s.configPath == "" {
		writeError(w, http.StatusServiceUnavailable, errors.New("config path unavailable"))
		return
	}
	file, err := s.readConfigFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.configEnvelope(file, false, false))
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if s.configPath == "" {
		writeError(w, http.StatusServiceUnavailable, errors.New("config path unavailable"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload settingsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json: %w", err))
		return
	}
	prev, err := s.readConfigFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	file, err := payloadToFile(payload, prev)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	next, err := runtimeConfigFromFile(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if next.APIKey == "" {
		writeError(w, http.StatusBadRequest, errors.New("llm.api_key 不能为空（也可改用环境变量 DEEPSEEK_API_KEY）"))
		return
	}
	if err := writeConfigYAML(s.configPath, file); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	restart := next.Port != s.cfg.Port || next.DataDir != s.cfg.DataDir || next.PythonCacheDir != s.cfg.PythonCacheDir
	if err := s.applyLiveConfig(next); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("已写入文件，但热加载失败：%w", err))
		return
	}
	writeJSON(w, http.StatusOK, s.configEnvelope(file, true, restart))
}

func (s *Server) readConfigFile() (appconfig.FileConfig, error) {
	if s.configPath == "" {
		return appconfig.FileConfig{}, errors.New("config path unavailable")
	}
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return appconfig.FileConfig{}, fmt.Errorf("reading %s: %w", s.configPath, err)
	}
	var file appconfig.FileConfig
	if err := yaml.Unmarshal(data, &file); err != nil {
		return appconfig.FileConfig{}, fmt.Errorf("parsing %s: %w", s.configPath, err)
	}
	return file, nil
}

func (s *Server) configEnvelope(file appconfig.FileConfig, saved, restart bool) settingsEnvelope {
	env := []string{}
	if os.Getenv("DEEPSEEK_API_KEY") != "" {
		env = append(env, "DEEPSEEK_API_KEY 会在启动时覆盖 yaml 里的密钥")
	}
	if os.Getenv("DEEPSEEK_BASE_URL") != "" {
		env = append(env, "DEEPSEEK_BASE_URL 会在启动时覆盖 yaml 里的接口地址")
	}
	if os.Getenv("WEB_DEMO_DATA_DIR") != "" {
		env = append(env, "WEB_DEMO_DATA_DIR 会在启动时覆盖数据目录")
	}
	if os.Getenv("WEB_DEMO_WORKSPACE_DIR") != "" {
		env = append(env, "WEB_DEMO_WORKSPACE_DIR 会在启动时覆盖工作目录")
	}
	out := settingsEnvelope{
		Path:          s.configPath,
		EnvOverrides:  env,
		Config:        fileToPayload(file),
		Saved:         saved,
		RestartNeeded: restart,
	}
	if restart {
		out.RestartHint = "端口、数据目录或 Python 缓存已改，需重启服务后完全生效。"
	}
	return out
}

func (s *Server) applyLiveConfig(next appconfig.Config) error {
	llm, err := deepseek.New(deepseek.Config{
		APIKey:              next.APIKey,
		BaseURL:             next.BaseURL,
		Thinking:            next.Thinking,
		StreamIdleTimeout:   next.StreamIdleTimeout,
		ConnectTimeout:      next.ConnectTimeout,
		TLSHandshakeTimeout: next.TLSHandshakeTimeout,
	})
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	s.cfg = next
	s.llm = llm
	s.cfgMu.Unlock()
	return nil
}

func runtimeConfigFromFile(file appconfig.FileConfig) (appconfig.Config, error) {
	cfg := appconfig.Default()
	if err := appconfig.ApplyFile(&cfg, file); err != nil {
		return appconfig.Config{}, err
	}
	appconfig.Finalize(&cfg)
	return cfg, nil
}

func fileToPayload(f appconfig.FileConfig) settingsPayload {
	bashNet := true
	if f.Sandbox.BashNetwork != nil {
		bashNet = *f.Sandbox.BashNetwork
	}
	esc := true
	if f.Sandbox.AutoApproveEscalation != nil {
		esc = *f.Sandbox.AutoApproveEscalation
	}
	plan := true
	if f.Agent.AutoApprovePlan != nil {
		plan = *f.Agent.AutoApprovePlan
	}
	return settingsPayload{
		Server: settingsServer{
			Port:            f.Server.Port,
			DataDir:         f.Server.DataDir,
			WorkspaceDir:    f.Server.WorkspaceDir,
			ShutdownTimeout: durationString(f.Server.ShutdownTimeout),
		},
		LLM: settingsLLM{
			APIKey:              maskAPIKey(f.LLM.APIKey),
			APIKeySet:           strings.TrimSpace(f.LLM.APIKey) != "",
			BaseURL:             f.LLM.BaseURL,
			Model:               f.LLM.Model,
			StreamIdleTimeout:   durationString(f.LLM.StreamIdleTimeout),
			ConnectTimeout:      durationString(f.LLM.ConnectTimeout),
			TLSHandshakeTimeout: durationString(f.LLM.TLSHandshakeTimeout),
			Thinking:            f.LLM.Thinking,
			MaxTokens:           f.LLM.MaxTokens,
			Temperature:         f.LLM.Temperature,
			TopP:                f.LLM.TopP,
			ReasoningEffort:     f.LLM.ReasoningEffort,
			Retry: settingsRetry{
				Mode:           f.LLM.Retry.Mode,
				MaxRetries:     f.LLM.Retry.MaxRetries,
				InitialDelay:   durationString(f.LLM.Retry.InitialDelay),
				MaxDelay:       durationString(f.LLM.Retry.MaxDelay),
				JitterRatio:    f.LLM.Retry.JitterRatio,
				RetryableCodes: append([]string(nil), f.LLM.Retry.RetryableCodes...),
			},
		},
		Sandbox: &settingsSandbox{
			BashMode:              f.Sandbox.BashMode,
			BashNetwork:           bashNet,
			PythonMode:            f.Sandbox.PythonMode,
			FSMode:                f.Sandbox.FSMode,
			AutoApproveEscalation: esc,
		},
		Python: &settingsPython{
			CacheDir:       f.Python.CacheDir,
			Bin:            f.Python.Bin,
			InstallTimeout: durationString(f.Python.InstallTimeout),
			RunTimeout:     durationString(f.Python.RunTimeout),
			Packages:       append([]string(nil), f.Python.Packages...),
		},
		Tools: settingsTools{
			BashTimeout:          durationString(f.Tools.BashTimeout),
			MaxUploadBytes:       f.Tools.MaxUploadBytes,
			MaxParallelToolCalls: f.Tools.MaxParallelToolCalls,
		},
		Agent: settingsAgent{
			AutoApprovePlan: plan,
			SystemPrompt:    f.Agent.SystemPrompt,
		},
	}
}

func payloadToFile(p settingsPayload, prev appconfig.FileConfig) (appconfig.FileConfig, error) {
	shut, err := parseDurationField(p.Server.ShutdownTimeout)
	if err != nil {
		return appconfig.FileConfig{}, fmt.Errorf("server.shutdown_timeout: %w", err)
	}
	idle, err := parseDurationField(p.LLM.StreamIdleTimeout)
	if err != nil {
		return appconfig.FileConfig{}, fmt.Errorf("llm.stream_idle_timeout: %w", err)
	}
	conn, err := parseDurationField(p.LLM.ConnectTimeout)
	if err != nil {
		return appconfig.FileConfig{}, fmt.Errorf("llm.connect_timeout: %w", err)
	}
	tlsTO, err := parseDurationField(p.LLM.TLSHandshakeTimeout)
	if err != nil {
		return appconfig.FileConfig{}, fmt.Errorf("llm.tls_handshake_timeout: %w", err)
	}
	initD, err := parseDurationField(p.LLM.Retry.InitialDelay)
	if err != nil {
		return appconfig.FileConfig{}, fmt.Errorf("llm.retry.initial_delay: %w", err)
	}
	maxD, err := parseDurationField(p.LLM.Retry.MaxDelay)
	if err != nil {
		return appconfig.FileConfig{}, fmt.Errorf("llm.retry.max_delay: %w", err)
	}
	bashTO, err := parseDurationField(p.Tools.BashTimeout)
	if err != nil {
		return appconfig.FileConfig{}, fmt.Errorf("tools.bash_timeout: %w", err)
	}
	plan := p.Agent.AutoApprovePlan
	effort := strings.TrimSpace(p.LLM.ReasoningEffort)
	if effort == "" {
		effort = prev.LLM.ReasoningEffort
	}
	out := appconfig.FileConfig{
		Server: appconfig.FileServer{
			Port:            p.Server.Port,
			DataDir:         strings.TrimSpace(p.Server.DataDir),
			WorkspaceDir:    strings.TrimSpace(p.Server.WorkspaceDir),
			ShutdownTimeout: shut,
		},
		LLM: appconfig.FileLLM{
			APIKey:              keepAPIKey(p.LLM.APIKey, prev.LLM.APIKey),
			BaseURL:             strings.TrimSpace(p.LLM.BaseURL),
			Model:               strings.TrimSpace(p.LLM.Model),
			StreamIdleTimeout:   idle,
			ConnectTimeout:      conn,
			TLSHandshakeTimeout: tlsTO,
			Thinking:            strings.TrimSpace(p.LLM.Thinking),
			MaxTokens:           p.LLM.MaxTokens,
			Temperature:         p.LLM.Temperature,
			TopP:                p.LLM.TopP,
			ReasoningEffort:     effort,
			Retry: appconfig.FileRetry{
				Mode:           strings.TrimSpace(p.LLM.Retry.Mode),
				MaxRetries:     p.LLM.Retry.MaxRetries,
				InitialDelay:   initD,
				MaxDelay:       maxD,
				JitterRatio:    p.LLM.Retry.JitterRatio,
				RetryableCodes: compactStrings(p.LLM.Retry.RetryableCodes),
			},
		},
		Sandbox: prev.Sandbox,
		Python:  prev.Python,
		Tools: appconfig.FileTools{
			BashTimeout:          bashTO,
			MaxUploadBytes:       p.Tools.MaxUploadBytes,
			MaxParallelToolCalls: p.Tools.MaxParallelToolCalls,
		},
		Agent: appconfig.FileAgent{
			AutoApprovePlan: &plan,
			SystemPrompt:    p.Agent.SystemPrompt,
		},
	}
	if p.Sandbox != nil {
		bashNet := p.Sandbox.BashNetwork
		esc := p.Sandbox.AutoApproveEscalation
		out.Sandbox = appconfig.FileSandbox{
			BashMode:              strings.TrimSpace(p.Sandbox.BashMode),
			BashNetwork:           &bashNet,
			PythonMode:            strings.TrimSpace(p.Sandbox.PythonMode),
			FSMode:                strings.TrimSpace(p.Sandbox.FSMode),
			AutoApproveEscalation: &esc,
		}
	}
	if p.Python != nil {
		pyInst, err := parseDurationField(p.Python.InstallTimeout)
		if err != nil {
			return appconfig.FileConfig{}, fmt.Errorf("python.install_timeout: %w", err)
		}
		pyRun, err := parseDurationField(p.Python.RunTimeout)
		if err != nil {
			return appconfig.FileConfig{}, fmt.Errorf("python.run_timeout: %w", err)
		}
		out.Python = appconfig.FilePython{
			CacheDir:       strings.TrimSpace(p.Python.CacheDir),
			Bin:            strings.TrimSpace(p.Python.Bin),
			InstallTimeout: pyInst,
			RunTimeout:     pyRun,
			Packages:       compactStrings(p.Python.Packages),
		}
	}
	return out, nil
}

func writeConfigYAML(path string, file appconfig.FileConfig) error {
	var buf bytes.Buffer
	buf.WriteString("# 由设置页保存。端口、数据目录、Python 缓存需重启后完全生效。\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(file); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func durationString(d appconfig.Duration) string {
	return appconfig.FormatDuration(time.Duration(d))
}

func parseDurationField(raw string) (appconfig.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (use 30s / 5m / 1h)", raw)
	}
	return appconfig.Duration(parsed), nil
}

func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "••••"
	}
	return key[:3] + "••••" + key[len(key)-4:]
}

func keepAPIKey(submitted, previous string) string {
	s := strings.TrimSpace(submitted)
	if s == "" || strings.Contains(s, "•") {
		return previous
	}
	return s
}

func compactStrings(in []string) []string {
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

package appconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-harness/llm/deepseek"
	"go-harness/llm/llmretry"
	"go-harness/sandbox/sandbox"
	"go-harness/tools/python"

	"gopkg.in/yaml.v3"
)

// Duration is time.Duration that unmarshals from YAML strings like "30s".
type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return FormatDuration(time.Duration(d)), nil
}

func (d Duration) Duration() time.Duration { return time.Duration(d) }

// FormatDuration is a compact Go duration (10m, 100s, 500ms) instead of
// time.Duration.String()'s 10m0s / 1m40s.
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return ""
	}
	sign := ""
	if d < 0 {
		sign = "-"
		d = -d
	}
	var body string
	switch {
	case d%time.Hour == 0:
		body = fmt.Sprintf("%dh", d/time.Hour)
	case d >= time.Hour && d%time.Minute == 0:
		body = fmt.Sprintf("%dh%dm", d/time.Hour, (d%time.Hour)/time.Minute)
	case d%time.Minute == 0:
		body = fmt.Sprintf("%dm", d/time.Minute)
	case d%time.Second == 0:
		body = fmt.Sprintf("%ds", d/time.Second)
	case d%time.Millisecond == 0:
		body = fmt.Sprintf("%dms", d/time.Millisecond)
	default:
		body = d.String()
	}
	return sign + body
}

// FileConfig is the on-disk YAML shape. loadConfigFile converts it into the
// runtime config the rest of the process actually reads.
type FileConfig struct {
	Server  FileServer  `yaml:"server"`
	LLM     FileLLM     `yaml:"llm"`
	Sandbox FileSandbox `yaml:"sandbox"`
	Python  FilePython  `yaml:"python"`
	Tools   FileTools   `yaml:"tools"`
	Agent   FileAgent   `yaml:"agent"`
}

type FileServer struct {
	Port            int      `yaml:"port"`
	DataDir         string   `yaml:"data_dir"`
	WorkspaceDir    string   `yaml:"workspace_dir"`
	ShutdownTimeout Duration `yaml:"shutdown_timeout"`
}

type FileLLM struct {
	APIKey              string    `yaml:"api_key"`
	BaseURL             string    `yaml:"base_url"`
	Model               string    `yaml:"model"`
	StreamIdleTimeout   Duration  `yaml:"stream_idle_timeout"`
	ConnectTimeout      Duration  `yaml:"connect_timeout"`
	TLSHandshakeTimeout Duration  `yaml:"tls_handshake_timeout"`
	Thinking            string    `yaml:"thinking"`
	MaxTokens           int       `yaml:"max_tokens"`
	Temperature         *float64  `yaml:"temperature"`
	TopP                *float64  `yaml:"top_p"`
	ReasoningEffort     string    `yaml:"reasoning_effort"`
	Retry               FileRetry `yaml:"retry"`
}

type FileRetry struct {
	Mode           string   `yaml:"mode"`
	MaxRetries     int      `yaml:"max_retries"`
	InitialDelay   Duration `yaml:"initial_delay"`
	MaxDelay       Duration `yaml:"max_delay"`
	JitterRatio    float64  `yaml:"jitter_ratio"`
	RetryableCodes []string `yaml:"retryable_codes"`
}

type FileSandbox struct {
	BashMode              string `yaml:"bash_mode"`
	BashNetwork           *bool  `yaml:"bash_network"`
	PythonMode            string `yaml:"python_mode"`
	PythonNetwork         *bool  `yaml:"python_network"`
	FSMode                string `yaml:"fs_mode"`
	AutoApproveEscalation *bool  `yaml:"auto_approve_escalation"`
}

type FilePython struct {
	CacheDir       string   `yaml:"cache_dir"`
	Bin            string   `yaml:"bin"`
	InstallTimeout Duration `yaml:"install_timeout"`
	RunTimeout     Duration `yaml:"run_timeout"`
	Packages       []string `yaml:"packages"`
}

type FileTools struct {
	BashTimeout          Duration `yaml:"bash_timeout"`
	MaxUploadBytes       int64    `yaml:"max_upload_bytes"`
	MaxParallelToolCalls int      `yaml:"max_parallel_tool_calls"`
}

type FileAgent struct {
	AutoApprovePlan *bool  `yaml:"auto_approve_plan"`
	SystemPrompt    string `yaml:"system_prompt"`
}

// Config is the resolved runtime configuration. Every field the server
// actually uses lives here after YAML + defaults + env overlay.
type Config struct {
	APIKey              string
	BaseURL             string
	Model               string
	Addr                string
	Port                int
	DataDir             string
	WorkspaceDir        string
	PythonCacheDir      string
	ShutdownTimeout     time.Duration
	StreamIdleTimeout   time.Duration
	ConnectTimeout      time.Duration
	TLSHandshakeTimeout time.Duration
	Thinking            string
	MaxTokens           int
	Temperature         *float64
	TopP                *float64
	ReasoningEffort     string
	Retry               llmretry.Policy

	BashMode              sandbox.Mode
	BashNetwork           bool
	PythonMode            sandbox.Mode
	PythonNetwork         bool
	FSMode                sandbox.Mode
	AutoApproveEscalation bool

	PythonBin            string
	PythonInstallTimeout time.Duration
	PythonRunTimeout     time.Duration
	PythonPackages       []string

	BashTimeout          time.Duration
	MaxUploadBytes       int64
	MaxParallelToolCalls int

	AutoApprovePlan bool
	SystemPrompt    string
}

const DefaultUploadBytes int64 = 64 << 20

func Default() Config {
	return Config{
		Model:               "deepseek-v4-pro",
		Port:                8081,
		ShutdownTimeout:     5 * time.Second,
		StreamIdleTimeout:   deepseek.DefaultStreamIdleTimeout,
		ConnectTimeout:      deepseek.DefaultConnectTimeout,
		TLSHandshakeTimeout: deepseek.DefaultTLSHandshakeTimeout,
		Retry:               llmretry.NormalPolicy(),

		BashMode:              sandbox.DangerFullAccess,
		BashNetwork:           true,
		PythonMode:            sandbox.DangerFullAccess,
		PythonNetwork:         false,
		FSMode:                sandbox.DangerFullAccess,
		AutoApproveEscalation: true,

		PythonBin:            "python3",
		PythonInstallTimeout: 5 * time.Minute,
		PythonRunTimeout:     30 * time.Second,

		BashTimeout:          30 * time.Second,
		MaxUploadBytes:       DefaultUploadBytes,
		MaxParallelToolCalls: 10,

		AutoApprovePlan: true,
		SystemPrompt:    "",
	}
}

func parseConfigYAML(data []byte) (Config, error) {
	cfg, err := decodeConfig(data)
	if err != nil {
		return Config{}, err
	}
	Finalize(&cfg)
	return cfg, nil
}

func loadConfigFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading config %s: %w", path, err)
	}
	cfg, err := decodeConfig(data)
	if err != nil {
		return Config{}, err
	}
	applyEnvOverrides(&cfg)
	Finalize(&cfg)
	return cfg, nil
}

func decodeConfig(data []byte) (Config, error) {
	var file FileConfig
	if err := yaml.Unmarshal(data, &file); err != nil {
		return Config{}, fmt.Errorf("parsing config yaml: %w", err)
	}
	cfg := Default()
	if err := ApplyFile(&cfg, file); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Load(explicitPath string) (Config, string, error) {
	path, err := findConfigPath(explicitPath)
	if err != nil {
		return Config{}, "", err
	}
	cfg, err := loadConfigFile(path)
	if err != nil {
		return Config{}, "", err
	}
	if cfg.APIKey == "" {
		return Config{}, "", fmt.Errorf("set llm.api_key in %s or DEEPSEEK_API_KEY", path)
	}
	return cfg, path, nil
}

func findConfigPath(explicit string) (string, error) {
	candidates := []string{}
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if env := os.Getenv("WEB_DEMO_CONFIG"); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates, "config.yaml", filepath.Join("web", "config.yaml"))
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "config.yaml"))
	}
	seen := map[string]bool{}
	var tried []string
	for _, p := range candidates {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		tried = append(tried, p)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("no config.yaml found (looked in: %s); pass -config or set WEB_DEMO_CONFIG", strings.Join(tried, ", "))
}

func ApplyFile(cfg *Config, file FileConfig) error {
	if file.Server.Port > 0 {
		cfg.Port = file.Server.Port
	}
	if file.Server.DataDir != "" {
		cfg.DataDir = file.Server.DataDir
	}
	cfg.WorkspaceDir = strings.TrimSpace(file.Server.WorkspaceDir)
	if file.Server.ShutdownTimeout.Duration() > 0 {
		cfg.ShutdownTimeout = file.Server.ShutdownTimeout.Duration()
	}

	if file.LLM.APIKey != "" {
		cfg.APIKey = file.LLM.APIKey
	}
	if file.LLM.BaseURL != "" {
		cfg.BaseURL = file.LLM.BaseURL
	}
	if file.LLM.Model != "" {
		cfg.Model = file.LLM.Model
	}
	if file.LLM.StreamIdleTimeout.Duration() > 0 {
		cfg.StreamIdleTimeout = file.LLM.StreamIdleTimeout.Duration()
	}
	if file.LLM.ConnectTimeout.Duration() > 0 {
		cfg.ConnectTimeout = file.LLM.ConnectTimeout.Duration()
	}
	if file.LLM.TLSHandshakeTimeout.Duration() > 0 {
		cfg.TLSHandshakeTimeout = file.LLM.TLSHandshakeTimeout.Duration()
	}
	cfg.Thinking = file.LLM.Thinking
	if file.LLM.MaxTokens > 0 {
		cfg.MaxTokens = file.LLM.MaxTokens
	}
	if file.LLM.Temperature != nil {
		cfg.Temperature = file.LLM.Temperature
	}
	if file.LLM.TopP != nil {
		cfg.TopP = file.LLM.TopP
	}
	cfg.ReasoningEffort = file.LLM.ReasoningEffort
	if err := applyRetry(cfg, file.LLM.Retry); err != nil {
		return err
	}

	mode, err := parseSandboxMode(file.Sandbox.BashMode, cfg.BashMode)
	if err != nil {
		return fmt.Errorf("sandbox.bash_mode: %w", err)
	}
	cfg.BashMode = mode
	mode, err = parseSandboxMode(file.Sandbox.PythonMode, cfg.PythonMode)
	if err != nil {
		return fmt.Errorf("sandbox.python_mode: %w", err)
	}
	cfg.PythonMode = mode
	mode, err = parseSandboxMode(file.Sandbox.FSMode, cfg.FSMode)
	if err != nil {
		return fmt.Errorf("sandbox.fs_mode: %w", err)
	}
	cfg.FSMode = mode
	if file.Sandbox.BashNetwork != nil {
		cfg.BashNetwork = *file.Sandbox.BashNetwork
	}
	if file.Sandbox.PythonNetwork != nil {
		cfg.PythonNetwork = *file.Sandbox.PythonNetwork
	}
	if file.Sandbox.AutoApproveEscalation != nil {
		cfg.AutoApproveEscalation = *file.Sandbox.AutoApproveEscalation
	}

	if file.Python.CacheDir != "" {
		cfg.PythonCacheDir = file.Python.CacheDir
	}
	if file.Python.Bin != "" {
		cfg.PythonBin = file.Python.Bin
	}
	if file.Python.InstallTimeout.Duration() > 0 {
		cfg.PythonInstallTimeout = file.Python.InstallTimeout.Duration()
	}
	if file.Python.RunTimeout.Duration() > 0 {
		cfg.PythonRunTimeout = file.Python.RunTimeout.Duration()
	}
	if len(file.Python.Packages) > 0 {
		cfg.PythonPackages = append([]string(nil), file.Python.Packages...)
	}

	if file.Tools.BashTimeout.Duration() > 0 {
		cfg.BashTimeout = file.Tools.BashTimeout.Duration()
	}
	if file.Tools.MaxUploadBytes > 0 {
		cfg.MaxUploadBytes = file.Tools.MaxUploadBytes
	}
	if file.Tools.MaxParallelToolCalls > 0 {
		cfg.MaxParallelToolCalls = file.Tools.MaxParallelToolCalls
	}

	if file.Agent.AutoApprovePlan != nil {
		cfg.AutoApprovePlan = *file.Agent.AutoApprovePlan
	}
	if strings.TrimSpace(file.Agent.SystemPrompt) != "" {
		cfg.SystemPrompt = file.Agent.SystemPrompt
	}
	return nil
}

func applyRetry(cfg *Config, file FileRetry) error {
	policy := cfg.Retry
	mode := strings.ToLower(strings.TrimSpace(file.Mode))
	switch mode {
	case "", "normal":
		policy.Mode = llmretry.ModeNormal
	case "always":
		policy.Mode = llmretry.ModeAlways
	default:
		return fmt.Errorf("llm.retry.mode: unknown %q (want normal or always)", file.Mode)
	}
	if file.MaxRetries > 0 {
		policy.MaxRetries = file.MaxRetries
	}
	if file.InitialDelay.Duration() > 0 {
		policy.Backoff.InitialDelay = file.InitialDelay.Duration()
	}
	if file.MaxDelay.Duration() > 0 {
		policy.Backoff.MaxDelay = file.MaxDelay.Duration()
	}
	if file.JitterRatio > 0 {
		policy.Backoff.JitterRatio = file.JitterRatio
	}
	if len(file.RetryableCodes) > 0 {
		policy.RetryableCodes = append([]string(nil), file.RetryableCodes...)
	}
	cfg.Retry = policy
	return nil
}

func parseSandboxMode(raw string, fallback sandbox.Mode) (sandbox.Mode, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	m := sandbox.Mode(raw)
	switch m {
	case sandbox.ReadOnly, sandbox.WorkspaceWrite, sandbox.DangerFullAccess:
		return m, nil
	default:
		return "", fmt.Errorf("unknown %q (want read-only, workspace-write, or danger-full-access)", raw)
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("DEEPSEEK_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("DEEPSEEK_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("DEEPSEEK_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil && port > 0 {
			cfg.Port = port
		}
	}
	if v := os.Getenv("WEB_DEMO_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("WEB_DEMO_WORKSPACE_DIR"); v != "" {
		cfg.WorkspaceDir = v
	}
	if v := os.Getenv("WEB_DEMO_PYTHON_CACHE_DIR"); v != "" {
		cfg.PythonCacheDir = v
	}
	if v := os.Getenv("HARNESS_PYTHON_BIN"); v != "" {
		cfg.PythonBin = v
	}
}

func Finalize(cfg *Config) {
	if cfg.Port <= 0 {
		cfg.Port = 8081
	}
	cfg.Addr = fmt.Sprintf(":%d", cfg.Port)
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(os.TempDir(), "go-harness-web-demo")
	}
	if cfg.PythonCacheDir == "" {
		if cacheDir, err := os.UserCacheDir(); err == nil {
			cfg.PythonCacheDir = filepath.Join(cacheDir, "go-harness", "python-venv")
		} else {
			cfg.PythonCacheDir = filepath.Join(cfg.DataDir, "python-venv")
		}
	}
	if len(cfg.PythonPackages) == 0 {
		cfg.PythonPackages = append([]string(nil), python.DefaultDocumentPackages...)
	}
	if cfg.MaxUploadBytes <= 0 {
		cfg.MaxUploadBytes = DefaultUploadBytes
	}
}

func (c Config) PythonEnvOptions() python.EnvOptions {
	return python.EnvOptions{
		CacheDir:       c.PythonCacheDir,
		Packages:       c.PythonPackages,
		PythonBin:      c.PythonBin,
		InstallTimeout: c.PythonInstallTimeout,
	}
}

// Demo: an HTTP agent server stringing together every package built in this
// module — modeled on deepseek-harness's examples/web-cordis, minus the
// Cordis-specific dynamic-plugin-mounting feature that example centers on
// (no Go equivalent to mounting/unmounting live plugins in a running
// process — see README.md). What's left is the more broadly useful part:
// one process, multiple concurrent agent sessions, a real model, real
// tools, a real sandbox, a real skill catalog.
//
//	export DEEPSEEK_API_KEY=sk-...
//	go run .
//
// Then, from another terminal:
//
//	curl -s -X POST localhost:8080/sessions | tee /tmp/session.json
//	id=$(jq -r .id /tmp/session.json)
//	curl -s -X POST localhost:8080/sessions/$id/messages \
//	  -d '{"text":"What packages does run_python have installed?"}'
//	curl -s localhost:8080/sessions/$id/messages
//	curl -s localhost:8080/skills
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

type config struct {
	apiKey  string
	baseURL string
	model   string
	addr    string
	dataDir string
}

func loadConfig() (config, error) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return config{}, fmt.Errorf("set DEEPSEEK_API_KEY to run this demo")
	}
	model := os.Getenv("DEEPSEEK_MODEL")
	if model == "" {
		model = "deepseek-chat"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dataDir := os.Getenv("WEB_DEMO_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(os.TempDir(), "go-harness-web-demo")
	}
	return config{
		apiKey:  apiKey,
		baseURL: os.Getenv("DEEPSEEK_BASE_URL"),
		model:   model,
		addr:    ":" + port,
		dataDir: dataDir,
	}, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server, err := newServer(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "startup:", err)
		os.Exit(1)
	}

	httpServer := &http.Server{Addr: cfg.addr, Handler: server.routes()}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	fmt.Println("listening on", cfg.addr, "model:", cfg.model, "data:", cfg.dataDir)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

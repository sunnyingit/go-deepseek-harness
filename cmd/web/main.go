// Demo: an HTTP agent server stringing together every package built in this
// module — modeled on deepseek-harness's examples/web-cordis, minus the
// Cordis-specific dynamic-plugin-mounting feature that example centers on
// (no Go equivalent to mounting/unmounting live plugins in a running
// process). What's left is the more broadly useful part: one process,
// multiple concurrent agent sessions, a real model, real tools, a real
// sandbox, a real skill catalog.
//
//	export DEEPSEEK_API_KEY=sk-...
//	go run ./cmd/web
//
// Config lives in config.yaml (override with -config or WEB_DEMO_CONFIG).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-harness/internal/appconfig"
	"go-harness/internal/server"
)

func main() {
	configPath := flag.String("config", "", "path to config.yaml (default: ./config.yaml, ./web/config.yaml or $WEB_DEMO_CONFIG)")
	flag.Parse()

	cfg, path, err := appconfig.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := server.New(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "startup:", err)
		os.Exit(1)
	}
	srv.SetConfigPath(path)

	httpServer := &http.Server{Addr: cfg.Addr, Handler: srv.Routes()}

	go func() {
		<-ctx.Done()
		shutdown := cfg.ShutdownTimeout
		if shutdown <= 0 {
			shutdown = 5 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdown)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	fmt.Println("listening on", cfg.Addr, "model:", cfg.Model, "data:", cfg.DataDir, "python cache:", cfg.PythonCacheDir)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

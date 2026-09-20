// Command vidbuild runs the vid-build 0.01 local-first video studio.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vidbuild/internal/config"
	"vidbuild/internal/db"
	"vidbuild/internal/store"
	"vidbuild/internal/web"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config.json")
	flag.Parse()

	cfg, err := config.LoadOrCreate(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	basePath, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cwd error:", err)
		os.Exit(1)
	}
	if err := cfg.EnsureDirs(basePath); err != nil {
		fmt.Fprintln(os.Stderr, "dir error:", err)
		os.Exit(1)
	}

	logger := newLogger(cfg)

	database, err := db.Open(cfg.AbsPath(basePath, cfg.Database.Path), cfg.Database.BusyTimeoutMs, cfg.Database.MaxPoolConns)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	st := store.New(database)
	srv := web.New(cfg, st, logger)
	if err := srv.Bootstrap(context.Background()); err != nil {
		logger.Error("bootstrap", "err", err)
		os.Exit(1)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("vid-build listening", "addr", "http://"+addr, "mode", cfg.Server.Mode)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	logger.Info("vid-build stopped")
}

func newLogger(cfg *config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.Logging.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	var w *os.File = os.Stderr
	if cfg.Logging.Console {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	}
	if cfg.Logging.File != "" {
		if f, err := os.OpenFile(cfg.Logging.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			w = f
		}
	}
	_ = w
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

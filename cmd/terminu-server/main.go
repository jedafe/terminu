// Ref: .teos/.ai/architecture.yaml — cmd/terminu-server entry point.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jedafe/terminu/internal/adapter"
	"github.com/jedafe/terminu/internal/auth"
	"github.com/jedafe/terminu/internal/broker"
	"github.com/jedafe/terminu/internal/config"
	"github.com/jedafe/terminu/internal/db"
	"github.com/jedafe/terminu/internal/db/gen"
	"github.com/jedafe/terminu/internal/registry"
	"github.com/jedafe/terminu/internal/server"
	"github.com/jedafe/terminu/internal/session"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// TU_LOG_LEVEL selects slog's floor — it was loaded but never
	// applied; default stays info.
	if lvl, ok := map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo,
		"warn": slog.LevelWarn, "error": slog.LevelError,
	}[strings.ToLower(cfg.LogLevel)]; ok {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
	}
	ctx := context.Background()

	d, err := db.Open(ctx, cfg.SQLitePath)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer func(d *sql.DB) { _ = d.Close() }(d)

	q := gen.New(d)
	bus := session.NewEventBus()
	mgr := session.NewManager(cfg, q, bus)
	if err := mgr.Recover(ctx); err != nil {
		return fmt.Errorf("recover: %w", err)
	}
	authSvc := auth.New(q)
	if err := authSvc.Seed(ctx, cfg); err != nil {
		return fmt.Errorf("auth seed: %w", err)
	}
	br := broker.New(q)

	// §04: command registry — builtin + custom sources sync to the
	// table, shadow resolution rebuilds the search index.
	reg := registry.New(q, cfg.DataDir)
	if err := reg.Sync(ctx); err != nil {
		return fmt.Errorf("registry sync: %w", err)
	}
	// §06: declared adapter pins persist; the fixture suite replays at
	// boot so /api/adapters/{tool} reads a current pass/fail record.
	if err := mgr.SyncPins(ctx); err != nil {
		return fmt.Errorf("pin sync: %w", err)
	}
	replayFixtures(ctx, q)

	srv := server.New(cfg, q, authSvc, br, mgr, bus, reg)

	addr := cfg.Bind + ":" + strconv.Itoa(cfg.Port)
	hs := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("terminu listening", "addr", addr,
			"mode", cfg.LocalTargetMode, "data", cfg.DataDir)
		errCh <- hs.ListenAndServe()
	}()

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
		slog.Info("shutting down")
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = hs.Shutdown(ctx)
	mgr.CloseAll()
	return nil
}

// replayFixtures registers every embedded .cast in the fixtures table
// and records the boot smoke result — a failing fixture warns loudly in
// logs and shows as last_result=fail on the adapter route.
func replayFixtures(ctx context.Context, q *gen.Queries) {
	results := adapter.SmokeAll()
	failed := 0
	for _, res := range results {
		status := "pass"
		if !res.Pass {
			status = "fail"
			failed++
		}
		if err := q.UpsertFixture(ctx, gen.UpsertFixtureParams{
			Tool: res.Tool, Version: res.Version,
			Scenario: res.Scenario, Path: res.Path,
		}); err != nil {
			slog.Warn("fixture upsert", "err", err)
			continue
		}
		if err := q.UpdateFixtureResult(ctx, gen.UpdateFixtureResultParams{
			LastResult: status, Tool: res.Tool,
			Version: res.Version, Scenario: res.Scenario,
		}); err != nil {
			slog.Warn("fixture result", "err", err)
		}
	}
	if failed > 0 {
		slog.Warn("fixture replay failures", "failed", failed, "total", len(results))
	} else {
		slog.Info("fixture replay green", "total", len(results))
	}
}

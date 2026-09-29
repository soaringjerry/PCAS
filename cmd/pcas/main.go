package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/soaringjerry/PCAS/internal/config"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], logger); err != nil {
		logger.Error("pcas stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, logger *slog.Logger) error {
	if len(args) != 1 || (args[0] != "serve" && args[0] != "worker" && args[0] != "migrate") {
		return fmt.Errorf("usage: pcas {serve|worker|migrate}")
	}
	command := args[0]
	cfg, err := config.Load(command)
	if err != nil {
		return err
	}
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	db, err := postgres.Open(startup, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()
	if command == "migrate" {
		migrationCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := db.Migrate(migrationCtx); err != nil {
			return err
		}
		logger.Info("memory schema ready")
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = db.CheckSchema(checkCtx)
	cancel()
	if err != nil {
		return err
	}
	if command == "worker" {
		logger.Info("memory worker started", "handlers", []string{"source.chunk"})
		return worker.New(db, map[string]worker.Handler{"source.chunk": db.ProcessChunks}, logger).Run(ctx)
	}
	api := httpapi.New(memory.NewService(db), nil, httpapi.NewOwnerToken(cfg.APIToken, cfg.OwnerID), db.Ping, logger)
	server := &http.Server{Addr: cfg.HTTPAddress, Handler: api, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	stopped := make(chan error, 1)
	go func() { stopped <- server.ListenAndServe() }()
	logger.Info("memory API started", "address", cfg.HTTPAddress)
	select {
	case err := <-stopped:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}

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

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/blob"
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
	codex, err := ai.NewCodex(os.Getenv("PCAS_CODEX_BINARY"), os.Getenv("PCAS_CODEX_HOME"))
	if err != nil {
		return err
	}
	if codex != nil {
		defer codex.Close()
	}
	models, err := ai.Load(os.Getenv("PCAS_MODELS_FILE"), codex)
	if err != nil {
		return err
	}
	models.ReloadSubscription = command == "worker"
	db.SetModels(models)
	blobDir := os.Getenv("PCAS_BLOB_DIR")
	if blobDir == "" {
		blobDir = "data/blobs"
	}
	files, err := blob.NewFiles(blobDir)
	if err != nil {
		return err
	}
	db.SetBlobs(files)
	if dir := os.Getenv("PCAS_INBOX_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	if command == "worker" {
		logger.Info("memory worker started")
		return worker.New(db, map[string]worker.Handler{"memory.summary": db.ProcessSummary, "source.parse": db.ProcessAttachment, "source.chunk": db.ProcessChunks, "source.tokenize": db.ProcessIndex, "memory.index": db.ProcessIndex, "source.extract": db.ProcessExtraction, "source.embed": db.ProcessEmbedding, "memory.embed": db.ProcessEmbedding}, logger).Run(ctx)
	}
	webDir := os.Getenv("PCAS_WEB_DIR")
	if webDir == "" {
		webDir = "web/dist"
	}
	credentials, err := httpapi.LoadCredentials(cfg.APIToken, cfg.OwnerID, os.Getenv("PCAS_AGENT_TOKENS_FILE"))
	if err != nil {
		return err
	}
	api := httpapi.New(memory.NewService(db), db, httpapi.NewSessions(credentials, cfg.PublicURL), db.Ping, logger, httpapi.Options{Continuity: db, Connectors: db, Attachments: db, Writer: db, Workspace: db, Editor: db, Activity: db, Models: models, WebDir: webDir})
	workCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	go func() { _ = db.RunAgents(workCtx, logger) }()
	go func() { _ = db.RunReminders(workCtx, logger) }()
	go func() { _ = db.RunConnectors(workCtx, logger) }()
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

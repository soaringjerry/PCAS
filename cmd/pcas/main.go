package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/ai/siwc"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/config"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/telegram"
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
	if len(args) > 0 && (args[0] == "chatgpt-login" || args[0] == "chatgpt-import" || args[0] == "chatgpt-verify") {
		return chatgptCommand(ctx, args)
	}
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
	directEnabled := false
	if value := os.Getenv("PCAS_CHATGPT_DIRECT_ENABLED"); value != "" {
		directEnabled, err = strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid PCAS_CHATGPT_DIRECT_ENABLED")
		}
	}
	var direct *siwc.Manager
	if directEnabled {
		directDir := os.Getenv("PCAS_CHATGPT_DIR")
		if directDir == "" {
			directDir = "data/chatgpt"
		}
		callbackPort := 1455
		if value := os.Getenv("PCAS_CHATGPT_CALLBACK_PORT"); value != "" {
			callbackPort, err = strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid PCAS_CHATGPT_CALLBACK_PORT")
			}
		}
		direct, err = siwc.New(directDir, os.Getenv("PCAS_CHATGPT_CALLBACK_BIND"), callbackPort)
		if err != nil {
			return err
		}
		defer direct.Close()
	}
	models, err := ai.Load(os.Getenv("PCAS_MODELS_FILE"), codex, direct)
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
		backfillCtx, stopBackfill := context.WithCancel(ctx)
		defer stopBackfill()
		organizeDone := make(chan struct{})
		defer func() { stopBackfill(); <-organizeDone }()
		go func() {
			defer close(organizeDone)
			db.RunOrganize(backfillCtx, logger)
		}()
		backfillDone := make(chan struct{})
		defer func() { stopBackfill(); <-backfillDone }()
		go func() {
			defer close(backfillDone)
			timer := time.NewTicker(postgres.ExtractionBackfillInterval)
			defer timer.Stop()
			for {
				if backfillCtx.Err() != nil {
					return
				}
				if _, err := db.BackfillExtractions(backfillCtx, time.Now()); err != nil && backfillCtx.Err() == nil {
					logger.Warn("extraction backfill check failed", "error_type", "backfill_failed")
				}
				select {
				case <-backfillCtx.Done():
					return
				case <-timer.C:
				}
			}
		}()
		return worker.New(db, map[string]worker.Handler{"memory.organize": db.ProcessOrganize, "memory.summary": db.ProcessSummary, "source.parse": db.ProcessAttachment, "source.chunk": db.ProcessChunks, "source.tokenize": db.ProcessIndex, "memory.index": db.ProcessIndex, "source.extract": db.ProcessExtraction, "source.embed": db.ProcessEmbedding, "memory.embed": db.ProcessEmbedding}, logger).Run(ctx)
	}
	webDir := os.Getenv("PCAS_WEB_DIR")
	if webDir == "" {
		webDir = "web/dist"
	}
	credentials, err := httpapi.LoadCredentials(cfg.APIToken, cfg.OwnerID, os.Getenv("PCAS_AGENT_TOKENS_FILE"))
	if err != nil {
		return err
	}
	// A key saved in Settings wins; the environment key is the fallback.
	router := ai.NewRouter(func() string {
		if key := models.DecisionKey(); key != "" {
			return key
		}
		return os.Getenv("TYPESAFE_API_KEY")
	})
	notifier := postgres.NewNotifier(db, notify.Settings{Path: notify.SettingsPath()}, cfg.PublicURL)
	api := httpapi.New(memory.NewService(db), db, httpapi.NewSessions(credentials, cfg.PublicURL), db.Ping, logger, httpapi.Options{Continuity: db, Connectors: db, Attachments: db, Writer: db, Workspace: notifier, Editor: db, Activity: db, Models: models, Router: router, WebDir: webDir})
	workCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	go func() { _ = db.RunAgents(workCtx, logger) }()
	go func() { _ = db.RunReminders(workCtx, logger) }()
	go func() { _ = db.RunNotify(workCtx, logger, notifier.Channels) }()
	go telegram.Run(workCtx, db, models, notifier.Settings, cfg.OwnerID, logger)
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

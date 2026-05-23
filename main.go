package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"code-review-bot/internal/config"
	"code-review-bot/internal/db"
	"code-review-bot/internal/jobs"
	"code-review-bot/internal/server"
	"code-review-bot/internal/settings"
	"code-review-bot/internal/worker"
)

func openDatabaseWithRetry(ctx context.Context, databaseURL string, logger *slog.Logger) (*sql.DB, error) {
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for {
		database, err := db.Open(ctx, databaseURL)
		if err == nil {
			return database, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		logger.Warn("database is not ready; retrying", "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()
	ctx := context.Background()

	var store jobs.Store = jobs.NewMemoryStore()
	var settingsStore *settings.Store
	var databaseCloser func() error
	if cfg.DatabaseURL != "" {
		database, err := openDatabaseWithRetry(ctx, cfg.DatabaseURL, logger)
		if err != nil {
			logger.Error("database connection failed", "error", err)
			os.Exit(1)
		}
		if err := db.Migrate(ctx, database); err != nil {
			logger.Error("database migration failed", "error", err)
			database.Close()
			os.Exit(1)
		}
		databaseCloser = database.Close
		store = jobs.NewPostgresStore(database)
		settingsStore = settings.NewStore(database, settings.FromConfig(cfg))
		logger.Info("using postgresql job store")
	} else {
		logger.Warn("DATABASE_URL is not set; using in-memory job store and env configuration")
	}

	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	if settingsStore != nil {
		go worker.New(store, settingsStore, worker.Options{PollInterval: cfg.WorkerPollInterval}, logger).Run(workerCtx)
		logger.Info("review worker started")
	} else {
		logger.Warn("review worker is disabled without DATABASE_URL")
	}

	handler := server.New(cfg, store, settingsStore, logger).Handler()

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("starting code review bot", "port", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	stopWorker()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}
	if databaseCloser != nil {
		if err := databaseCloser(); err != nil {
			logger.Error("database close failed", "error", err)
		}
	}
	logger.Info("server stopped")
}

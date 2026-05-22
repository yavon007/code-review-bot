package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"code-review-bot/internal/config"
	"code-review-bot/internal/db"
	"code-review-bot/internal/gitea"
	"code-review-bot/internal/jobs"
	"code-review-bot/internal/review"
	"code-review-bot/internal/server"
	"code-review-bot/internal/worker"
)

func buildReviewer(cfg config.Config, logger *slog.Logger) review.Reviewer {
	if cfg.OpenAIAPIKey == "" {
		logger.Warn("OPENAI_API_KEY is not set; using mock reviewer")
		return review.MockReviewer{}
	}
	reviewer, err := review.NewOpenAIReviewer(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.ReviewModel)
	if err != nil {
		logger.Error("openai reviewer initialization failed; using mock reviewer", "error", err)
		return review.MockReviewer{}
	}
	logger.Info("using openai-compatible reviewer", "model", cfg.ReviewModel)
	return reviewer
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()
	ctx := context.Background()

	var store jobs.Store = jobs.NewMemoryStore()
	var databaseCloser func() error
	if cfg.DatabaseURL != "" {
		database, err := db.Open(ctx, cfg.DatabaseURL)
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
		logger.Info("using postgresql job store")
	} else {
		logger.Warn("DATABASE_URL is not set; using in-memory job store")
	}

	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	if cfg.GiteaBaseURL != "" && cfg.GiteaToken != "" {
		giteaClient, err := gitea.NewClient(cfg.GiteaBaseURL, cfg.GiteaToken)
		if err != nil {
			logger.Error("gitea client initialization failed", "error", err)
			os.Exit(1)
		}
		reviewer := buildReviewer(cfg, logger)
		go worker.New(store, giteaClient, reviewer, cfg.WorkerPollInterval, cfg.ReviewMaxDiffBytes, logger).Run(workerCtx)
		logger.Info("review worker started")
	} else {
		logger.Warn("GITEA_BASE_URL or GITEA_TOKEN is not set; review worker is disabled")
	}

	handler := server.New(cfg, store, logger).Handler()

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

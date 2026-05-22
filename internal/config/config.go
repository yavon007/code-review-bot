package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port               string
	DatabaseURL        string
	GiteaBaseURL       string
	GiteaToken         string
	GiteaWebhookSecret string
	BotName            string
	ReviewModel        string
	ReviewMaxDiffBytes int64
	OpenAIAPIKey       string
	OpenAIBaseURL      string
	WorkerPollInterval time.Duration
	WorkerConcurrency  int
}

func Load() Config {
	return Config{
		Port:               getEnv("PORT", "8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		GiteaBaseURL:       os.Getenv("GITEA_BASE_URL"),
		GiteaToken:         os.Getenv("GITEA_TOKEN"),
		GiteaWebhookSecret: os.Getenv("GITEA_WEBHOOK_SECRET"),
		BotName:            getEnv("BOT_NAME", "gpt-review-bot"),
		ReviewModel:        getEnv("REVIEW_MODEL", "gpt-4.1"),
		ReviewMaxDiffBytes: getEnvInt64("REVIEW_MAX_DIFF_BYTES", 120000),
		OpenAIAPIKey:       os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL:      getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		WorkerPollInterval: getEnvDuration("WORKER_POLL_INTERVAL", 5*time.Second),
		WorkerConcurrency:  getEnvInt("WORKER_CONCURRENCY", 1),
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

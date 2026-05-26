package settings

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"code-review-bot/internal/config"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrAlreadyInitialized = errors.New("system is already initialized")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type AppSettings struct {
	GiteaBaseURL                     string        `json:"gitea_base_url"`
	GiteaToken                       string        `json:"gitea_token,omitempty"`
	GiteaWebhookSecret               string        `json:"gitea_webhook_secret,omitempty"`
	BotName                          string        `json:"bot_name"`
	OpenAIAPIKey                     string        `json:"openai_api_key,omitempty"`
	OpenAIBaseURL                    string        `json:"openai_base_url"`
	ReviewModel                      string        `json:"review_model"`
	ReviewLanguage                   string        `json:"review_language"`
	ReviewProfile                    string        `json:"review_profile"`
	ReviewFocusAreas                 []string      `json:"review_focus_areas"`
	ReviewOutputStyle                string        `json:"review_output_style"`
	ReviewExtraInstructions          string        `json:"review_extra_instructions"`
	ReviewInputTokenPricePerMillion  float64       `json:"review_input_token_price_per_million"`
	ReviewOutputTokenPricePerMillion float64       `json:"review_output_token_price_per_million"`
	ReviewMaxDiffBytes               int64         `json:"review_max_diff_bytes"`
	ReviewExcludePaths               []string      `json:"review_exclude_paths"`
	ReviewFailOnHigh                 bool          `json:"review_fail_on_high"`
	ReviewPostInlineComments         bool          `json:"review_post_inline_comments"`
	ReviewMaxFindings                int           `json:"review_max_findings"`
	ReviewMaxAttempts                int           `json:"review_max_attempts"`
	ReviewStaleTimeout               time.Duration `json:"-"`
	ReviewStaleTimeoutText           string        `json:"review_stale_timeout"`
	WorkerPollInterval               time.Duration `json:"-"`
	WorkerPollIntervalText           string        `json:"worker_poll_interval"`
}

type PublicSettings struct {
	GiteaBaseURL                     string   `json:"gitea_base_url"`
	HasGiteaToken                    bool     `json:"has_gitea_token"`
	HasGiteaWebhookSecret            bool     `json:"has_gitea_webhook_secret"`
	BotName                          string   `json:"bot_name"`
	HasOpenAIAPIKey                  bool     `json:"has_openai_api_key"`
	OpenAIBaseURL                    string   `json:"openai_base_url"`
	ReviewModel                      string   `json:"review_model"`
	ReviewLanguage                   string   `json:"review_language"`
	ReviewProfile                    string   `json:"review_profile"`
	ReviewFocusAreas                 []string `json:"review_focus_areas"`
	ReviewOutputStyle                string   `json:"review_output_style"`
	ReviewExtraInstructions          string   `json:"review_extra_instructions"`
	ReviewInputTokenPricePerMillion  float64  `json:"review_input_token_price_per_million"`
	ReviewOutputTokenPricePerMillion float64  `json:"review_output_token_price_per_million"`
	ReviewMaxDiffBytes               int64    `json:"review_max_diff_bytes"`
	ReviewExcludePaths               []string `json:"review_exclude_paths"`
	ReviewFailOnHigh                 bool     `json:"review_fail_on_high"`
	ReviewPostInlineComments         bool     `json:"review_post_inline_comments"`
	ReviewMaxFindings                int      `json:"review_max_findings"`
	ReviewMaxAttempts                int      `json:"review_max_attempts"`
	ReviewStaleTimeout               string   `json:"review_stale_timeout"`
	WorkerPollInterval               string   `json:"worker_poll_interval"`
}

type Store struct {
	db       *sql.DB
	fallback AppSettings
}

func NewStore(db *sql.DB, fallback AppSettings) *Store {
	return &Store{db: db, fallback: fallback.Normalize()}
}

func FromConfig(cfg config.Config) AppSettings {
	return AppSettings{
		GiteaBaseURL:                     cfg.GiteaBaseURL,
		GiteaToken:                       cfg.GiteaToken,
		GiteaWebhookSecret:               cfg.GiteaWebhookSecret,
		BotName:                          cfg.BotName,
		OpenAIAPIKey:                     cfg.OpenAIAPIKey,
		OpenAIBaseURL:                    cfg.OpenAIBaseURL,
		ReviewModel:                      cfg.ReviewModel,
		ReviewLanguage:                   "中文",
		ReviewProfile:                    "balanced",
		ReviewFocusAreas:                 []string{"correctness", "security", "data_loss", "concurrency", "test_gap"},
		ReviewOutputStyle:                "detailed",
		ReviewExtraInstructions:          "",
		ReviewInputTokenPricePerMillion:  0,
		ReviewOutputTokenPricePerMillion: 0,
		ReviewMaxDiffBytes:               cfg.ReviewMaxDiffBytes,
		ReviewExcludePaths:               cfg.ReviewExcludePaths,
		ReviewFailOnHigh:                 cfg.ReviewFailOnHigh,
		ReviewPostInlineComments:         cfg.ReviewPostInlineComments,
		ReviewMaxFindings:                cfg.ReviewMaxFindings,
		ReviewMaxAttempts:                3,
		ReviewStaleTimeout:               10 * time.Minute,
		ReviewStaleTimeoutText:           (10 * time.Minute).String(),
		WorkerPollInterval:               cfg.WorkerPollInterval,
		WorkerPollIntervalText:           cfg.WorkerPollInterval.String(),
	}
}

func (s AppSettings) Normalize() AppSettings {
	if s.BotName == "" {
		s.BotName = "gpt-review-bot"
	}
	if s.OpenAIBaseURL == "" {
		s.OpenAIBaseURL = "https://api.openai.com/v1"
	}
	if s.ReviewModel == "" {
		s.ReviewModel = "gpt-4.1"
	}
	if strings.TrimSpace(s.ReviewLanguage) == "" {
		s.ReviewLanguage = "中文"
	}
	if strings.TrimSpace(s.ReviewProfile) == "" {
		s.ReviewProfile = "balanced"
	}
	if len(s.ReviewFocusAreas) == 0 {
		s.ReviewFocusAreas = []string{"correctness", "security", "data_loss", "concurrency", "test_gap"}
	}
	if strings.TrimSpace(s.ReviewOutputStyle) == "" {
		s.ReviewOutputStyle = "detailed"
	}
	s.ReviewExtraInstructions = strings.TrimSpace(s.ReviewExtraInstructions)
	if s.ReviewInputTokenPricePerMillion < 0 {
		s.ReviewInputTokenPricePerMillion = 0
	}
	if s.ReviewOutputTokenPricePerMillion < 0 {
		s.ReviewOutputTokenPricePerMillion = 0
	}
	if s.ReviewMaxDiffBytes <= 0 {
		s.ReviewMaxDiffBytes = 120000
	}
	if len(s.ReviewExcludePaths) == 0 {
		s.ReviewExcludePaths = []string{"vendor/**", "node_modules/**", "dist/**", "build/**", "*.lock", "*.min.js"}
	}
	if s.ReviewMaxFindings <= 0 {
		s.ReviewMaxFindings = 20
	}
	if s.ReviewMaxAttempts <= 0 {
		s.ReviewMaxAttempts = 3
	}
	if s.ReviewStaleTimeoutText != "" {
		if parsed, err := time.ParseDuration(s.ReviewStaleTimeoutText); err == nil {
			s.ReviewStaleTimeout = parsed
		}
	}
	if s.ReviewStaleTimeout <= 0 {
		s.ReviewStaleTimeout = 10 * time.Minute
	}
	if s.ReviewStaleTimeout < 15*time.Second {
		s.ReviewStaleTimeout = 15 * time.Second
	}
	s.ReviewStaleTimeoutText = s.ReviewStaleTimeout.String()
	if s.WorkerPollIntervalText != "" {
		if parsed, err := time.ParseDuration(s.WorkerPollIntervalText); err == nil {
			s.WorkerPollInterval = parsed
		}
	}
	if s.WorkerPollInterval <= 0 {
		s.WorkerPollInterval = 5 * time.Second
	}
	s.WorkerPollIntervalText = s.WorkerPollInterval.String()
	return s
}

func (s AppSettings) Public() PublicSettings {
	s = s.Normalize()
	return PublicSettings{
		GiteaBaseURL:                     s.GiteaBaseURL,
		HasGiteaToken:                    s.GiteaToken != "",
		HasGiteaWebhookSecret:            s.GiteaWebhookSecret != "",
		BotName:                          s.BotName,
		HasOpenAIAPIKey:                  s.OpenAIAPIKey != "",
		OpenAIBaseURL:                    s.OpenAIBaseURL,
		ReviewModel:                      s.ReviewModel,
		ReviewLanguage:                   s.ReviewLanguage,
		ReviewProfile:                    s.ReviewProfile,
		ReviewFocusAreas:                 s.ReviewFocusAreas,
		ReviewOutputStyle:                s.ReviewOutputStyle,
		ReviewExtraInstructions:          s.ReviewExtraInstructions,
		ReviewInputTokenPricePerMillion:  s.ReviewInputTokenPricePerMillion,
		ReviewOutputTokenPricePerMillion: s.ReviewOutputTokenPricePerMillion,
		ReviewMaxDiffBytes:               s.ReviewMaxDiffBytes,
		ReviewExcludePaths:               s.ReviewExcludePaths,
		ReviewFailOnHigh:                 s.ReviewFailOnHigh,
		ReviewPostInlineComments:         s.ReviewPostInlineComments,
		ReviewMaxFindings:                s.ReviewMaxFindings,
		ReviewMaxAttempts:                s.ReviewMaxAttempts,
		ReviewStaleTimeout:               s.ReviewStaleTimeout.String(),
		WorkerPollInterval:               s.WorkerPollInterval.String(),
	}
}

func (s *Store) IsInitialized(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `select count(*) from admin_users`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) CreateInitialAdmin(ctx context.Context, username string, password string, appSettings AppSettings) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, `select count(*) from admin_users`).Scan(&count); err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, ErrAlreadyInitialized
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}

	var id int64
	if err := tx.QueryRowContext(ctx, `
		insert into admin_users (username, password_hash)
		values ($1, $2)
		returning id
	`, username, string(hash)).Scan(&id); err != nil {
		return 0, err
	}

	if err := upsertSettings(ctx, tx, appSettings.Normalize(), false); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) Authenticate(ctx context.Context, username string, password string) (int64, int, error) {
	var id int64
	var sessionVersion int
	var hash string
	err := s.db.QueryRowContext(ctx, `
		select id, coalesce(session_version, 0), password_hash from admin_users where username = $1
	`, username).Scan(&id, &sessionVersion, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, ErrInvalidCredentials
	}
	if err != nil {
		return 0, 0, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return 0, 0, ErrInvalidCredentials
	}
	return id, sessionVersion, nil
}

func (s *Store) SessionVersion(ctx context.Context, userID int64) (int, error) {
	var sessionVersion int
	err := s.db.QueryRowContext(ctx, `
		select coalesce(session_version, 0) from admin_users where id = $1
	`, userID).Scan(&sessionVersion)
	if err != nil {
		return 0, err
	}
	return sessionVersion, nil
}

func (s *Store) RevokeUserSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `
		update admin_users set session_version = session_version + 1 where id = $1
	`, userID)
	return err
}

func (s *Store) Load(ctx context.Context) (AppSettings, error) {
	settings := s.fallback.Normalize()
	rows, err := s.db.QueryContext(ctx, `select key, value from app_settings`)
	if err != nil {
		return AppSettings{}, err
	}
	defer rows.Close()

	values := make(map[string]string)
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return AppSettings{}, err
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return AppSettings{}, err
	}
	applyValues(&settings, values)
	return settings.Normalize(), nil
}

func (s *Store) Save(ctx context.Context, appSettings AppSettings, preserveSensitive bool) error {
	if preserveSensitive {
		current, err := s.Load(ctx)
		if err != nil {
			return err
		}
		if appSettings.GiteaToken == "" {
			appSettings.GiteaToken = current.GiteaToken
		}
		if appSettings.GiteaWebhookSecret == "" {
			appSettings.GiteaWebhookSecret = current.GiteaWebhookSecret
		}
		if appSettings.OpenAIAPIKey == "" {
			appSettings.OpenAIAPIKey = current.OpenAIAPIKey
		}
	}
	return upsertSettings(ctx, s.db, appSettings.Normalize(), false)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func upsertSettings(ctx context.Context, db execer, settings AppSettings, preserveSensitive bool) error {
	_ = preserveSensitive
	for key, value := range settingValues(settings.Normalize()) {
		if _, err := db.ExecContext(ctx, `
			insert into app_settings (key, value)
			values ($1, $2)
			on conflict (key) do update set value = excluded.value, updated_at = now()
		`, key, value); err != nil {
			return err
		}
	}
	return nil
}

func settingValues(settings AppSettings) map[string]string {
	return map[string]string{
		"gitea_base_url":                        settings.GiteaBaseURL,
		"gitea_token":                           settings.GiteaToken,
		"gitea_webhook_secret":                  settings.GiteaWebhookSecret,
		"bot_name":                              settings.BotName,
		"openai_api_key":                        settings.OpenAIAPIKey,
		"openai_base_url":                       settings.OpenAIBaseURL,
		"review_model":                          settings.ReviewModel,
		"review_language":                       settings.ReviewLanguage,
		"review_profile":                        settings.ReviewProfile,
		"review_focus_areas":                    strings.Join(settings.ReviewFocusAreas, ","),
		"review_output_style":                   settings.ReviewOutputStyle,
		"review_extra_instructions":             settings.ReviewExtraInstructions,
		"review_input_token_price_per_million":  strconv.FormatFloat(settings.ReviewInputTokenPricePerMillion, 'f', -1, 64),
		"review_output_token_price_per_million": strconv.FormatFloat(settings.ReviewOutputTokenPricePerMillion, 'f', -1, 64),
		"review_max_diff_bytes":                 strconv.FormatInt(settings.ReviewMaxDiffBytes, 10),
		"review_exclude_paths":                  strings.Join(settings.ReviewExcludePaths, ","),
		"review_fail_on_high":                   strconv.FormatBool(settings.ReviewFailOnHigh),
		"review_post_inline_comments":           strconv.FormatBool(settings.ReviewPostInlineComments),
		"review_max_findings":                   strconv.Itoa(settings.ReviewMaxFindings),
		"review_max_attempts":                   strconv.Itoa(settings.ReviewMaxAttempts),
		"review_stale_timeout":                  settings.ReviewStaleTimeout.String(),
		"worker_poll_interval":                  settings.WorkerPollInterval.String(),
	}
}

func applyValues(settings *AppSettings, values map[string]string) {
	settings.GiteaBaseURL = valueOrDefault(values, "gitea_base_url", settings.GiteaBaseURL)
	settings.GiteaToken = valueOrDefault(values, "gitea_token", settings.GiteaToken)
	settings.GiteaWebhookSecret = valueOrDefault(values, "gitea_webhook_secret", settings.GiteaWebhookSecret)
	settings.BotName = valueOrDefault(values, "bot_name", settings.BotName)
	settings.OpenAIAPIKey = valueOrDefault(values, "openai_api_key", settings.OpenAIAPIKey)
	settings.OpenAIBaseURL = valueOrDefault(values, "openai_base_url", settings.OpenAIBaseURL)
	settings.ReviewModel = valueOrDefault(values, "review_model", settings.ReviewModel)
	settings.ReviewLanguage = valueOrDefault(values, "review_language", settings.ReviewLanguage)
	settings.ReviewProfile = valueOrDefault(values, "review_profile", settings.ReviewProfile)
	settings.ReviewFocusAreas = splitList(valueOrDefault(values, "review_focus_areas", strings.Join(settings.ReviewFocusAreas, ",")))
	settings.ReviewOutputStyle = valueOrDefault(values, "review_output_style", settings.ReviewOutputStyle)
	settings.ReviewExtraInstructions = valueOrDefault(values, "review_extra_instructions", settings.ReviewExtraInstructions)
	if value, ok := values["review_input_token_price_per_million"]; ok {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			settings.ReviewInputTokenPricePerMillion = parsed
		}
	}
	if value, ok := values["review_output_token_price_per_million"]; ok {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			settings.ReviewOutputTokenPricePerMillion = parsed
		}
	}
	settings.ReviewExcludePaths = splitList(valueOrDefault(values, "review_exclude_paths", strings.Join(settings.ReviewExcludePaths, ",")))
	settings.ReviewStaleTimeoutText = valueOrDefault(values, "review_stale_timeout", settings.ReviewStaleTimeout.String())
	settings.WorkerPollIntervalText = valueOrDefault(values, "worker_poll_interval", settings.WorkerPollInterval.String())
	if value, ok := values["review_max_diff_bytes"]; ok {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			settings.ReviewMaxDiffBytes = parsed
		}
	}
	if value, ok := values["review_fail_on_high"]; ok {
		if parsed, err := strconv.ParseBool(value); err == nil {
			settings.ReviewFailOnHigh = parsed
		}
	}
	if value, ok := values["review_post_inline_comments"]; ok {
		if parsed, err := strconv.ParseBool(value); err == nil {
			settings.ReviewPostInlineComments = parsed
		}
	}
	if value, ok := values["review_max_findings"]; ok {
		if parsed, err := strconv.Atoi(value); err == nil {
			settings.ReviewMaxFindings = parsed
		}
	}
	if value, ok := values["review_max_attempts"]; ok {
		if parsed, err := strconv.Atoi(value); err == nil {
			settings.ReviewMaxAttempts = parsed
		}
	}
}

func valueOrDefault(values map[string]string, key string, fallback string) string {
	if value, ok := values[key]; ok {
		return value
	}
	return fallback
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"code-review-bot/internal/config"
	"code-review-bot/internal/jobs"
	"code-review-bot/internal/settings"
	"code-review-bot/internal/webhook"
)

type Server struct {
	cfg           config.Config
	jobs          jobs.Store
	settings      *settings.Store
	sessionSecret string
	mux           *http.ServeMux
	log           *slog.Logger
}

func New(cfg config.Config, store jobs.Store, settingsStore *settings.Store, logger *slog.Logger) *Server {
	secret := cfg.SessionSecret
	if secret == "" {
		secret = sessionSecretFallback(cfg.Port)
	}
	s := &Server{
		cfg:           cfg,
		jobs:          store,
		settings:      settingsStore,
		sessionSecret: secret,
		mux:           http.NewServeMux(),
		log:           logger,
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("POST /webhooks/gitea", s.handleGiteaWebhook)
	s.mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/me", s.handleMe)
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("POST /api/settings", s.handleSaveSettings)
	s.mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	s.mux.HandleFunc("GET /api/jobs/{id}/findings", s.handleListFindings)
	s.mux.HandleFunc("POST /api/jobs/{id}/retry", s.handleRetryJob)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.isInitialized(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read setup status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"initialized": initialized})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeError(w, http.StatusConflict, "database is required for setup")
		return
	}

	var request struct {
		Username string               `json:"username"`
		Password string               `json:"password"`
		Settings settings.AppSettings `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid setup payload")
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if request.Username == "" || len(request.Password) < 8 {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	userID, err := s.settings.CreateInitialAdmin(r.Context(), request.Username, request.Password, request.Settings)
	if err != nil {
		if errors.Is(err, settings.ErrAlreadyInitialized) {
			writeError(w, http.StatusConflict, "system is already initialized")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to initialize system")
		return
	}
	setSessionCookie(w, s.sessionSecret, userID)
	writeJSON(w, http.StatusCreated, map[string]any{"initialized": true})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeError(w, http.StatusUnauthorized, "login is disabled without database")
		return
	}

	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid login payload")
		return
	}
	userID, err := s.settings.Authenticate(r.Context(), strings.TrimSpace(request.Username), request.Password)
	if err != nil {
		if errors.Is(err, settings.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to login")
		return
	}
	setSessionCookie(w, s.sessionSecret, userID)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "user_id": userID})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	appSettings, err := s.currentSettings(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": appSettings.Public()})
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if s.settings == nil {
		writeError(w, http.StatusConflict, "database is required for settings")
		return
	}

	var request settings.AppSettings
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid settings payload")
		return
	}
	if err := s.settings.Save(r.Context(), request, true); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	appSettings, err := s.settings.Load(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": appSettings.Public()})
}

func (s *Server) handleGiteaWebhook(w http.ResponseWriter, r *http.Request) {
	appSettings, err := s.currentSettings(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	if appSettings.GiteaWebhookSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "webhook secret is not configured")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	eventName := r.Header.Get("X-Gitea-Event")
	deliveryID := r.Header.Get("X-Gitea-Delivery")
	signature := r.Header.Get("X-Gitea-Signature")
	hubSignature := r.Header.Get("X-Hub-Signature-256")

	if !webhook.VerifyGiteaSignature(appSettings.GiteaWebhookSecret, body, signature, hubSignature) {
		s.log.Warn("rejected gitea webhook with invalid signature", "event", eventName, "delivery_id", deliveryID)
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	input, err := webhook.DecodePayload(eventName, deliveryID, body)
	if err != nil {
		if errors.Is(err, webhook.ErrUnsupportedEvent) {
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
			return
		}
		writeError(w, http.StatusBadRequest, "invalid webhook payload")
		return
	}

	if input.Sender == appSettings.BotName {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored_bot_event"})
		return
	}

	job, err := s.jobs.Create(r.Context(), input)
	if err != nil {
		if errors.Is(err, jobs.ErrDuplicateJob) {
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "duplicate", "job": job})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create job")
		return
	}

	s.log.Info("queued review job", "job_id", job.ID, "repo", job.RepoFullName, "pr", job.PRNumber, "head_sha", job.HeadSHA)
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "job": job})
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	jobList, err := s.jobs.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobList})
}

func (s *Server) handleListFindings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	findings, err := s.jobs.ListFindings(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list findings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"findings": findings})
}

func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	job, err := s.jobs.Retry(r.Context(), id)
	if err != nil {
		if errors.Is(err, jobs.ErrJobNotRetryable) || errors.Is(err, jobs.ErrJobNotFound) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to retry job")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) isInitialized(r *http.Request) (bool, error) {
	if s.settings == nil {
		return true, nil
	}
	return s.settings.IsInitialized(r.Context())
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (int64, bool) {
	if s.settings == nil {
		return 0, true
	}
	initialized, err := s.settings.IsInitialized(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read setup status")
		return 0, false
	}
	if !initialized {
		writeError(w, http.StatusConflict, "setup required")
		return 0, false
	}
	userID, ok := readSessionUserID(r, s.sessionSecret)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return 0, false
	}
	return userID, true
}

func (s *Server) currentSettings(r *http.Request) (settings.AppSettings, error) {
	if s.settings == nil {
		return settings.FromConfig(s.cfg).Normalize(), nil
	}
	return s.settings.Load(r.Context())
}

func parseJobID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

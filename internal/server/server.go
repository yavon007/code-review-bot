package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"code-review-bot/internal/config"
	"code-review-bot/internal/jobs"
	"code-review-bot/internal/webhook"
)

type Server struct {
	cfg  config.Config
	jobs jobs.Store
	mux  *http.ServeMux
	log  *slog.Logger
}

func New(cfg config.Config, store jobs.Store, logger *slog.Logger) *Server {
	s := &Server{
		cfg:  cfg,
		jobs: store,
		mux:  http.NewServeMux(),
		log:  logger,
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
	s.mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	s.mux.HandleFunc("GET /api/jobs/{id}/findings", s.handleListFindings)
	s.mux.HandleFunc("POST /api/jobs/{id}/retry", s.handleRetryJob)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGiteaWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	eventName := r.Header.Get("X-Gitea-Event")
	deliveryID := r.Header.Get("X-Gitea-Delivery")
	signature := r.Header.Get("X-Gitea-Signature")
	hubSignature := r.Header.Get("X-Hub-Signature-256")

	if !webhook.VerifyGiteaSignature(s.cfg.GiteaWebhookSecret, body, signature, hubSignature) {
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

	if input.Sender == s.cfg.BotName {
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
	jobList, err := s.jobs.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobList})
}

func (s *Server) handleListFindings(w http.ResponseWriter, r *http.Request) {
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

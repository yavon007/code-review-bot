package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"code-review-bot/internal/config"
	"code-review-bot/internal/jobs"
)

func TestHealthz(t *testing.T) {
	handler := newTestServer("secret")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
}

func TestGiteaWebhookRejectsInvalidSignature(t *testing.T) {
	handler := newTestServer("secret")
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitea", bytes.NewReader(validPayload()))
	req.Header.Set("X-Gitea-Event", "pull_request")
	req.Header.Set("X-Gitea-Delivery", "delivery-1")
	req.Header.Set("X-Gitea-Signature", "bad-signature")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

func TestGiteaWebhookQueuesJob(t *testing.T) {
	secret := "secret"
	body := validPayload()
	handler := newTestServer(secret)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitea", bytes.NewReader(body))
	req.Header.Set("X-Gitea-Event", "pull_request")
	req.Header.Set("X-Gitea-Delivery", "delivery-1")
	req.Header.Set("X-Gitea-Signature", sign(secret, body))
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", res.Code, res.Body.String())
	}
}

func newTestServer(secret string) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{GiteaWebhookSecret: secret, BotName: "gpt-review-bot"}
	server, err := New(cfg, jobs.NewMemoryStore(), nil, logger)
	if err != nil {
		panic(err)
	}
	return server.Handler()
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func validPayload() []byte {
	return []byte(`{
		"action":"opened",
		"repository":{
			"full_name":"acme/order-service",
			"name":"order-service",
			"owner":{"username":"acme"}
		},
		"pull_request":{
			"index":123,
			"head":{"sha":"abc123"},
			"base":{"sha":"def456"}
		},
		"sender":{"username":"alice"}
	}`)
}

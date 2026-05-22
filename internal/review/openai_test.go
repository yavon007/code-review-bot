package review

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"code-review-bot/internal/jobs"
)

func TestOpenAIReviewerReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected authorization header")
		}

		var payload responsesRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "test-model" {
			t.Fatalf("unexpected model: %s", payload.Model)
		}
		if payload.Text.Format.Name != "gitea_pr_review" {
			t.Fatalf("unexpected schema name: %s", payload.Text.Format.Name)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output_text":"{\"summary\":\"整体风险较低。\",\"risk_level\":\"low\",\"decision\":\"comment\"}"}`))
	}))
	defer server.Close()

	reviewer, err := NewOpenAIReviewer(server.URL, "test-key", "test-model")
	if err != nil {
		t.Fatalf("NewOpenAIReviewer returned error: %v", err)
	}

	result, err := reviewer.Review(t.Context(), Input{Job: jobs.Job{RepoFullName: "acme/order-service", PRNumber: 123, HeadSHA: "abc123"}, Diff: "diff --git"})
	if err != nil {
		t.Fatalf("Review returned error: %v", err)
	}
	if result.Summary != "整体风险较低。" || result.RiskLevel != "low" || result.Decision != "comment" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

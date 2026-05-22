package gitea

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPullRequestFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/acme/order-service/pulls/123/files" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"filename":"main.go","status":"modified","additions":10,"deletions":2,"changes":12}]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	files, err := client.ListPullRequestFiles(t.Context(), "acme", "order-service", 123)
	if err != nil {
		t.Fatalf("ListPullRequestFiles returned error: %v", err)
	}
	if len(files) != 1 || files[0].Filename != "main.go" || files[0].Changes != 12 {
		t.Fatalf("unexpected files: %+v", files)
	}
}

func TestGetPullRequestDiffTruncates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/acme/order-service/pulls/123.diff" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	diff, truncated, err := client.GetPullRequestDiff(t.Context(), "acme", "order-service", 123, 3)
	if err != nil {
		t.Fatalf("GetPullRequestDiff returned error: %v", err)
	}
	if diff != "abc" || !truncated {
		t.Fatalf("unexpected diff/truncated: %q %v", diff, truncated)
	}
}

func TestCreateCommitStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/acme/order-service/statuses/abc123" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "token test-token" {
			t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
		}
		var payload CommitStatus
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.State != "pending" || payload.Context != "code-review-bot/review" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	err = client.CreateCommitStatus(t.Context(), "acme", "order-service", "abc123", CommitStatus{
		State:   "pending",
		Context: "code-review-bot/review",
	})
	if err != nil {
		t.Fatalf("CreateCommitStatus returned error: %v", err)
	}
}

func TestCreateIssueComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/acme/order-service/issues/123/comments" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["body"] != "review summary" {
			t.Fatalf("unexpected body: %s", payload["body"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42,"html_url":"https://gitea.local/comment/42"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	comment, err := client.CreateIssueComment(t.Context(), "acme", "order-service", 123, "review summary")
	if err != nil {
		t.Fatalf("CreateIssueComment returned error: %v", err)
	}
	if comment.ID != 42 {
		t.Fatalf("unexpected comment id: %d", comment.ID)
	}
}

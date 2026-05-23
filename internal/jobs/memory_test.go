package jobs

import (
	"context"
	"errors"
	"testing"

	"code-review-bot/internal/webhook"
)

func TestMemoryStoreCreateDeduplicatesReviewKey(t *testing.T) {
	store := NewMemoryStore()
	input := webhook.ReviewJobInput{
		DeliveryID:   "delivery-1",
		EventName:    "pull_request",
		RepoFullName: "acme/order-service",
		Owner:        "acme",
		Repo:         "order-service",
		PRNumber:     123,
		HeadSHA:      "abc123",
	}

	first, err := store.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	input.DeliveryID = "delivery-2"
	second, err := store.Create(context.Background(), input)
	if !errors.Is(err, ErrDuplicateJob) {
		t.Fatalf("expected duplicate job error, got %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected duplicate to return original job")
	}
}

func TestMemoryStoreCreateAllowsNewHeadSHA(t *testing.T) {
	store := NewMemoryStore()
	input := webhook.ReviewJobInput{
		DeliveryID:   "delivery-1",
		EventName:    "pull_request",
		RepoFullName: "acme/order-service",
		Owner:        "acme",
		Repo:         "order-service",
		PRNumber:     123,
		HeadSHA:      "abc123",
	}

	first, err := store.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	input.DeliveryID = "delivery-2"
	input.HeadSHA = "def456"
	second, err := store.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("Create returned error for new head sha: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("expected new head sha to create a new job")
	}
}

func TestMemoryStoreRetryResetsAttemptCount(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Create(context.Background(), webhook.ReviewJobInput{RepoFullName: "acme/order-service", Owner: "acme", Repo: "order-service", PRNumber: 123, HeadSHA: "abc123"}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	job, ok, err := store.ClaimQueued(context.Background(), "worker-1")
	if err != nil || !ok {
		t.Fatalf("ClaimQueued returned job=%v ok=%v err=%v", job, ok, err)
	}
	if job.AttemptCount != 1 {
		t.Fatalf("expected attempt count 1 after claim, got %d", job.AttemptCount)
	}
	if err := store.Fail(context.Background(), job.ID, "worker-1", StatusErrored, "failed"); err != nil {
		t.Fatalf("Fail returned error: %v", err)
	}

	retried, err := store.Retry(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Retry returned error: %v", err)
	}
	if retried.AttemptCount != 0 {
		t.Fatalf("expected retry to reset attempt count, got %d", retried.AttemptCount)
	}
}

func TestMemoryStoreSaveFindingsRequiresLease(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Create(context.Background(), webhook.ReviewJobInput{RepoFullName: "acme/order-service", Owner: "acme", Repo: "order-service", PRNumber: 123, HeadSHA: "abc123"}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	job, ok, err := store.ClaimQueued(context.Background(), "worker-1")
	if err != nil || !ok {
		t.Fatalf("ClaimQueued returned job=%v ok=%v err=%v", job, ok, err)
	}

	err = store.SaveFindings(context.Background(), job.ID, "worker-2", []ReviewFinding{{Title: "issue"}})
	if !errors.Is(err, ErrJobLeaseLost) {
		t.Fatalf("expected lease lost, got %v", err)
	}
}

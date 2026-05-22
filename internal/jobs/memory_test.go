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

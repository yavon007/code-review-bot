package jobs

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"code-review-bot/internal/webhook"
)

var ErrDuplicateJob = errors.New("duplicate review job")

type Store interface {
	Create(ctx context.Context, input webhook.ReviewJobInput) (Job, error)
	List(ctx context.Context) ([]Job, error)
	ClaimQueued(ctx context.Context) (Job, bool, error)
	Complete(ctx context.Context, id int64, summary string, commentID string) error
	Fail(ctx context.Context, id int64, status Status, message string) error
}

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusErrored   Status = "errored"
)

type Job struct {
	ID           int64     `json:"id"`
	DeliveryID   string    `json:"delivery_id"`
	EventName    string    `json:"event_name"`
	Action       string    `json:"action"`
	RepoFullName string    `json:"repo_full_name"`
	Owner        string    `json:"owner"`
	Repo         string    `json:"repo"`
	PRNumber     int       `json:"pr_number"`
	HeadSHA      string    `json:"head_sha"`
	BaseSHA      string    `json:"base_sha"`
	Sender       string    `json:"sender"`
	Status       Status    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	CommentID    string    `json:"gitea_comment_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type MemoryStore struct {
	mu           sync.Mutex
	nextID       int64
	jobs         map[int64]Job
	byReviewKey  map[string]int64
	byDeliveryID map[string]int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nextID:       1,
		jobs:         make(map[int64]Job),
		byReviewKey:  make(map[string]int64),
		byDeliveryID: make(map[string]int64),
	}
}

func (s *MemoryStore) Create(ctx context.Context, input webhook.ReviewJobInput) (Job, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	if input.DeliveryID != "" {
		if id, ok := s.byDeliveryID[input.DeliveryID]; ok {
			return s.jobs[id], ErrDuplicateJob
		}
	}

	key := reviewKey(input.RepoFullName, input.PRNumber, input.HeadSHA)
	if id, ok := s.byReviewKey[key]; ok {
		return s.jobs[id], ErrDuplicateJob
	}

	job := Job{
		ID:           s.nextID,
		DeliveryID:   input.DeliveryID,
		EventName:    input.EventName,
		Action:       input.Action,
		RepoFullName: input.RepoFullName,
		Owner:        input.Owner,
		Repo:         input.Repo,
		PRNumber:     input.PRNumber,
		HeadSHA:      input.HeadSHA,
		BaseSHA:      input.BaseSHA,
		Sender:       input.Sender,
		Status:       StatusQueued,
		CreatedAt:    time.Now().UTC(),
	}

	s.jobs[job.ID] = job
	s.byReviewKey[key] = job.ID
	if input.DeliveryID != "" {
		s.byDeliveryID[input.DeliveryID] = job.ID
	}
	s.nextID++

	return job, nil
}

func (s *MemoryStore) List(ctx context.Context) ([]Job, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		result = append(result, job)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID > result[j].ID
	})
	return result, nil
}

func (s *MemoryStore) ClaimQueued(ctx context.Context) (Job, bool, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	var selected Job
	for _, job := range s.jobs {
		if job.Status == StatusQueued && (selected.ID == 0 || job.ID < selected.ID) {
			selected = job
		}
	}
	if selected.ID == 0 {
		return Job{}, false, nil
	}
	selected.Status = StatusRunning
	s.jobs[selected.ID] = selected
	return selected, true, nil
}

func (s *MemoryStore) Complete(ctx context.Context, id int64, summary string, commentID string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.jobs[id]
	job.Status = StatusSucceeded
	job.Summary = summary
	job.CommentID = commentID
	s.jobs[id] = job
	return nil
}

func (s *MemoryStore) Fail(ctx context.Context, id int64, status Status, message string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.jobs[id]
	job.Status = status
	job.ErrorMessage = message
	s.jobs[id] = job
	return nil
}

func reviewKey(repoFullName string, prNumber int, headSHA string) string {
	return repoFullName + "#" + strconv.Itoa(prNumber) + "@" + headSHA
}

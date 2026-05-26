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

var (
	ErrDuplicateJob    = errors.New("duplicate review job")
	ErrJobNotFound     = errors.New("review job not found")
	ErrJobNotRetryable = errors.New("review job is not retryable")
	ErrJobLeaseLost    = errors.New("review job lease lost")
	ErrFindingNotFound = errors.New("review finding not found")
)

type Store interface {
	Create(ctx context.Context, input webhook.ReviewJobInput) (Job, error)
	List(ctx context.Context) ([]Job, error)
	ClaimQueued(ctx context.Context, workerID string) (Job, bool, error)
	Heartbeat(ctx context.Context, id int64, workerID string) error
	RecoverStale(ctx context.Context, staleTimeout time.Duration, maxAttempts int) (int64, error)
	ClaimPendingStatusSync(ctx context.Context, workerID string, limit int) ([]Job, error)
	MarkStatusSynced(ctx context.Context, id int64, workerID string) error
	MarkStatusSyncError(ctx context.Context, id int64, workerID string, message string) error
	RecordDelivery(ctx context.Context, input WebhookDeliveryInput) error
	ListDeliveries(ctx context.Context) ([]WebhookDelivery, error)
	RecordJobEvent(ctx context.Context, jobID int64, eventType string, message string) error
	ListJobEvents(ctx context.Context, jobID int64) ([]JobEvent, error)
	Complete(ctx context.Context, id int64, workerID string, status Status, summary string, commentID string) error
	Fail(ctx context.Context, id int64, workerID string, status Status, message string) error
	Retry(ctx context.Context, id int64) (Job, error)
	SaveSummaryComment(ctx context.Context, id int64, workerID string, commentID string) error
	SaveReviewUsage(ctx context.Context, id int64, workerID string, inputTokens int, outputTokens int, estimatedCost float64) error
	SaveFindings(ctx context.Context, jobID int64, workerID string, findings []ReviewFinding) error
	ListFindings(ctx context.Context, jobID int64) ([]ReviewFinding, error)
	MarkFindingPosted(ctx context.Context, jobID int64, workerID string, id int64, commentID string, commentURL string) error
	MarkFindingPostError(ctx context.Context, jobID int64, workerID string, id int64, message string) error
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
	ID            int64     `json:"id"`
	DeliveryID    string    `json:"delivery_id"`
	EventName     string    `json:"event_name"`
	Action        string    `json:"action"`
	RepoFullName  string    `json:"repo_full_name"`
	Owner         string    `json:"owner"`
	Repo          string    `json:"repo"`
	PRNumber      int       `json:"pr_number"`
	HeadSHA       string    `json:"head_sha"`
	BaseSHA       string    `json:"base_sha"`
	Sender        string    `json:"sender"`
	Status        Status    `json:"status"`
	AttemptCount  int       `json:"attempt_count"`
	ErrorMessage  string    `json:"error_message,omitempty"`
	Summary       string    `json:"summary,omitempty"`
	CommentID     string    `json:"gitea_comment_id,omitempty"`
	InputTokens   int       `json:"input_tokens,omitempty"`
	OutputTokens  int       `json:"output_tokens,omitempty"`
	EstimatedCost float64   `json:"estimated_cost,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type WebhookDelivery struct {
	ID             int64     `json:"id"`
	DeliveryID     string    `json:"delivery_id"`
	EventName      string    `json:"event_name"`
	Action         string    `json:"action"`
	RepoFullName   string    `json:"repo_full_name"`
	PRNumber       int       `json:"pr_number,omitempty"`
	HeadSHA        string    `json:"head_sha"`
	Sender         string    `json:"sender"`
	SignatureValid bool      `json:"signature_valid"`
	Status         string    `json:"status"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	JobID          int64     `json:"job_id,omitempty"`
	ReceivedAt     time.Time `json:"received_at"`
}

type WebhookDeliveryInput struct {
	DeliveryID     string
	EventName      string
	Action         string
	RepoFullName   string
	PRNumber       int
	HeadSHA        string
	Sender         string
	SignatureValid bool
	Status         string
	ErrorMessage   string
	JobID          int64
}

type JobEvent struct {
	ID        int64     `json:"id"`
	JobID     int64     `json:"job_id"`
	Type      string    `json:"type"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ReviewFinding struct {
	ID          int64   `json:"id"`
	JobID       int64   `json:"job_id"`
	Path        string  `json:"path"`
	Line        int     `json:"line,omitempty"`
	Severity    string  `json:"severity"`
	Category    string  `json:"category"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	Confidence  float64 `json:"confidence,omitempty"`
	IsInline    bool    `json:"is_inline"`
	IsPosted    bool    `json:"is_posted"`
	CommentID   string  `json:"gitea_comment_id,omitempty"`
	CommentURL  string  `json:"gitea_comment_url,omitempty"`
	PostError   string  `json:"post_error,omitempty"`
	FindingHash string  `json:"-"`
}

type MemoryStore struct {
	mu                sync.Mutex
	nextID            int64
	nextDeliveryID    int64
	nextFindingID     int64
	nextEventID       int64
	jobs              map[int64]Job
	findings          map[int64][]ReviewFinding
	events            map[int64][]JobEvent
	deliveries        []WebhookDelivery
	runningSince      map[int64]time.Time
	runningWorker     map[int64]string
	statusSyncPending map[int64]bool
	statusSyncWorker  map[int64]string
	byReviewKey       map[string]int64
	byDeliveryID      map[string]int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nextID:            1,
		nextDeliveryID:    1,
		nextFindingID:     1,
		nextEventID:       1,
		jobs:              make(map[int64]Job),
		findings:          make(map[int64][]ReviewFinding),
		events:            make(map[int64][]JobEvent),
		runningSince:      make(map[int64]time.Time),
		runningWorker:     make(map[int64]string),
		statusSyncPending: make(map[int64]bool),
		statusSyncWorker:  make(map[int64]string),
		byReviewKey:       make(map[string]int64),
		byDeliveryID:      make(map[string]int64),
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
	s.recordDeliveryLocked(WebhookDeliveryInput{
		DeliveryID:     input.DeliveryID,
		EventName:      input.EventName,
		Action:         input.Action,
		RepoFullName:   input.RepoFullName,
		PRNumber:       input.PRNumber,
		HeadSHA:        input.HeadSHA,
		Sender:         input.Sender,
		SignatureValid: true,
		Status:         "queued",
		JobID:          job.ID,
	})

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

func (s *MemoryStore) ClaimQueued(ctx context.Context, workerID string) (Job, bool, error) {
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
	selected.AttemptCount++
	s.jobs[selected.ID] = selected
	s.runningSince[selected.ID] = time.Now().UTC()
	s.runningWorker[selected.ID] = workerID
	return selected, true, nil
}

func (s *MemoryStore) Heartbeat(ctx context.Context, id int64, workerID string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok || job.Status != StatusRunning || s.runningWorker[id] != workerID {
		return ErrJobLeaseLost
	}
	s.runningSince[id] = time.Now().UTC()
	return nil
}

func (s *MemoryStore) RecoverStale(ctx context.Context, staleTimeout time.Duration, maxAttempts int) (int64, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	var recovered int64
	cutoff := time.Now().Add(-staleTimeout)
	for id, job := range s.jobs {
		startedAt, ok := s.runningSince[id]
		if job.Status != StatusRunning || !ok || startedAt.After(cutoff) {
			continue
		}
		if job.AttemptCount >= maxAttempts {
			job.Status = StatusErrored
			job.ErrorMessage = "review job stale after max attempts"
			s.statusSyncPending[id] = true
		} else {
			job.Status = StatusQueued
			job.ErrorMessage = ""
		}
		s.jobs[id] = job
		delete(s.runningSince, id)
		delete(s.runningWorker, id)
		recovered++
	}
	return recovered, nil
}

func (s *MemoryStore) ClaimPendingStatusSync(ctx context.Context, workerID string, limit int) ([]Job, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Job, 0)
	for id := range s.statusSyncPending {
		if len(result) >= limit {
			break
		}
		if s.statusSyncWorker[id] != "" {
			continue
		}
		job, ok := s.jobs[id]
		if !ok || !isTerminalStatus(job.Status) {
			continue
		}
		s.statusSyncWorker[id] = workerID
		result = append(result, job)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (s *MemoryStore) MarkStatusSynced(ctx context.Context, id int64, workerID string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.statusSyncWorker[id] != workerID {
		return ErrJobLeaseLost
	}
	delete(s.statusSyncPending, id)
	delete(s.statusSyncWorker, id)
	return nil
}

func (s *MemoryStore) MarkStatusSyncError(ctx context.Context, id int64, workerID string, message string) error {
	_ = ctx
	_ = message
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[id]; !ok {
		return ErrJobNotFound
	}
	if s.statusSyncWorker[id] != workerID {
		return ErrJobLeaseLost
	}
	s.statusSyncPending[id] = true
	delete(s.statusSyncWorker, id)
	return nil
}

func (s *MemoryStore) RecordDelivery(ctx context.Context, input WebhookDeliveryInput) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	s.recordDeliveryLocked(input)
	return nil
}

func (s *MemoryStore) recordDeliveryLocked(input WebhookDeliveryInput) {
	delivery := WebhookDelivery{
		ID:             s.nextDeliveryID,
		DeliveryID:     input.DeliveryID,
		EventName:      input.EventName,
		Action:         input.Action,
		RepoFullName:   input.RepoFullName,
		PRNumber:       input.PRNumber,
		HeadSHA:        input.HeadSHA,
		Sender:         input.Sender,
		SignatureValid: input.SignatureValid,
		Status:         input.Status,
		ErrorMessage:   input.ErrorMessage,
		JobID:          input.JobID,
		ReceivedAt:     time.Now().UTC(),
	}
	if delivery.Status == "" {
		delivery.Status = "received"
	}
	s.deliveries = append(s.deliveries, delivery)
	s.nextDeliveryID++
}

func (s *MemoryStore) ListDeliveries(ctx context.Context) ([]WebhookDelivery, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]WebhookDelivery, len(s.deliveries))
	copy(result, s.deliveries)
	sort.Slice(result, func(i, j int) bool {
		return result[i].ReceivedAt.After(result[j].ReceivedAt)
	})
	return result, nil
}

func (s *MemoryStore) RecordJobEvent(ctx context.Context, jobID int64, eventType string, message string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[jobID]; !ok {
		return ErrJobNotFound
	}
	s.events[jobID] = append(s.events[jobID], JobEvent{ID: s.nextEventID, JobID: jobID, Type: eventType, Message: message, CreatedAt: time.Now().UTC()})
	s.nextEventID++
	return nil
}

func (s *MemoryStore) ListJobEvents(ctx context.Context, jobID int64) ([]JobEvent, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]JobEvent, len(s.events[jobID]))
	copy(result, s.events[jobID])
	return result, nil
}

func (s *MemoryStore) Complete(ctx context.Context, id int64, workerID string, status Status, summary string, commentID string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok || job.Status != StatusRunning || s.runningWorker[id] != workerID {
		return ErrJobLeaseLost
	}
	job.Status = status
	job.Summary = summary
	job.CommentID = commentID
	job.ErrorMessage = ""
	s.jobs[id] = job
	delete(s.runningSince, id)
	delete(s.runningWorker, id)
	s.statusSyncPending[id] = true
	delete(s.statusSyncWorker, id)
	return nil
}

func (s *MemoryStore) Fail(ctx context.Context, id int64, workerID string, status Status, message string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok || job.Status != StatusRunning || s.runningWorker[id] != workerID {
		return ErrJobLeaseLost
	}
	job.Status = status
	job.ErrorMessage = message
	s.jobs[id] = job
	delete(s.runningSince, id)
	delete(s.runningWorker, id)
	s.statusSyncPending[id] = true
	delete(s.statusSyncWorker, id)
	return nil
}

func (s *MemoryStore) Retry(ctx context.Context, id int64) (Job, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	if job.Status != StatusErrored || s.statusSyncWorker[id] != "" {
		return Job{}, ErrJobNotRetryable
	}
	job.Status = StatusQueued
	job.AttemptCount = 0
	job.ErrorMessage = ""
	job.Summary = ""
	job.InputTokens = 0
	job.OutputTokens = 0
	job.EstimatedCost = 0
	s.jobs[id] = job
	delete(s.runningSince, id)
	delete(s.runningWorker, id)
	delete(s.statusSyncPending, id)
	delete(s.statusSyncWorker, id)
	return job, nil
}

func (s *MemoryStore) SaveSummaryComment(ctx context.Context, id int64, workerID string, commentID string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok || job.Status != StatusRunning || s.runningWorker[id] != workerID {
		return ErrJobLeaseLost
	}
	job.CommentID = commentID
	s.jobs[id] = job
	return nil
}

func (s *MemoryStore) SaveReviewUsage(ctx context.Context, id int64, workerID string, inputTokens int, outputTokens int, estimatedCost float64) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok || job.Status != StatusRunning || s.runningWorker[id] != workerID {
		return ErrJobLeaseLost
	}
	job.InputTokens = inputTokens
	job.OutputTokens = outputTokens
	job.EstimatedCost = estimatedCost
	s.jobs[id] = job
	return nil
}

func (s *MemoryStore) SaveFindings(ctx context.Context, jobID int64, workerID string, findings []ReviewFinding) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok || job.Status != StatusRunning || s.runningWorker[jobID] != workerID {
		return ErrJobLeaseLost
	}

	posted := make(map[string]ReviewFinding)
	for _, finding := range s.findings[jobID] {
		if finding.IsPosted {
			posted[memoryFindingKey(finding)] = finding
		}
	}

	next := make([]ReviewFinding, len(findings))
	for i, finding := range findings {
		if existing := posted[memoryFindingKey(finding)]; existing.IsPosted {
			finding.IsPosted = true
			finding.CommentID = existing.CommentID
			finding.CommentURL = existing.CommentURL
			finding.PostError = existing.PostError
		}
		if finding.ID == 0 {
			finding.ID = s.nextFindingID
			s.nextFindingID++
		}
		finding.JobID = jobID
		next[i] = finding
	}
	s.findings[jobID] = next
	return nil
}

func (s *MemoryStore) ListFindings(ctx context.Context, jobID int64) ([]ReviewFinding, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]ReviewFinding, len(s.findings[jobID]))
	copy(result, s.findings[jobID])
	return result, nil
}

func (s *MemoryStore) MarkFindingPosted(ctx context.Context, jobID int64, workerID string, id int64, commentID string, commentURL string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok || job.Status != StatusRunning || s.runningWorker[jobID] != workerID {
		return ErrJobLeaseLost
	}
	findings := s.findings[jobID]
	for i := range findings {
		if findings[i].ID == id {
			findings[i].IsPosted = true
			findings[i].CommentID = commentID
			findings[i].CommentURL = commentURL
			findings[i].PostError = ""
			s.findings[jobID] = findings
			return nil
		}
	}
	return ErrFindingNotFound
}

func (s *MemoryStore) MarkFindingPostError(ctx context.Context, jobID int64, workerID string, id int64, message string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok || job.Status != StatusRunning || s.runningWorker[jobID] != workerID {
		return ErrJobLeaseLost
	}
	findings := s.findings[jobID]
	for i := range findings {
		if findings[i].ID == id {
			findings[i].PostError = message
			s.findings[jobID] = findings
			return nil
		}
	}
	return ErrFindingNotFound
}

func isTerminalStatus(status Status) bool {
	return status == StatusSucceeded || status == StatusFailed || status == StatusErrored
}

func memoryFindingKey(finding ReviewFinding) string {
	if finding.FindingHash != "" {
		return finding.FindingHash
	}
	return finding.Path + "\x00" + strconv.Itoa(finding.Line) + "\x00" + finding.Severity + "\x00" + finding.Title + "\x00" + finding.Body
}

func reviewKey(repoFullName string, prNumber int, headSHA string) string {
	return repoFullName + "#" + strconv.Itoa(prNumber) + "@" + headSHA
}

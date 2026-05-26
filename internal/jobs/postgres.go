package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

import "code-review-bot/internal/webhook"

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) RecordDelivery(ctx context.Context, input WebhookDeliveryInput) error {
	return insertDelivery(ctx, s.db, input)
}

type deliveryExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertDelivery(ctx context.Context, db deliveryExecer, input WebhookDeliveryInput) error {
	status := input.Status
	if status == "" {
		status = "received"
	}
	_, err := db.ExecContext(ctx, `
		insert into webhook_deliveries (
			delivery_id, event_name, action, repo_full_name, pr_number, head_sha, sender, signature_valid, status, error_message, job_id
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, nullString(input.DeliveryID), input.EventName, input.Action, nullString(input.RepoFullName), nullInt(input.PRNumber), input.HeadSHA, input.Sender, input.SignatureValid, status, nullString(input.ErrorMessage), nullInt64(input.JobID))
	return err
}

func (s *PostgresStore) ListDeliveries(ctx context.Context) ([]WebhookDelivery, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, coalesce(delivery_id, ''), event_name, coalesce(action, ''), coalesce(repo_full_name, ''), coalesce(pr_number, 0), coalesce(head_sha, ''),
			coalesce(sender, ''), signature_valid, status, coalesce(error_message, ''), coalesce(job_id, 0), received_at
		from webhook_deliveries
		order by received_at desc
		limit 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]WebhookDelivery, 0)
	for rows.Next() {
		var delivery WebhookDelivery
		if err := rows.Scan(
			&delivery.ID,
			&delivery.DeliveryID,
			&delivery.EventName,
			&delivery.Action,
			&delivery.RepoFullName,
			&delivery.PRNumber,
			&delivery.HeadSHA,
			&delivery.Sender,
			&delivery.SignatureValid,
			&delivery.Status,
			&delivery.ErrorMessage,
			&delivery.JobID,
			&delivery.ReceivedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PostgresStore) Create(ctx context.Context, input webhook.ReviewJobInput) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()

	if input.DeliveryID != "" {
		job, findErr := s.findByDeliveryID(ctx, tx, input.DeliveryID)
		if findErr == nil {
			return job, ErrDuplicateJob
		}
	}

	var job Job
	err = tx.QueryRowContext(ctx, `
		insert into review_jobs (
			delivery_id, event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		on conflict do nothing
		returning id, coalesce(delivery_id, ''), event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status, attempt_count, coalesce(error_message, ''),
			coalesce(summary, ''), coalesce(gitea_comment_id, ''), coalesce(input_tokens, 0), coalesce(output_tokens, 0), coalesce(estimated_cost, 0), created_at
	`, nullString(input.DeliveryID), input.EventName, input.Action, input.RepoFullName, input.Owner, input.Repo,
		input.PRNumber, input.HeadSHA, input.BaseSHA, input.Sender, StatusQueued).Scan(
		&job.ID,
		&job.DeliveryID,
		&job.EventName,
		&job.Action,
		&job.RepoFullName,
		&job.Owner,
		&job.Repo,
		&job.PRNumber,
		&job.HeadSHA,
		&job.BaseSHA,
		&job.Sender,
		&job.Status,
		&job.AttemptCount,
		&job.ErrorMessage,
		&job.Summary,
		&job.CommentID,
		&job.InputTokens,
		&job.OutputTokens,
		&job.EstimatedCost,
		&job.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if input.DeliveryID != "" {
			job, findErr := s.findByDeliveryID(ctx, tx, input.DeliveryID)
			if findErr == nil {
				return job, ErrDuplicateJob
			}
		}
		job, findErr := s.findByReviewKey(ctx, tx, input.RepoFullName, input.PRNumber, input.HeadSHA)
		if findErr == nil {
			return job, ErrDuplicateJob
		}
		return Job{}, ErrDuplicateJob
	}
	if err != nil {
		return Job{}, fmt.Errorf("insert review job: %w", err)
	}

	if err := insertDelivery(ctx, tx, WebhookDeliveryInput{
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
	}); err != nil {
		return Job{}, fmt.Errorf("record queued delivery: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *PostgresStore) RecordJobEvent(ctx context.Context, jobID int64, eventType string, message string) error {
	_, err := s.db.ExecContext(ctx, `
		insert into review_job_events (job_id, event_type, message)
		values ($1, $2, $3)
	`, jobID, eventType, nullString(message))
	return err
}

func (s *PostgresStore) ListJobEvents(ctx context.Context, jobID int64) ([]JobEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, job_id, event_type, coalesce(message, ''), created_at
		from review_job_events
		where job_id = $1
		order by id asc
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]JobEvent, 0)
	for rows.Next() {
		var event JobEvent
		if err := rows.Scan(&event.ID, &event.JobID, &event.Type, &event.Message, &event.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PostgresStore) List(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, jobSelectQuery()+`
		from review_jobs
		order by id desc
		limit 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PostgresStore) ClaimQueued(ctx context.Context, workerID string) (Job, bool, error) {
	var job Job
	err := s.db.QueryRowContext(ctx, `
		with selected as (
			select id
			from review_jobs
			where status = $1
			order by id asc
			for update skip locked
			limit 1
		)
		update review_jobs
		set status = $2, started_at = now(), heartbeat_at = now(), worker_id = $3, attempt_count = attempt_count + 1
		where id = (select id from selected)
		returning id, coalesce(delivery_id, ''), event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status, attempt_count, coalesce(error_message, ''),
			coalesce(summary, ''), coalesce(gitea_comment_id, ''), coalesce(input_tokens, 0), coalesce(output_tokens, 0), coalesce(estimated_cost, 0), created_at
	`, StatusQueued, StatusRunning, workerID).Scan(
		&job.ID,
		&job.DeliveryID,
		&job.EventName,
		&job.Action,
		&job.RepoFullName,
		&job.Owner,
		&job.Repo,
		&job.PRNumber,
		&job.HeadSHA,
		&job.BaseSHA,
		&job.Sender,
		&job.Status,
		&job.AttemptCount,
		&job.ErrorMessage,
		&job.Summary,
		&job.CommentID,
		&job.InputTokens,
		&job.OutputTokens,
		&job.EstimatedCost,
		&job.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	return job, true, nil
}

func (s *PostgresStore) Heartbeat(ctx context.Context, id int64, workerID string) error {
	result, err := s.db.ExecContext(ctx, `
		update review_jobs
		set heartbeat_at = now()
		where id = $1 and status = $2 and worker_id = $3
	`, id, StatusRunning, workerID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrJobLeaseLost
	}
	return nil
}

func (s *PostgresStore) RecoverStale(ctx context.Context, staleTimeout time.Duration, maxAttempts int) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		update review_jobs
		set status = $1, error_message = null, queued_at = now(), started_at = null, heartbeat_at = null, worker_id = null, stale_at = now()
		where status = $2 and coalesce(heartbeat_at, started_at) < now() - ($3 * interval '1 second') and attempt_count < $4
	`, StatusQueued, StatusRunning, staleTimeout.Seconds(), maxAttempts)
	if err != nil {
		return 0, err
	}
	recovered, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	result, err = s.db.ExecContext(ctx, `
		update review_jobs
		set status = $1, error_message = $2, finished_at = now(), stale_at = now(), heartbeat_at = null, worker_id = null,
			status_sync_pending = true, status_sync_error = null, status_sync_attempt_count = 0,
			next_status_sync_at = null, status_sync_worker_id = null, status_sync_started_at = null
		where status = $3 and coalesce(heartbeat_at, started_at) < now() - ($4 * interval '1 second') and attempt_count >= $5
	`, StatusErrored, "review job stale after max attempts", StatusRunning, staleTimeout.Seconds(), maxAttempts)
	if err != nil {
		return 0, err
	}
	expired, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return recovered + expired, nil
}

func (s *PostgresStore) ClaimPendingStatusSync(ctx context.Context, workerID string, limit int) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `
		with selected as (
			select id
			from review_jobs
			where status_sync_pending = true
				and status in ($1, $2, $3)
				and (next_status_sync_at is null or next_status_sync_at <= now())
				and (status_sync_worker_id is null or status_sync_started_at < now() - interval '2 minutes')
			order by coalesce(next_status_sync_at, created_at) asc, id asc
			for update skip locked
			limit $4
		)
		update review_jobs
		set status_sync_worker_id = $5, status_sync_started_at = now()
		where id in (select id from selected)
		returning id, coalesce(delivery_id, ''), event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status, attempt_count, coalesce(error_message, ''),
			coalesce(summary, ''), coalesce(gitea_comment_id, ''), coalesce(input_tokens, 0), coalesce(output_tokens, 0), coalesce(estimated_cost, 0), created_at
	`, StatusSucceeded, StatusFailed, StatusErrored, limit, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PostgresStore) MarkStatusSynced(ctx context.Context, id int64, workerID string) error {
	result, err := s.db.ExecContext(ctx, `
		update review_jobs
		set status_sync_pending = false, status_sync_error = null, status_sync_attempt_count = 0,
			next_status_sync_at = null, status_sync_worker_id = null, status_sync_started_at = null, status_synced_at = now()
		where id = $1 and status_sync_worker_id = $2 and status_sync_pending = true
	`, id, workerID)
	return leaseUpdateErr(result, err)
}

func (s *PostgresStore) MarkStatusSyncError(ctx context.Context, id int64, workerID string, message string) error {
	result, err := s.db.ExecContext(ctx, `
		update review_jobs
		set status_sync_pending = true,
			status_sync_error = $1,
			status_sync_attempt_count = status_sync_attempt_count + 1,
			next_status_sync_at = now() + (least(300, (status_sync_attempt_count + 1) * 30) * interval '1 second'),
			status_sync_worker_id = null,
			status_sync_started_at = null
		where id = $2 and status_sync_worker_id = $3 and status_sync_pending = true
	`, message, id, workerID)
	return leaseUpdateErr(result, err)
}

func (s *PostgresStore) Complete(ctx context.Context, id int64, workerID string, status Status, summary string, commentID string) error {
	result, err := s.db.ExecContext(ctx, `
		update review_jobs
		set status = $1, summary = $2, gitea_comment_id = $3, finished_at = now(), error_message = null, heartbeat_at = null, worker_id = null,
			status_sync_pending = true, status_sync_error = null, status_sync_attempt_count = 0,
			next_status_sync_at = null, status_sync_worker_id = null, status_sync_started_at = null
		where id = $4 and worker_id = $5 and status = $6
	`, status, summary, commentID, id, workerID, StatusRunning)
	return leaseUpdateErr(result, err)
}

func (s *PostgresStore) Fail(ctx context.Context, id int64, workerID string, status Status, message string) error {
	result, err := s.db.ExecContext(ctx, `
		update review_jobs
		set status = $1, error_message = $2, finished_at = now(), heartbeat_at = null, worker_id = null,
			status_sync_pending = true, status_sync_error = null, status_sync_attempt_count = 0,
			next_status_sync_at = null, status_sync_worker_id = null, status_sync_started_at = null
		where id = $3 and worker_id = $4 and status = $5
	`, status, message, id, workerID, StatusRunning)
	return leaseUpdateErr(result, err)
}

func (s *PostgresStore) Retry(ctx context.Context, id int64) (Job, error) {
	var job Job
	err := s.db.QueryRowContext(ctx, `
		update review_jobs
		set status = $1, attempt_count = 0, error_message = null, summary = null, started_at = null, heartbeat_at = null, worker_id = null,
			finished_at = null, queued_at = now(), status_sync_pending = false, status_sync_error = null,
			status_sync_attempt_count = 0, next_status_sync_at = null, status_sync_worker_id = null, status_sync_started_at = null
		where id = $2 and status = $3 and (status_sync_worker_id is null or status_sync_started_at < now() - interval '2 minutes')
		returning id, coalesce(delivery_id, ''), event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status, attempt_count, coalesce(error_message, ''),
			coalesce(summary, ''), coalesce(gitea_comment_id, ''), coalesce(input_tokens, 0), coalesce(output_tokens, 0), coalesce(estimated_cost, 0), created_at
	`, StatusQueued, id, StatusErrored).Scan(
		&job.ID,
		&job.DeliveryID,
		&job.EventName,
		&job.Action,
		&job.RepoFullName,
		&job.Owner,
		&job.Repo,
		&job.PRNumber,
		&job.HeadSHA,
		&job.BaseSHA,
		&job.Sender,
		&job.Status,
		&job.AttemptCount,
		&job.ErrorMessage,
		&job.Summary,
		&job.CommentID,
		&job.InputTokens,
		&job.OutputTokens,
		&job.EstimatedCost,
		&job.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotRetryable
	}
	return job, err
}

func (s *PostgresStore) SaveSummaryComment(ctx context.Context, id int64, workerID string, commentID string) error {
	result, err := s.db.ExecContext(ctx, `
		update review_jobs
		set gitea_comment_id = $1
		where id = $2 and worker_id = $3 and status = $4
	`, commentID, id, workerID, StatusRunning)
	return leaseUpdateErr(result, err)
}

func (s *PostgresStore) SaveReviewUsage(ctx context.Context, id int64, workerID string, inputTokens int, outputTokens int, estimatedCost float64) error {
	result, err := s.db.ExecContext(ctx, `
		update review_jobs
		set input_tokens = $1, output_tokens = $2, estimated_cost = $3
		where id = $4 and worker_id = $5 and status = $6
	`, inputTokens, outputTokens, estimatedCost, id, workerID, StatusRunning)
	return leaseUpdateErr(result, err)
}

func (s *PostgresStore) SaveFindings(ctx context.Context, jobID int64, workerID string, findings []ReviewFinding) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var lease int
	if err := tx.QueryRowContext(ctx, `
		select 1
		from review_jobs
		where id = $1 and status = $2 and worker_id = $3
		for update
	`, jobID, StatusRunning, workerID).Scan(&lease); errors.Is(err, sql.ErrNoRows) {
		return ErrJobLeaseLost
	} else if err != nil {
		return err
	}

	postedByHash, err := existingPostedFindings(ctx, tx, jobID)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `delete from review_findings where job_id = $1`, jobID); err != nil {
		return err
	}

	for _, finding := range findings {
		line := any(nil)
		if finding.Line > 0 {
			line = finding.Line
		}
		hash := finding.FindingHash
		if hash == "" {
			hash = findingHash(jobID, finding)
		}
		posted := postedByHash[hash]
		if posted.IsPosted {
			finding.IsPosted = true
			finding.CommentID = posted.CommentID
			finding.CommentURL = posted.CommentURL
			finding.PostError = posted.PostError
		}
		_, err := tx.ExecContext(ctx, `
			insert into review_findings (
				job_id, finding_hash, path, side, line, severity, category, title, body, confidence, is_inline, is_posted,
				gitea_comment_id, gitea_comment_url, post_error
			) values ($1, $2, $3, 'RIGHT', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			on conflict (finding_hash) do update set
				path = excluded.path,
				line = excluded.line,
				severity = excluded.severity,
				category = excluded.category,
				title = excluded.title,
				body = excluded.body,
				confidence = excluded.confidence,
				is_inline = excluded.is_inline,
				is_posted = excluded.is_posted,
				gitea_comment_id = excluded.gitea_comment_id,
				gitea_comment_url = excluded.gitea_comment_url,
				post_error = excluded.post_error
		`, jobID, hash, finding.Path, line, finding.Severity, finding.Category, finding.Title, finding.Body, finding.Confidence, finding.IsInline, finding.IsPosted, nullString(finding.CommentID), nullString(finding.CommentURL), nullString(finding.PostError))
		if err != nil {
			return fmt.Errorf("insert review finding: %w", err)
		}
	}
	return tx.Commit()
}

func existingPostedFindings(ctx context.Context, tx *sql.Tx, jobID int64) (map[string]ReviewFinding, error) {
	rows, err := tx.QueryContext(ctx, `
		select finding_hash, is_posted, coalesce(gitea_comment_id, ''), coalesce(gitea_comment_url, ''), coalesce(post_error, '')
		from review_findings
		where job_id = $1 and is_posted = true
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]ReviewFinding)
	for rows.Next() {
		var finding ReviewFinding
		if err := rows.Scan(&finding.FindingHash, &finding.IsPosted, &finding.CommentID, &finding.CommentURL, &finding.PostError); err != nil {
			return nil, err
		}
		result[finding.FindingHash] = finding
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PostgresStore) ListFindings(ctx context.Context, jobID int64) ([]ReviewFinding, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, job_id, path, line, severity, category, title, body, confidence, is_inline, is_posted,
			coalesce(gitea_comment_id, ''), coalesce(gitea_comment_url, ''), coalesce(post_error, ''), finding_hash
		from review_findings
		where job_id = $1
		order by id asc
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]ReviewFinding, 0)
	for rows.Next() {
		var finding ReviewFinding
		var line sql.NullInt64
		var confidence sql.NullFloat64
		if err := rows.Scan(
			&finding.ID,
			&finding.JobID,
			&finding.Path,
			&line,
			&finding.Severity,
			&finding.Category,
			&finding.Title,
			&finding.Body,
			&confidence,
			&finding.IsInline,
			&finding.IsPosted,
			&finding.CommentID,
			&finding.CommentURL,
			&finding.PostError,
			&finding.FindingHash,
		); err != nil {
			return nil, err
		}
		if line.Valid {
			finding.Line = int(line.Int64)
		}
		if confidence.Valid {
			finding.Confidence = confidence.Float64
		}
		result = append(result, finding)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PostgresStore) MarkFindingPosted(ctx context.Context, jobID int64, workerID string, id int64, commentID string, commentURL string) error {
	result, err := s.db.ExecContext(ctx, `
		update review_findings
		set is_posted = true, gitea_comment_id = $1, gitea_comment_url = $2, post_error = null
		where id = $3 and job_id = $4 and exists (
			select 1 from review_jobs
			where review_jobs.id = review_findings.job_id and review_jobs.status = $5 and review_jobs.worker_id = $6
		)
	`, commentID, commentURL, id, jobID, StatusRunning, workerID)
	return leaseUpdateErr(result, err)
}

func (s *PostgresStore) MarkFindingPostError(ctx context.Context, jobID int64, workerID string, id int64, message string) error {
	result, err := s.db.ExecContext(ctx, `
		update review_findings
		set post_error = $1
		where id = $2 and job_id = $3 and exists (
			select 1 from review_jobs
			where review_jobs.id = review_findings.job_id and review_jobs.status = $4 and review_jobs.worker_id = $5
		)
	`, message, id, jobID, StatusRunning, workerID)
	return leaseUpdateErr(result, err)
}

func (s *PostgresStore) findByDeliveryID(ctx context.Context, tx *sql.Tx, deliveryID string) (Job, error) {
	return queryOneJob(ctx, tx, jobSelectQuery()+`
		from review_jobs
		where delivery_id = $1
	`, deliveryID)
}

func (s *PostgresStore) findByReviewKey(ctx context.Context, tx *sql.Tx, repoFullName string, prNumber int, headSHA string) (Job, error) {
	return queryOneJob(ctx, tx, jobSelectQuery()+`
		from review_jobs
		where repo_full_name = $1 and pr_number = $2 and head_sha = $3
	`, repoFullName, prNumber, headSHA)
}

func jobSelectQuery() string {
	return `
		select id, coalesce(delivery_id, ''), event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status, attempt_count, coalesce(error_message, ''),
			coalesce(summary, ''), coalesce(gitea_comment_id, ''), coalesce(input_tokens, 0), coalesce(output_tokens, 0), coalesce(estimated_cost, 0), created_at
	`
}

type rowScanner interface {
	Scan(dest ...any) error
}

type jobQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func queryOneJob(ctx context.Context, db jobQuerier, query string, args ...any) (Job, error) {
	job, err := scanJob(db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, err
	}
	return job, err
}

func scanJob(scanner rowScanner) (Job, error) {
	var job Job
	err := scanner.Scan(
		&job.ID,
		&job.DeliveryID,
		&job.EventName,
		&job.Action,
		&job.RepoFullName,
		&job.Owner,
		&job.Repo,
		&job.PRNumber,
		&job.HeadSHA,
		&job.BaseSHA,
		&job.Sender,
		&job.Status,
		&job.AttemptCount,
		&job.ErrorMessage,
		&job.Summary,
		&job.CommentID,
		&job.InputTokens,
		&job.OutputTokens,
		&job.EstimatedCost,
		&job.CreatedAt,
	)
	return job, err
}

func leaseUpdateErr(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrJobLeaseLost
	}
	return nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func findingHash(jobID int64, finding ReviewFinding) string {
	hash := sha256.Sum256([]byte(strconv.FormatInt(jobID, 10) + "\x00" + finding.Path + "\x00" + strconv.Itoa(finding.Line) + "\x00" + finding.Severity + "\x00" + finding.Title + "\x00" + finding.Body))
	return hex.EncodeToString(hash[:])
}

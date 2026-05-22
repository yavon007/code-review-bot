package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

import "code-review-bot/internal/webhook"

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Create(ctx context.Context, input webhook.ReviewJobInput) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()

	if input.DeliveryID != "" {
		result, err := tx.ExecContext(ctx, `
			insert into webhook_deliveries (delivery_id, event_name, repo_full_name, pr_number, head_sha, signature_valid)
			values ($1, $2, $3, $4, $5, true)
			on conflict (delivery_id) do nothing
		`, input.DeliveryID, input.EventName, input.RepoFullName, input.PRNumber, input.HeadSHA)
		if err != nil {
			return Job{}, fmt.Errorf("insert webhook delivery: %w", err)
		}
		if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected == 0 {
			job, findErr := s.findByDeliveryID(ctx, tx, input.DeliveryID)
			if findErr == nil {
				return job, ErrDuplicateJob
			}
			return Job{}, ErrDuplicateJob
		}
	}

	var job Job
	err = tx.QueryRowContext(ctx, `
		insert into review_jobs (
			delivery_id, event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		on conflict (repo_full_name, pr_number, head_sha) do nothing
		returning id, delivery_id, event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status, attempt_count, coalesce(error_message, ''),
			coalesce(summary, ''), coalesce(gitea_comment_id, ''), created_at
	`, input.DeliveryID, input.EventName, input.Action, input.RepoFullName, input.Owner, input.Repo,
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
		&job.CreatedAt,
	)
	if err != nil {
		job, findErr := s.findByReviewKey(ctx, tx, input.RepoFullName, input.PRNumber, input.HeadSHA)
		if findErr == nil {
			return job, ErrDuplicateJob
		}
		return Job{}, fmt.Errorf("insert review job: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return job, nil
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

func (s *PostgresStore) ClaimQueued(ctx context.Context) (Job, bool, error) {
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
		set status = $2, started_at = now(), attempt_count = attempt_count + 1
		where id = (select id from selected)
		returning id, delivery_id, event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status, attempt_count, coalesce(error_message, ''),
			coalesce(summary, ''), coalesce(gitea_comment_id, ''), created_at
	`, StatusQueued, StatusRunning).Scan(
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

func (s *PostgresStore) Complete(ctx context.Context, id int64, status Status, summary string, commentID string) error {
	_, err := s.db.ExecContext(ctx, `
		update review_jobs
		set status = $1, summary = $2, gitea_comment_id = $3, finished_at = now(), error_message = null
		where id = $4
	`, status, summary, commentID, id)
	return err
}

func (s *PostgresStore) Fail(ctx context.Context, id int64, status Status, message string) error {
	_, err := s.db.ExecContext(ctx, `
		update review_jobs
		set status = $1, error_message = $2, finished_at = now()
		where id = $3
	`, status, message, id)
	return err
}

func (s *PostgresStore) Retry(ctx context.Context, id int64) (Job, error) {
	var job Job
	err := s.db.QueryRowContext(ctx, `
		update review_jobs
		set status = $1, error_message = null, summary = null, started_at = null, finished_at = null, queued_at = now()
		where id = $2 and status = $3
		returning id, delivery_id, event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status, attempt_count, coalesce(error_message, ''),
			coalesce(summary, ''), coalesce(gitea_comment_id, ''), created_at
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
		&job.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotRetryable
	}
	return job, err
}

func (s *PostgresStore) SaveFindings(ctx context.Context, jobID int64, findings []ReviewFinding) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

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
		_, err := tx.ExecContext(ctx, `
			insert into review_findings (
				job_id, finding_hash, path, side, line, severity, category, title, body, confidence, is_inline, is_posted
			) values ($1, $2, $3, 'RIGHT', $4, $5, $6, $7, $8, $9, $10, $11)
			on conflict (finding_hash) do update set
				path = excluded.path,
				line = excluded.line,
				severity = excluded.severity,
				category = excluded.category,
				title = excluded.title,
				body = excluded.body,
				confidence = excluded.confidence
		`, jobID, hash, finding.Path, line, finding.Severity, finding.Category, finding.Title, finding.Body, finding.Confidence, finding.IsInline, finding.IsPosted)
		if err != nil {
			return fmt.Errorf("insert review finding: %w", err)
		}
	}
	return tx.Commit()
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
		select id, delivery_id, event_name, action, repo_full_name, owner_name, repo_name,
			pr_number, head_sha, base_sha, sender, status, attempt_count, coalesce(error_message, ''),
			coalesce(summary, ''), coalesce(gitea_comment_id, ''), created_at
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
		&job.CreatedAt,
	)
	return job, err
}

func findingHash(jobID int64, finding ReviewFinding) string {
	hash := sha256.Sum256([]byte(strconv.FormatInt(jobID, 10) + "\x00" + finding.Path + "\x00" + strconv.Itoa(finding.Line) + "\x00" + finding.Severity + "\x00" + finding.Title + "\x00" + finding.Body))
	return hex.EncodeToString(hash[:])
}

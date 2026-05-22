package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"code-review-bot/internal/gitea"
	"code-review-bot/internal/jobs"
	"code-review-bot/internal/review"
)

type Worker struct {
	store        jobs.Store
	gitea        *gitea.Client
	reviewer     review.Reviewer
	interval     time.Duration
	maxDiffBytes int64
	logger       *slog.Logger
}

func New(store jobs.Store, giteaClient *gitea.Client, reviewer review.Reviewer, interval time.Duration, maxDiffBytes int64, logger *slog.Logger) *Worker {
	return &Worker{
		store:        store,
		gitea:        giteaClient,
		reviewer:     reviewer,
		interval:     interval,
		maxDiffBytes: maxDiffBytes,
		logger:       logger,
	}
}

func (w *Worker) Run(ctx context.Context) {
	if w.interval <= 0 {
		w.interval = 5 * time.Second
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		w.processOne(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) processOne(ctx context.Context) {
	job, ok, err := w.store.ClaimQueued(ctx)
	if err != nil {
		w.logger.Error("failed to claim review job", "error", err)
		return
	}
	if !ok {
		return
	}

	w.logger.Info("processing review job", "job_id", job.ID, "repo", job.RepoFullName, "pr", job.PRNumber, "head_sha", job.HeadSHA)

	if err := w.gitea.CreateCommitStatus(ctx, job.Owner, job.Repo, job.HeadSHA, gitea.CommitStatus{
		State:       "pending",
		Description: "Code review bot is reviewing this PR.",
		Context:     "code-review-bot/review",
	}); err != nil {
		w.fail(ctx, job, jobs.StatusErrored, fmt.Errorf("create pending status: %w", err))
		return
	}

	changedFiles, err := w.gitea.ListPullRequestFiles(ctx, job.Owner, job.Repo, job.PRNumber)
	if err != nil {
		w.fail(ctx, job, jobs.StatusErrored, fmt.Errorf("list pull request files: %w", err))
		return
	}

	diff, truncated, err := w.gitea.GetPullRequestDiff(ctx, job.Owner, job.Repo, job.PRNumber, w.maxDiffBytes)
	if err != nil {
		w.fail(ctx, job, jobs.StatusErrored, fmt.Errorf("get pull request diff: %w", err))
		return
	}

	result, err := w.reviewer.Review(ctx, review.Input{Job: job, ChangedFiles: changedFiles, Diff: diff, DiffTruncated: truncated})
	if err != nil {
		w.fail(ctx, job, jobs.StatusErrored, fmt.Errorf("review failed: %w", err))
		return
	}

	comment, err := w.gitea.CreateIssueComment(ctx, job.Owner, job.Repo, job.PRNumber, formatSummary(job, result))
	if err != nil {
		w.fail(ctx, job, jobs.StatusErrored, fmt.Errorf("create summary comment: %w", err))
		return
	}

	if err := w.gitea.CreateCommitStatus(ctx, job.Owner, job.Repo, job.HeadSHA, gitea.CommitStatus{
		State:       "success",
		Description: "Code review bot completed review.",
		Context:     "code-review-bot/review",
	}); err != nil {
		w.fail(ctx, job, jobs.StatusErrored, fmt.Errorf("create success status: %w", err))
		return
	}

	if err := w.store.Complete(ctx, job.ID, result.Summary, fmt.Sprintf("%d", comment.ID)); err != nil {
		w.logger.Error("failed to mark review job complete", "job_id", job.ID, "error", err)
		return
	}
	w.logger.Info("completed review job", "job_id", job.ID)
}

func (w *Worker) fail(ctx context.Context, job jobs.Job, status jobs.Status, err error) {
	w.logger.Error("review job failed", "job_id", job.ID, "error", err)
	_ = w.gitea.CreateCommitStatus(ctx, job.Owner, job.Repo, job.HeadSHA, gitea.CommitStatus{
		State:       "error",
		Description: "Code review bot failed to review this PR.",
		Context:     "code-review-bot/review",
	})
	if markErr := w.store.Fail(ctx, job.ID, status, err.Error()); markErr != nil {
		w.logger.Error("failed to mark review job failed", "job_id", job.ID, "error", markErr)
	}
}

func formatSummary(job jobs.Job, result review.Result) string {
	return fmt.Sprintf("## Code Review Bot\n\n%s\n\nRisk: `%s`  \nDecision: `%s`\n\n---\nPR: #%d  \nHead SHA: `%s`", result.Summary, result.RiskLevel, result.Decision, job.PRNumber, job.HeadSHA)
}

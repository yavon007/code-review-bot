package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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

	findings := reviewFindings(job.ID, result.Findings)
	if err := w.store.SaveFindings(ctx, job.ID, findings); err != nil {
		w.fail(ctx, job, jobs.StatusErrored, fmt.Errorf("save review findings: %w", err))
		return
	}

	comment, err := w.gitea.CreateIssueComment(ctx, job.Owner, job.Repo, job.PRNumber, formatSummary(job, result))
	if err != nil {
		w.fail(ctx, job, jobs.StatusErrored, fmt.Errorf("create summary comment: %w", err))
		return
	}

	finalStatus := reviewJobStatus(result)
	if err := w.gitea.CreateCommitStatus(ctx, job.Owner, job.Repo, job.HeadSHA, gitea.CommitStatus{
		State:       commitStatusState(finalStatus),
		Description: commitStatusDescription(finalStatus),
		Context:     "code-review-bot/review",
	}); err != nil {
		w.fail(ctx, job, jobs.StatusErrored, fmt.Errorf("create final status: %w", err))
		return
	}

	if err := w.store.Complete(ctx, job.ID, finalStatus, result.Summary, fmt.Sprintf("%d", comment.ID)); err != nil {
		w.logger.Error("failed to mark review job complete", "job_id", job.ID, "error", err)
		return
	}
	w.logger.Info("completed review job", "job_id", job.ID, "status", finalStatus, "findings", len(findings))
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
	var builder strings.Builder
	builder.WriteString("## Code Review Bot\n\n")
	builder.WriteString(result.Summary)
	builder.WriteString(fmt.Sprintf("\n\nRisk: `%s`  \nDecision: `%s`", result.RiskLevel, result.Decision))
	if len(result.Findings) > 0 {
		builder.WriteString("\n\n### Findings\n")
		for _, finding := range result.Findings {
			location := finding.Path
			if finding.Line > 0 {
				location = fmt.Sprintf("%s:%d", finding.Path, finding.Line)
			}
			builder.WriteString(fmt.Sprintf("\n- **[%s/%s] %s** (`%s`)\n  %s\n", finding.Severity, finding.Category, finding.Title, location, finding.Body))
		}
	}
	builder.WriteString(fmt.Sprintf("\n---\nPR: #%d  \nHead SHA: `%s`", job.PRNumber, job.HeadSHA))
	return builder.String()
}

func reviewJobStatus(result review.Result) jobs.Status {
	if result.Decision == "request_changes" || result.RiskLevel == "high" {
		return jobs.StatusFailed
	}
	return jobs.StatusSucceeded
}

func commitStatusState(status jobs.Status) string {
	if status == jobs.StatusFailed {
		return "failure"
	}
	return "success"
}

func commitStatusDescription(status jobs.Status) string {
	if status == jobs.StatusFailed {
		return "Code review found blocking issues."
	}
	return "Code review bot completed review."
}

func reviewFindings(jobID int64, findings []review.Finding) []jobs.ReviewFinding {
	result := make([]jobs.ReviewFinding, 0, len(findings))
	for _, finding := range findings {
		result = append(result, jobs.ReviewFinding{
			JobID:      jobID,
			Path:       finding.Path,
			Line:       finding.Line,
			Severity:   finding.Severity,
			Category:   finding.Category,
			Title:      finding.Title,
			Body:       finding.Body,
			Confidence: finding.Confidence,
		})
	}
	return result
}

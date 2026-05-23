package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"code-review-bot/internal/gitea"
	"code-review-bot/internal/jobs"
	"code-review-bot/internal/review"
	"code-review-bot/internal/settings"
)

type SettingsProvider interface {
	Load(ctx context.Context) (settings.AppSettings, error)
}

type Options struct {
	PollInterval time.Duration
}

type Worker struct {
	store            jobs.Store
	settingsProvider SettingsProvider
	options          Options
	logger           *slog.Logger
	workerID         string
}

func New(store jobs.Store, settingsProvider SettingsProvider, options Options, logger *slog.Logger) *Worker {
	return &Worker{
		store:            store,
		settingsProvider: settingsProvider,
		options:          options,
		logger:           logger,
		workerID:         defaultWorkerID(),
	}
}

func (w *Worker) Run(ctx context.Context) {
	defaultPollInterval := w.options.PollInterval
	if defaultPollInterval <= 0 {
		defaultPollInterval = 5 * time.Second
	}

	for {
		w.processOne(ctx)

		pollInterval := defaultPollInterval
		if appSettings, err := w.loadSettings(ctx); err != nil {
			w.logger.Error("failed to load settings for worker poll interval", "error", err)
		} else {
			pollInterval = appSettings.WorkerPollInterval
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) processOne(ctx context.Context) {
	appSettings, err := w.loadSettings(ctx)
	if err != nil {
		w.logger.Error("failed to load settings", "error", err)
		return
	}
	if recovered, err := w.store.RecoverStale(ctx, appSettings.ReviewStaleTimeout, appSettings.ReviewMaxAttempts); err != nil {
		w.logger.Error("failed to recover stale jobs", "error", err)
	} else if recovered > 0 {
		w.logger.Warn("recovered stale review jobs", "count", recovered)
	}

	if appSettings.GiteaBaseURL == "" || appSettings.GiteaToken == "" {
		return
	}

	giteaClient, err := gitea.NewClient(appSettings.GiteaBaseURL, appSettings.GiteaToken)
	if err != nil {
		w.logger.Error("failed to create gitea client", "error", err)
		return
	}
	w.syncPendingStatuses(ctx, giteaClient)

	job, ok, err := w.store.ClaimQueued(ctx, w.workerID)
	if err != nil {
		w.logger.Error("failed to claim review job", "error", err)
		return
	}
	if !ok {
		return
	}
	stopHeartbeat := w.startHeartbeat(ctx, job.ID, appSettings.ReviewStaleTimeout)
	defer stopHeartbeat()
	reviewer := reviewerFromSettings(appSettings, w.logger)

	w.logger.Info("processing review job", "job_id", job.ID, "repo", job.RepoFullName, "pr", job.PRNumber, "head_sha", job.HeadSHA, "worker_id", w.workerID)

	if err := giteaClient.CreateCommitStatus(ctx, job.Owner, job.Repo, job.HeadSHA, gitea.CommitStatus{
		State:       "pending",
		Description: "Code review bot is reviewing this PR.",
		Context:     "code-review-bot/review",
	}); err != nil {
		w.fail(ctx, giteaClient, job, jobs.StatusErrored, fmt.Errorf("create pending status: %w", err))
		return
	}

	changedFiles, err := giteaClient.ListPullRequestFiles(ctx, job.Owner, job.Repo, job.PRNumber)
	if err != nil {
		w.fail(ctx, giteaClient, job, jobs.StatusErrored, fmt.Errorf("list pull request files: %w", err))
		return
	}

	diff, truncated, err := giteaClient.GetPullRequestDiff(ctx, job.Owner, job.Repo, job.PRNumber, appSettings.ReviewMaxDiffBytes)
	if err != nil {
		w.fail(ctx, giteaClient, job, jobs.StatusErrored, fmt.Errorf("get pull request diff: %w", err))
		return
	}

	changedFiles = filterChangedFiles(changedFiles, appSettings.ReviewExcludePaths)
	diff = filterUnifiedDiff(diff, appSettings.ReviewExcludePaths)

	result, err := reviewer.Review(ctx, review.Input{Job: job, ChangedFiles: changedFiles, Diff: diff, DiffTruncated: truncated})
	if err != nil {
		w.fail(ctx, giteaClient, job, jobs.StatusErrored, fmt.Errorf("review failed: %w", err))
		return
	}
	if !w.ensureLease(ctx, job.ID) {
		return
	}

	result.Findings = limitFindings(result.Findings, appSettings.ReviewMaxFindings)
	findings := reviewFindings(job.ID, result.Findings)
	if err := w.store.SaveFindings(ctx, job.ID, w.workerID, findings); err != nil {
		w.fail(ctx, giteaClient, job, jobs.StatusErrored, fmt.Errorf("save review findings: %w", err))
		return
	}
	if appSettings.ReviewPostInlineComments {
		if !w.ensureLease(ctx, job.ID) {
			return
		}
		if err := w.postInlineComments(ctx, giteaClient, job); err != nil {
			w.fail(ctx, giteaClient, job, jobs.StatusErrored, fmt.Errorf("post inline comments: %w", err))
			return
		}
	}
	if !w.ensureLease(ctx, job.ID) {
		return
	}

	comment, err := w.upsertSummaryComment(ctx, giteaClient, job, result)
	if err != nil {
		w.fail(ctx, giteaClient, job, jobs.StatusErrored, fmt.Errorf("save summary comment: %w", err))
		return
	}
	commentID := fmt.Sprintf("%d", comment.ID)
	if err := w.store.SaveSummaryComment(ctx, job.ID, w.workerID, commentID); err != nil {
		w.fail(ctx, giteaClient, job, jobs.StatusErrored, fmt.Errorf("save summary comment id: %w", err))
		return
	}

	finalStatus := reviewJobStatus(result, appSettings)
	if !w.ensureLease(ctx, job.ID) {
		return
	}
	if err := w.store.Complete(ctx, job.ID, w.workerID, finalStatus, result.Summary, commentID); err != nil {
		w.logger.Error("failed to mark review job complete", "job_id", job.ID, "worker_id", w.workerID, "error", err)
		return
	}
	w.syncPendingStatuses(ctx, giteaClient)
	w.logger.Info("completed review job", "job_id", job.ID, "status", finalStatus, "findings", len(findings))
}

func (w *Worker) syncPendingStatuses(ctx context.Context, giteaClient *gitea.Client) {
	jobsToSync, err := w.store.ClaimPendingStatusSync(ctx, w.workerID, 5)
	if err != nil {
		w.logger.Warn("failed to claim pending status sync jobs", "error", err)
		return
	}
	for _, job := range jobsToSync {
		if err := w.syncJobStatus(ctx, giteaClient, job); err != nil {
			w.logger.Warn("failed to sync terminal review status", "job_id", job.ID, "status", job.Status, "error", err)
			_ = w.store.MarkStatusSyncError(ctx, job.ID, w.workerID, err.Error())
			continue
		}
		if err := w.store.MarkStatusSynced(ctx, job.ID, w.workerID); err != nil {
			w.logger.Warn("failed to mark terminal review status synced", "job_id", job.ID, "error", err)
		}
	}
}

func (w *Worker) syncJobStatus(ctx context.Context, giteaClient *gitea.Client, job jobs.Job) error {
	return giteaClient.CreateCommitStatus(ctx, job.Owner, job.Repo, job.HeadSHA, gitea.CommitStatus{
		State:       commitStatusState(job.Status),
		Description: commitStatusDescription(job.Status),
		Context:     "code-review-bot/review",
	})
}

func (w *Worker) startHeartbeat(ctx context.Context, jobID int64, staleTimeout time.Duration) func() {
	interval := staleTimeout / 3
	if interval <= 0 || interval > time.Minute {
		interval = time.Minute
	}
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				heartbeatCallCtx, cancel := context.WithTimeout(heartbeatCtx, 10*time.Second)
				if err := w.store.Heartbeat(heartbeatCallCtx, jobID, w.workerID); err != nil {
					w.logger.Warn("failed to heartbeat review job", "job_id", jobID, "worker_id", w.workerID, "error", err)
				}
				cancel()
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

func (w *Worker) ensureLease(ctx context.Context, jobID int64) bool {
	if err := w.store.Heartbeat(ctx, jobID, w.workerID); err != nil {
		w.logger.Warn("review job lease lost", "job_id", jobID, "worker_id", w.workerID, "error", err)
		return false
	}
	return true
}

func (w *Worker) loadSettings(ctx context.Context) (settings.AppSettings, error) {
	if w.settingsProvider == nil {
		return settings.AppSettings{}.Normalize(), nil
	}
	appSettings, err := w.settingsProvider.Load(ctx)
	if err != nil {
		return settings.AppSettings{}, err
	}
	return appSettings.Normalize(), nil
}

func defaultWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "worker"
	}
	return fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), randomWorkerSuffix())
}

func randomWorkerSuffix() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(bytes[:])
}

func reviewerFromSettings(appSettings settings.AppSettings, logger *slog.Logger) review.Reviewer {
	if appSettings.OpenAIAPIKey == "" {
		logger.Warn("OPENAI_API_KEY is not configured; using mock reviewer")
		return review.MockReviewer{}
	}
	reviewer, err := review.NewOpenAIReviewer(appSettings.OpenAIBaseURL, appSettings.OpenAIAPIKey, appSettings.ReviewModel)
	if err != nil {
		logger.Error("openai reviewer initialization failed; using mock reviewer", "error", err)
		return review.MockReviewer{}
	}
	return reviewer
}

func (w *Worker) fail(ctx context.Context, giteaClient *gitea.Client, job jobs.Job, status jobs.Status, err error) {
	w.logger.Error("review job failed", "job_id", job.ID, "error", err)
	if leaseErr := w.store.Heartbeat(ctx, job.ID, w.workerID); errors.Is(leaseErr, jobs.ErrJobLeaseLost) {
		w.logger.Warn("review job lease lost", "job_id", job.ID, "worker_id", w.workerID, "error", leaseErr)
		return
	} else if leaseErr != nil {
		w.logger.Warn("failed to confirm review job lease before failing job", "job_id", job.ID, "worker_id", w.workerID, "error", leaseErr)
	}
	message := err.Error()
	if markErr := w.store.Fail(ctx, job.ID, w.workerID, status, message); markErr != nil {
		w.logger.Error("failed to mark review job failed", "job_id", job.ID, "worker_id", w.workerID, "error", markErr)
		return
	}
	w.syncPendingStatuses(ctx, giteaClient)
}

func (w *Worker) postInlineComments(ctx context.Context, giteaClient *gitea.Client, job jobs.Job) error {
	findings, err := w.store.ListFindings(ctx, job.ID)
	if err != nil {
		return err
	}
	for _, finding := range findings {
		if !finding.IsInline || finding.IsPosted || finding.Path == "" || finding.Line <= 0 {
			continue
		}
		if !w.ensureLease(ctx, job.ID) {
			return jobs.ErrJobLeaseLost
		}
		comment, err := giteaClient.CreatePullReviewComment(ctx, job.Owner, job.Repo, job.PRNumber, job.HeadSHA, gitea.InlineReviewComment{
			Path: finding.Path,
			Line: finding.Line,
			Body: inlineCommentBody(finding),
		})
		if err != nil {
			w.logger.Warn("failed to post inline comment", "job_id", job.ID, "finding_id", finding.ID, "error", err)
			if markErr := w.store.MarkFindingPostError(ctx, job.ID, w.workerID, finding.ID, err.Error()); markErr != nil {
				return fmt.Errorf("mark inline comment post error: %w", markErr)
			}
			continue
		}
		if err := w.store.MarkFindingPosted(ctx, job.ID, w.workerID, finding.ID, fmt.Sprintf("%d", comment.ID), comment.HTMLURL); err != nil {
			return fmt.Errorf("mark inline comment posted: %w", err)
		}
	}
	return nil
}

func (w *Worker) upsertSummaryComment(ctx context.Context, giteaClient *gitea.Client, job jobs.Job, result review.Result) (gitea.IssueComment, error) {
	body := formatSummary(job, result)
	if job.CommentID != "" {
		commentID, err := strconv.ParseInt(job.CommentID, 10, 64)
		if err == nil {
			comment, err := giteaClient.UpdateIssueComment(ctx, job.Owner, job.Repo, commentID, body)
			if err == nil {
				return comment, nil
			}
			w.logger.Warn("failed to update saved summary comment; searching for marker", "job_id", job.ID, "comment_id", job.CommentID, "error", err)
		}
	}

	comments, err := giteaClient.ListIssueComments(ctx, job.Owner, job.Repo, job.PRNumber)
	if err != nil {
		w.logger.Warn("failed to list issue comments before creating summary comment", "job_id", job.ID, "error", err)
		return giteaClient.CreateIssueComment(ctx, job.Owner, job.Repo, job.PRNumber, body)
	}
	marker := summaryMarker(job)
	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			return giteaClient.UpdateIssueComment(ctx, job.Owner, job.Repo, comment.ID, body)
		}
	}
	return giteaClient.CreateIssueComment(ctx, job.Owner, job.Repo, job.PRNumber, body)
}

func summaryMarker(job jobs.Job) string {
	return fmt.Sprintf("<!-- code-review-bot:job=%d head=%s -->", job.ID, job.HeadSHA)
}

func formatSummary(job jobs.Job, result review.Result) string {
	var builder strings.Builder
	builder.WriteString(summaryMarker(job))
	builder.WriteString("\n## Code Review Bot\n\n")
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

func reviewJobStatus(result review.Result, appSettings settings.AppSettings) jobs.Status {
	if result.Decision == "request_changes" || (appSettings.ReviewFailOnHigh && result.RiskLevel == "high") {
		return jobs.StatusFailed
	}
	return jobs.StatusSucceeded
}

func commitStatusState(status jobs.Status) string {
	switch status {
	case jobs.StatusFailed:
		return "failure"
	case jobs.StatusErrored:
		return "error"
	default:
		return "success"
	}
}

func commitStatusDescription(status jobs.Status) string {
	switch status {
	case jobs.StatusFailed:
		return "Code review found blocking issues."
	case jobs.StatusErrored:
		return "Code review bot failed to review this PR."
	default:
		return "Code review bot completed review."
	}
}

func inlineCommentBody(finding jobs.ReviewFinding) string {
	return fmt.Sprintf("**[%s/%s] %s**\n\n%s", finding.Severity, finding.Category, finding.Title, finding.Body)
}

func limitFindings(findings []review.Finding, maxFindings int) []review.Finding {
	if maxFindings <= 0 || len(findings) <= maxFindings {
		return findings
	}
	return findings[:maxFindings]
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
			IsInline:   finding.Path != "" && finding.Line > 0,
		})
	}
	return result
}

package review

import (
	"context"
	"fmt"
	"strings"

	"code-review-bot/internal/gitea"
	"code-review-bot/internal/jobs"
)

type Input struct {
	Job           jobs.Job
	ChangedFiles  []gitea.ChangedFile
	Diff          string
	DiffTruncated bool
}

type Result struct {
	Summary   string    `json:"summary"`
	RiskLevel string    `json:"risk_level"`
	Decision  string    `json:"decision"`
	Findings  []Finding `json:"findings"`
}

type Finding struct {
	Path       string  `json:"path"`
	Line       int     `json:"line"`
	Severity   string  `json:"severity"`
	Category   string  `json:"category"`
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	Confidence float64 `json:"confidence"`
}

type Reviewer interface {
	Review(ctx context.Context, input Input) (Result, error)
}

type MockReviewer struct{}

func (r MockReviewer) Review(ctx context.Context, input Input) (Result, error) {
	_ = ctx
	return Result{
		Summary:   fmt.Sprintf("已收到 PR #%d 的代码审查任务。当前拉取到 %d 个变更文件，diff 长度 %d 字节。未配置 OPENAI_API_KEY，因此使用 mock reviewer。", input.Job.PRNumber, len(input.ChangedFiles), len(input.Diff)),
		RiskLevel: "low",
		Decision:  "comment",
		Findings:  []Finding{},
	}, nil
}

func buildPrompt(input Input) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Repository: %s\n", input.Job.RepoFullName))
	builder.WriteString(fmt.Sprintf("Pull Request: #%d\n", input.Job.PRNumber))
	builder.WriteString(fmt.Sprintf("Head SHA: %s\n", input.Job.HeadSHA))
	builder.WriteString(fmt.Sprintf("Base SHA: %s\n", input.Job.BaseSHA))
	if input.DiffTruncated {
		builder.WriteString("Diff truncated: true\n")
	}
	builder.WriteString("Review output: include concrete findings when there are correctness, security, data loss, concurrency, performance, or test gap issues. Use line=0 for file-level findings. Use decision=request_changes only for high-confidence blocking issues.\n")
	builder.WriteString("\nChanged files:\n")
	for _, file := range input.ChangedFiles {
		builder.WriteString(fmt.Sprintf("- %s status=%s additions=%d deletions=%d changes=%d\n", file.Filename, file.Status, file.Additions, file.Deletions, file.Changes))
	}
	builder.WriteString("\nUnified diff:\n")
	builder.WriteString(input.Diff)
	return builder.String()
}

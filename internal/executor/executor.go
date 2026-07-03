package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/JakeTRogers/depflow/internal/githubcli"
	"github.com/JakeTRogers/depflow/internal/planner"
)

// sleepFunc is a package-level variable so tests can replace the post-merge delay.
var sleepFunc = func(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer stopTimer(timer)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Operator combines all GitHub operations needed by the executor.
type Operator interface {
	ViewPullRequest(ctx context.Context, repo string, number int) (githubcli.PRDetail, error)
	ApprovePullRequest(ctx context.Context, repo string, number int) error
	MergePullRequest(ctx context.Context, repo string, number int, admin bool) error
	CommentOnPR(ctx context.Context, repo string, number int, body string) error
	ListWorkflowRuns(ctx context.Context, repo string, branch string) ([]githubcli.WorkflowRun, error)
	CompareBranches(ctx context.Context, repo string, base string, head string) (githubcli.BranchComparison, error)
}

// Progress provides status updates for a progress indicator.
type Progress interface {
	SetStatus(status string)
	Increment()
}

type nopProgress struct{}

func (nopProgress) SetStatus(string) {}
func (nopProgress) Increment()       {}

// Config controls executor admin override, polling, and timeout behavior.
type Config struct {
	Admin            bool
	PollInterval     time.Duration
	CheckTimeout     time.Duration
	PostMergeDelay   time.Duration
	PostMergeTimeout time.Duration
	ShowChecks       bool
	ShowTiming       bool
}

type prStatus string

const (
	statusMerged  prStatus = "merged"
	statusSkipped prStatus = "skipped"
	statusFailed  prStatus = "failed"
)

// PRResult tracks the outcome for a single PR.
type PRResult struct {
	Item     planner.PlannedPR
	Status   prStatus
	Error    error
	Duration time.Duration
}

// Result is the overall execution outcome.
type Result struct {
	Processed []PRResult
}

// Merged returns the successfully merged PRs.
func (r *Result) Merged() []PRResult {
	var merged []PRResult
	for _, pr := range r.Processed {
		if pr.Status == statusMerged {
			merged = append(merged, pr)
		}
	}
	return merged
}

// Failed returns the PR that caused a stop, if any.
func (r *Result) Failed() *PRResult {
	for _, pr := range r.Processed {
		if pr.Status == statusFailed {
			return &pr
		}
	}
	return nil
}

func executionFailure(number int, err error) error {
	return fmt.Errorf("execution failed for PR #%d: %w", number, errors.Join(ErrExecutionFailed, err))
}

// Run processes each PR in plan order: update -> wait CI -> merge -> wait post-merge CI.
// It stops on the first failure and returns all results up to that point.
func Run(ctx context.Context, op Operator, plan planner.Plan, repo string, cfg Config, log *slog.Logger, progress Progress) (*Result, error) {
	if progress == nil {
		progress = nopProgress{}
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	result := &Result{}
	total := len(plan.Items)
	var itemStart time.Time

	record := func(item planner.PlannedPR, status prStatus, err error, statusText string) {
		progress.SetStatus(statusText)
		result.Processed = append(result.Processed, PRResult{
			Item:     item,
			Status:   status,
			Error:    err,
			Duration: time.Since(itemStart),
		})
		progress.Increment()
	}

	updateLastDuration := func(item planner.PlannedPR) {
		if len(result.Processed) == 0 {
			return
		}
		last := &result.Processed[len(result.Processed)-1]
		if last.Item.PR.Number == item.PR.Number {
			last.Duration = time.Since(itemStart)
		}
	}

	markLastFailed := func(item planner.PlannedPR, err error, statusText string) {
		progress.SetStatus(statusText)
		if len(result.Processed) > 0 {
			last := &result.Processed[len(result.Processed)-1]
			if last.Item.PR.Number == item.PR.Number {
				last.Status = statusFailed
				last.Error = err
				last.Duration = time.Since(itemStart)
				return
			}
		}

		result.Processed = append(result.Processed, PRResult{
			Item:     item,
			Status:   statusFailed,
			Error:    err,
			Duration: time.Since(itemStart),
		})
		progress.Increment()
	}

	for i, item := range plan.Items {
		itemStart = time.Now()
		log.Info("processing PR", "step", i+1, "total", total, "number", item.PR.Number, "title", item.PR.Title)

		progress.SetStatus(fmt.Sprintf("Inspecting PR #%d", item.PR.Number))
		detail, err := op.ViewPullRequest(ctx, repo, item.PR.Number)
		if err != nil {
			record(item, statusFailed, err, fmt.Sprintf("Failed PR #%d", item.PR.Number))
			return result, fmt.Errorf("viewing PR #%d: %w", item.PR.Number, err)
		}

		if detail.State != "OPEN" {
			log.Info("PR not open, skipping", "number", item.PR.Number, "state", detail.State)
			record(item, statusSkipped, nil, fmt.Sprintf("Skipped PR #%d", item.PR.Number))
			continue
		}

		progress.SetStatus(fmt.Sprintf("Checking branch state for PR #%d", item.PR.Number))
		comparison, err := op.CompareBranches(ctx, repo, detail.BaseRefName, detail.HeadRefName)
		if err != nil {
			record(item, statusFailed, err, fmt.Sprintf("Failed PR #%d", item.PR.Number))
			return result, fmt.Errorf("comparing branches for PR #%d: %w", item.PR.Number, err)
		}

		if comparison.BehindBy > 0 {
			log.Info("branch behind base, requesting rebase", "number", item.PR.Number, "behind_by", comparison.BehindBy)
			progress.SetStatus(fmt.Sprintf("Requesting rebase for PR #%d", item.PR.Number))
			if err := op.CommentOnPR(ctx, repo, item.PR.Number, "@dependabot rebase"); err != nil {
				record(item, statusFailed, err, fmt.Sprintf("Failed PR #%d", item.PR.Number))
				return result, fmt.Errorf("requesting rebase for PR #%d: %w", item.PR.Number, err)
			}
			if err := waitForBranchUpdate(ctx, op, repo, detail.BaseRefName, detail.HeadRefName, item.PR.Number, cfg, log, progress); err != nil {
				record(item, statusFailed, err, fmt.Sprintf("Failed PR #%d", item.PR.Number))
				return result, fmt.Errorf("waiting for rebase on PR #%d: %w", item.PR.Number, err)
			}
		}

		cr, err := waitForChecks(ctx, op, repo, item.PR.Number, cfg, log, progress)
		if err != nil {
			record(item, statusFailed, err, fmt.Sprintf("Failed PR #%d", item.PR.Number))
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return result, err
			}
			return result, executionFailure(item.PR.Number, err)
		}
		if len(cr.Failed) > 0 {
			log.Warn("admin override: continuing despite failed CI checks", "pr", item.PR.Number, "failed_checks", len(cr.Failed))
			for _, f := range cr.Failed {
				log.Warn("admin override: bypassed failed check", "pr", item.PR.Number, "check", f.Name, "conclusion", f.Conclusion, "state", f.State)
			}
		}

		progress.SetStatus(fmt.Sprintf("Re-checking PR #%d before merge", item.PR.Number))
		detail, err = op.ViewPullRequest(ctx, repo, item.PR.Number)
		if err != nil {
			record(item, statusFailed, err, fmt.Sprintf("Failed PR #%d", item.PR.Number))
			return result, fmt.Errorf("re-checking PR #%d before merge: %w", item.PR.Number, err)
		}

		if detail.Mergeable == "CONFLICTING" {
			mergeErr := fmt.Errorf("PR #%d: %w", item.PR.Number, ErrMergeConflict)
			record(item, statusFailed, mergeErr, fmt.Sprintf("Failed PR #%d: merge conflict", item.PR.Number))
			return result, executionFailure(item.PR.Number, mergeErr)
		}

		comparison, err = op.CompareBranches(ctx, repo, detail.BaseRefName, detail.HeadRefName)
		if err != nil {
			record(item, statusFailed, err, fmt.Sprintf("Failed PR #%d", item.PR.Number))
			return result, fmt.Errorf("final branch comparison for PR #%d: %w", item.PR.Number, err)
		}
		if comparison.BehindBy > 0 {
			mergeErr := fmt.Errorf("PR #%d: branch still %d commit(s) behind base after update", item.PR.Number, comparison.BehindBy)
			record(item, statusFailed, mergeErr, fmt.Sprintf("Failed PR #%d", item.PR.Number))
			return result, executionFailure(item.PR.Number, mergeErr)
		}

		progress.SetStatus(fmt.Sprintf("Approving PR #%d", item.PR.Number))
		if err := op.ApprovePullRequest(ctx, repo, item.PR.Number); err != nil {
			record(item, statusFailed, err, fmt.Sprintf("Failed PR #%d", item.PR.Number))
			return result, fmt.Errorf("approving PR #%d: %w", item.PR.Number, err)
		}

		progress.SetStatus(fmt.Sprintf("Merging PR #%d", item.PR.Number))
		if err := op.MergePullRequest(ctx, repo, item.PR.Number, cfg.Admin); err != nil {
			record(item, statusFailed, err, fmt.Sprintf("Failed PR #%d", item.PR.Number))
			return result, fmt.Errorf("merging PR #%d: %w", item.PR.Number, err)
		}

		log.Info("PR merged", "number", item.PR.Number)
		record(item, statusMerged, nil, fmt.Sprintf("Merged PR #%d", item.PR.Number))

		if i < total-1 {
			if err := sleepFunc(ctx, cfg.PostMergeDelay); err != nil {
				return result, err
			}
			progress.SetStatus(fmt.Sprintf("Confirming merge for PR #%d", item.PR.Number))
			mergedDetail, err := op.ViewPullRequest(ctx, repo, item.PR.Number)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return result, err
				}
				failedErr := fmt.Errorf("confirming merged PR #%d: %w", item.PR.Number, err)
				markLastFailed(item, failedErr, fmt.Sprintf("Failed PR #%d", item.PR.Number))
				return result, executionFailure(item.PR.Number, failedErr)
			}
			mergeSHA := mergedDetail.MergeCommit.OID
			if mergeSHA == "" {
				failedErr := fmt.Errorf("confirming merged PR #%d: merge commit SHA is empty", item.PR.Number)
				markLastFailed(item, failedErr, fmt.Sprintf("Failed PR #%d", item.PR.Number))
				return result, executionFailure(item.PR.Number, failedErr)
			}
			if err := waitForPostMergeCI(ctx, op, repo, detail.BaseRefName, mergeSHA, cfg, log, progress); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return result, err
				}
				failedErr := fmt.Errorf("post-merge CI failed after PR #%d: %w", item.PR.Number, err)
				markLastFailed(item, failedErr, fmt.Sprintf("Failed PR #%d", item.PR.Number))
				return result, executionFailure(item.PR.Number, failedErr)
			}
			updateLastDuration(item)
		} else {
			log.Info("last PR in plan, skipping post-merge CI wait", "number", item.PR.Number)
			progress.SetStatus(fmt.Sprintf("Merged PR #%d (last PR, skipping post-merge CI wait)", item.PR.Number))
		}
	}

	return result, nil
}

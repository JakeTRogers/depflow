package executor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type checkFailure struct {
	Name       string
	State      string
	Conclusion string
}

type checkResult struct {
	Failed []checkFailure
}

func isTerminalFailureConclusion(conclusion string) bool {
	switch conclusion {
	case "failure", "cancelled", "timed_out", "action_required", "startup_failure", "stale":
		return true
	default:
		return false
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func resetTimer(timer *time.Timer, delay time.Duration) {
	stopTimer(timer)
	timer.Reset(delay)
}

// waitForChecks polls ViewPullRequest until all status checks pass, a non-admin failure is observed,
// admin-mode checks settle with failures, or the wait times out.
func waitForChecks(ctx context.Context, op Operator, repo string, number int, cfg Config, log *slog.Logger, progress Progress) (checkResult, error) {
	progress.SetStatus(fmt.Sprintf("Waiting for CI on PR #%d", number))

	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, cfg.CheckTimeout)
	defer cancel()

	timer := time.NewTimer(0)
	stopTimer(timer)
	defer stopTimer(timer)

	for {
		detail, err := op.ViewPullRequest(ctx, repo, number)
		if err != nil {
			if parentErr := parentCtx.Err(); parentErr != nil {
				return checkResult{}, parentErr
			}
			if ctx.Err() != nil {
				return checkResult{}, fmt.Errorf("PR #%d: %w", number, ErrCheckTimeout)
			}
			return checkResult{}, fmt.Errorf("polling checks for PR #%d: %w", number, err)
		}

		checks := detail.StatusCheckRollup
		if len(checks) == 0 {
			log.Info("no CI checks configured, proceeding", "number", number)
			return checkResult{}, nil
		}

		allTerminal := true
		result := checkResult{}
		hasFailed := false
		for _, c := range checks {
			conclusion := strings.ToLower(c.Conclusion)
			state := strings.ToLower(c.State)

			if isTerminalFailureConclusion(conclusion) || state == "failure" || state == "error" {
				name := c.Name
				if name == "" {
					name = c.Context
				}

				failure := checkFailure{
					Name:       name,
					State:      state,
					Conclusion: conclusion,
				}
				if !cfg.Admin {
					result.Failed = []checkFailure{failure}
					return result, fmt.Errorf("check %q failed for PR #%d: %w", failure.Name, number, ErrCheckFailed)
				}

				result.Failed = append(result.Failed, failure)
				hasFailed = true
				continue
			}

			if conclusion != "success" && conclusion != "neutral" && conclusion != "skipped" && state != "success" {
				allTerminal = false
			}
		}

		if allTerminal {
			if hasFailed {
				return result, nil
			}

			log.Info("all checks passed", "number", number)
			return checkResult{}, nil
		}

		log.Debug("checks still pending, waiting", "number", number, "interval", cfg.PollInterval)
		resetTimer(timer, cfg.PollInterval)

		select {
		case <-parentCtx.Done():
			return checkResult{}, parentCtx.Err()
		case <-ctx.Done():
			return checkResult{}, fmt.Errorf("PR #%d: %w", number, ErrCheckTimeout)
		case <-timer.C:
		}
	}
}

// waitForPostMergeCI polls ListWorkflowRuns on the base branch until all runs for the merge commit complete.
func waitForPostMergeCI(ctx context.Context, op Operator, repo string, branch string, mergeSHA string, cfg Config, log *slog.Logger, progress Progress) error {
	progress.SetStatus(fmt.Sprintf("Waiting for post-merge CI on %s", branch))

	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, cfg.PostMergeTimeout)
	defer cancel()

	timer := time.NewTimer(0)
	stopTimer(timer)
	defer stopTimer(timer)

	for {
		runs, err := op.ListWorkflowRuns(ctx, repo, branch)
		if err != nil {
			if parentErr := parentCtx.Err(); parentErr != nil {
				return parentErr
			}
			if ctx.Err() != nil {
				return fmt.Errorf("post-merge CI timeout for branch %s merge %s: %w", branch, mergeSHA, ErrPostMergeTimeout)
			}
			return fmt.Errorf("listing workflow runs for branch %s: %w", branch, err)
		}

		var relevant []struct {
			name       string
			status     string
			conclusion string
		}
		for _, r := range runs {
			if r.HeadSHA == mergeSHA {
				relevant = append(relevant, struct {
					name       string
					status     string
					conclusion string
				}{r.Name, strings.ToLower(r.Status), strings.ToLower(r.Conclusion)})
			}
		}

		if len(relevant) == 0 {
			log.Debug("no post-merge runs detected yet, waiting", "branch", branch)
			resetTimer(timer, cfg.PollInterval)
			select {
			case <-parentCtx.Done():
				return parentCtx.Err()
			case <-ctx.Done():
				return fmt.Errorf("post-merge CI timeout for branch %s merge %s: %w", branch, mergeSHA, ErrPostMergeTimeout)
			case <-timer.C:
				continue
			}
		}

		for _, r := range relevant {
			if r.status == "completed" && isTerminalFailureConclusion(r.conclusion) {
				return fmt.Errorf("post-merge run %q failed on branch %s", r.name, branch)
			}
		}

		allCompleted := true
		for _, r := range relevant {
			if r.status != "completed" {
				allCompleted = false
				break
			}
		}

		if allCompleted {
			log.Info("post-merge CI passed", "branch", branch)
			return nil
		}

		log.Debug("post-merge CI still running, waiting", "branch", branch)
		resetTimer(timer, cfg.PollInterval)

		select {
		case <-parentCtx.Done():
			return parentCtx.Err()
		case <-ctx.Done():
			return fmt.Errorf("post-merge CI timeout for branch %s merge %s: %w", branch, mergeSHA, ErrPostMergeTimeout)
		case <-timer.C:
		}
	}
}

// waitForBranchUpdate polls CompareBranches until the PR branch is no longer behind its base.
func waitForBranchUpdate(ctx context.Context, op Operator, repo string, base string, head string, number int, cfg Config, log *slog.Logger, progress Progress) error {
	progress.SetStatus(fmt.Sprintf("Waiting for branch update on PR #%d", number))

	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, cfg.CheckTimeout)
	defer cancel()

	timer := time.NewTimer(0)
	stopTimer(timer)
	defer stopTimer(timer)

	for {
		comparison, err := op.CompareBranches(ctx, repo, base, head)
		if err != nil {
			if parentErr := parentCtx.Err(); parentErr != nil {
				return parentErr
			}
			if ctx.Err() != nil {
				return fmt.Errorf("PR #%d: timed out waiting for branch update: %w", number, ErrBranchUpdateTimeout)
			}
			return fmt.Errorf("polling branch status for PR #%d: %w", number, err)
		}

		if comparison.BehindBy == 0 {
			log.Info("branch up to date", "number", number)
			return nil
		}

		log.Debug("branch still behind, waiting for update", "number", number, "behind_by", comparison.BehindBy)
		resetTimer(timer, cfg.PollInterval)

		select {
		case <-parentCtx.Done():
			return parentCtx.Err()
		case <-ctx.Done():
			return fmt.Errorf("PR #%d: timed out waiting for branch update: %w", number, ErrBranchUpdateTimeout)
		case <-timer.C:
		}
	}
}

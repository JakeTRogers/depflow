package executor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/JakeTRogers/depflow/internal/githubcli"
	"github.com/JakeTRogers/depflow/internal/terminal"
)

type checkFailure struct {
	Name       string
	State      string
	Conclusion string
}

type checkResult struct {
	Failed []checkFailure
	// HeadSHA is the head commit whose checks were verified.
	HeadSHA string
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

// waitStatus builds a progress status line, optionally appending elapsed time and a detail suffix.
func waitStatus(base string, cfg Config, start time.Time, detail string) string {
	status := base
	if cfg.ShowTiming {
		status = fmt.Sprintf("%s [%s elapsed]", status, time.Since(start).Round(time.Second))
	}
	if cfg.ShowChecks && detail != "" {
		status = fmt.Sprintf("%s — %s", status, detail)
	}
	return status
}

// summarizeChecks renders a compact pending/failed breakdown of a PR's status checks.
func summarizeChecks(checks []githubcli.StatusCheck) string {
	var pending, failed []string
	passed := 0
	for _, c := range checks {
		name := c.Name
		if name == "" {
			name = c.Context
		}
		name = terminal.Sanitize(name)
		conclusion := strings.ToLower(c.Conclusion)
		state := strings.ToLower(c.State)

		switch {
		case isTerminalFailureConclusion(conclusion) || state == "failure" || state == "error":
			failed = append(failed, name)
		case conclusion == "success" || conclusion == "neutral" || conclusion == "skipped" || state == "success":
			passed++
		default:
			pending = append(pending, name)
		}
	}

	summary := fmt.Sprintf("%d/%d checks complete", passed+len(failed), len(checks))
	if len(pending) > 0 {
		summary += fmt.Sprintf(", pending: %s", strings.Join(pending, ", "))
	}
	if len(failed) > 0 {
		summary += fmt.Sprintf(", failed: %s", strings.Join(failed, ", "))
	}
	return summary
}

// waitForChecks polls ViewPullRequest until all status checks pass, a non-admin failure is observed,
// admin-mode checks settle with failures, or the wait times out.
//
// GitHub registers checks for a new head commit asynchronously, so an empty check list is only
// treated as "no CI configured" once cfg.CheckGrace has elapsed since observing that head.
// After a branch update, including a head change observed while polling, passing checks are not
// trusted before the grace period either, so checks that register late are not skipped.
func waitForChecks(ctx context.Context, op Operator, repo string, number int, freshCommit bool, cfg Config, log *slog.Logger, progress Progress) (checkResult, error) {
	start := time.Now()
	progress.SetStatus(waitStatus(fmt.Sprintf("Waiting for CI on PR #%d", number), cfg, start, ""))

	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, cfg.CheckTimeout)
	defer cancel()

	timer := time.NewTimer(0)
	stopTimer(timer)
	defer stopTimer(timer)

	var headSHA string
	var graceStart time.Time
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

		if graceStart.IsZero() || detail.HeadRefOid != headSHA {
			if !graceStart.IsZero() {
				freshCommit = true
			}
			headSHA = detail.HeadRefOid
			graceStart = time.Now()
		}
		graceElapsed := time.Since(graceStart) >= cfg.CheckGrace
		checks := detail.StatusCheckRollup
		summary := "no checks reported yet"
		if len(checks) == 0 {
			if graceElapsed {
				log.Info("no CI checks reported, proceeding", "number", number)
				return checkResult{HeadSHA: detail.HeadRefOid}, nil
			}
		} else {
			allTerminal := true
			result := checkResult{HeadSHA: detail.HeadRefOid}
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
					continue
				}

				if conclusion != "success" && conclusion != "neutral" && conclusion != "skipped" && state != "success" {
					allTerminal = false
				}
			}

			if allTerminal && (graceElapsed || !freshCommit) {
				if len(result.Failed) == 0 {
					log.Info("all checks passed", "number", number)
				}
				return result, nil
			}
			summary = summarizeChecks(checks)
		}

		progress.SetStatus(waitStatus(fmt.Sprintf("Waiting for CI on PR #%d", number), cfg, start, summary))
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
	start := time.Now()
	progress.SetStatus(waitStatus(fmt.Sprintf("Waiting for post-merge CI on %s", branch), cfg, start, ""))

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
				}{terminal.Sanitize(r.Name), strings.ToLower(r.Status), strings.ToLower(r.Conclusion)})
			}
		}

		if len(relevant) == 0 {
			progress.SetStatus(waitStatus(fmt.Sprintf("Waiting for post-merge CI on %s", branch), cfg, start, "no workflow runs detected yet"))
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

		var pending []string
		for _, r := range relevant {
			if r.status != "completed" {
				pending = append(pending, fmt.Sprintf("%s (%s)", r.name, r.status))
			}
		}
		detail := fmt.Sprintf("%d/%d runs complete", len(relevant)-len(pending), len(relevant))
		if len(pending) > 0 {
			detail += ", pending: " + strings.Join(pending, ", ")
		}
		progress.SetStatus(waitStatus(fmt.Sprintf("Waiting for post-merge CI on %s", branch), cfg, start, detail))

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
	start := time.Now()
	progress.SetStatus(waitStatus(fmt.Sprintf("Waiting for branch update on PR #%d", number), cfg, start, ""))

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

		progress.SetStatus(waitStatus(fmt.Sprintf("Waiting for branch update on PR #%d", number), cfg, start, fmt.Sprintf("behind by %d commit(s)", comparison.BehindBy)))
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

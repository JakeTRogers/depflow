package executor

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JakeTRogers/depflow/internal/githubcli"
)

func TestWaitForChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		op         *fakeOperator
		wantErr    bool
		wantErrIs  error
		wantFailed []checkFailure
	}{
		{
			name: "checks pass immediately",
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{
						{Name: "ci", Conclusion: "success"},
						{Name: "lint", Conclusion: "neutral"},
					}}},
				},
				viewErrors:    map[int]error{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				runResults:    map[string][][]githubcli.WorkflowRun{},
			},
		},
		{
			name: "check fails immediately",
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{
						{Name: "ci", Conclusion: "failure"},
					}}},
				},
				viewErrors:    map[int]error{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				runResults:    map[string][][]githubcli.WorkflowRun{},
			},
			wantErr:    true,
			wantErrIs:  ErrCheckFailed,
			wantFailed: []checkFailure{{Name: "ci", Conclusion: "failure"}},
		},
		{
			name: "no checks configured",
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{}}},
				},
				viewErrors:    map[int]error{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				runResults:    map[string][][]githubcli.WorkflowRun{},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

			result, err := waitForChecks(ctx, tc.op, "owner/repo", 1, false, testConfig(), log, nopProgress{})

			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
				t.Errorf("error: got %v, want %v", err, tc.wantErrIs)
			}
			if !reflect.DeepEqual(result.Failed, tc.wantFailed) {
				t.Errorf("failed checks: got %#v, want %#v", result.Failed, tc.wantFailed)
			}
		})
	}
}

func TestWaitForChecksTerminalFailureConclusionsReturnErrCheckFailed(t *testing.T) {
	t.Parallel()

	for _, conclusion := range []string{"failure", "cancelled", "timed_out", "action_required", "startup_failure", "stale"} {
		conclusion := conclusion
		t.Run(conclusion, func(t *testing.T) {
			t.Parallel()

			op := &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{
						Name:       "ci",
						Conclusion: conclusion,
					}}}},
				},
				viewErrors:    map[int]error{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				runResults:    map[string][][]githubcli.WorkflowRun{},
			}

			log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
			result, err := waitForChecks(context.Background(), op, "owner/repo", 1, false, testConfig(), log, nopProgress{})
			if !errors.Is(err, ErrCheckFailed) {
				t.Fatalf("error for %q: got %v, want %v", conclusion, err, ErrCheckFailed)
			}
			want := checkFailure{Name: "ci", Conclusion: conclusion}
			if !reflect.DeepEqual(result.Failed, []checkFailure{want}) {
				t.Fatalf("result for %q: got %#v, want %#v", conclusion, result.Failed, []checkFailure{want})
			}
		})
	}
}

func TestWaitForChecksShowChecksReportsPendingDetail(t *testing.T) {
	t.Parallel()

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{
			1: {
				{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{
					{Name: "build", Conclusion: "success"},
					{Name: "lint", State: "pending"},
				}},
				{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{
					{Name: "build", Conclusion: "success"},
					{Name: "lint", Conclusion: "success"},
				}},
			},
		},
		viewErrors:    map[int]error{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		runResults:    map[string][][]githubcli.WorkflowRun{},
	}

	cfg := testConfig()
	cfg.ShowChecks = true
	cfg.ShowTiming = true
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	spy := &spyProgress{}

	if _, err := waitForChecks(context.Background(), op, "owner/repo", 1, false, cfg, log, spy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spy.mu.Lock()
	defer spy.mu.Unlock()

	found := false
	for _, s := range spy.statuses {
		if strings.Contains(s, "pending: lint") && strings.Contains(s, "elapsed]") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("statuses = %v, want one with pending check detail and elapsed timing", spy.statuses)
	}
}

func TestWaitForChecksShowChecksSanitizesCheckNames(t *testing.T) {
	t.Parallel()

	maliciousName := "lint\x1b[31m\x07"

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{
			1: {
				{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{
					{Name: maliciousName, State: "pending"},
				}},
				{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{
					{Name: maliciousName, Conclusion: "success"},
				}},
			},
		},
		viewErrors:    map[int]error{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		runResults:    map[string][][]githubcli.WorkflowRun{},
	}

	cfg := testConfig()
	cfg.ShowChecks = true
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	spy := &spyProgress{}

	if _, err := waitForChecks(context.Background(), op, "owner/repo", 1, false, cfg, log, spy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spy.mu.Lock()
	defer spy.mu.Unlock()

	for _, s := range spy.statuses {
		if strings.ContainsAny(s, "\x00\x07\x1b\x7f") {
			t.Fatalf("status %q contains unsanitized terminal control bytes from a GitHub-controlled check name", s)
		}
	}
}

func TestWaitForChecksAdminCollectsFailuresAfterAllChecksSettle(t *testing.T) {
	t.Parallel()

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{
			1: {
				{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Context: "ci/context", Conclusion: "failure"}, {Name: "lint", State: "pending"}}},
				{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Context: "ci/context", Conclusion: "failure"}, {Name: "lint", Conclusion: "cancelled"}}},
			},
		},
		viewErrors:    map[int]error{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		runResults:    map[string][][]githubcli.WorkflowRun{},
	}

	cfg := testConfig()
	cfg.Admin = true
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	result, err := waitForChecks(context.Background(), op, "owner/repo", 1, false, cfg, log, nopProgress{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []checkFailure{
		{Name: "ci/context", Conclusion: "failure"},
		{Name: "lint", Conclusion: "cancelled"},
	}
	if !reflect.DeepEqual(result.Failed, want) {
		t.Fatalf("failed checks: got %#v, want %#v", result.Failed, want)
	}
	if remaining := len(op.viewResults[1]); remaining != 0 {
		t.Fatalf("remaining polls = %d, want 0 after waiting for checks to settle", remaining)
	}
}

func TestWaitForChecksNonAdminFailsFastOnFirstFailure(t *testing.T) {
	t.Parallel()

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{
			1: {{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "failure"}, {Name: "lint"}}}},
		},
		viewErrors:    map[int]error{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		runResults:    map[string][][]githubcli.WorkflowRun{},
	}

	cfg := testConfig()
	cfg.Admin = false
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	result, err := waitForChecks(context.Background(), op, "owner/repo", 1, false, cfg, log, nopProgress{})
	if !errors.Is(err, ErrCheckFailed) {
		t.Fatalf("error: got %v, want %v", err, ErrCheckFailed)
	}

	want := []checkFailure{{Name: "ci", Conclusion: "failure"}}
	if !reflect.DeepEqual(result.Failed, want) {
		t.Fatalf("failed checks: got %#v, want %#v", result.Failed, want)
	}
	if len(op.viewCalls) != 1 || op.viewCalls[0] != 1 {
		t.Fatalf("view calls: got %v, want [1]", op.viewCalls)
	}
}

func TestWaitForChecksTimeoutReturnsErrCheckTimeout(t *testing.T) {
	t.Parallel()

	pending := make([]githubcli.PRDetail, 32)
	for i := range pending {
		pending[i] = githubcli.PRDetail{
			Number: 1,
			State:  "OPEN",
			StatusCheckRollup: []githubcli.StatusCheck{{
				Name:  "ci",
				State: "pending",
			}},
		}
	}

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{
			1: pending,
		},
		viewErrors:    map[int]error{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		runResults:    map[string][][]githubcli.WorkflowRun{},
	}

	cfg := Config{PollInterval: time.Millisecond, CheckTimeout: 5 * time.Millisecond}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	_, err := waitForChecks(context.Background(), op, "owner/repo", 1, false, cfg, log, nopProgress{})
	if !errors.Is(err, ErrCheckTimeout) {
		t.Fatalf("error: got %v, want %v", err, ErrCheckTimeout)
	}
}

func TestWaitForChecksParentCancellationReturnsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{},
		viewErrors: map[int]error{
			1: context.Canceled,
		},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		runResults:    map[string][][]githubcli.WorkflowRun{},
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	_, err := waitForChecks(ctx, op, "owner/repo", 1, false, testConfig(), log, nopProgress{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: got %v, want %v", err, context.Canceled)
	}
	if errors.Is(err, ErrCheckTimeout) {
		t.Fatalf("error: got %v, should not match %v", err, ErrCheckTimeout)
	}
}

func TestWaitForPostMergeCI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		op        *fakeOperator
		wantErr   bool
		wantErrIs error
	}{
		{
			name: "post-merge CI passes",
			op: &fakeOperator{
				viewResults:   map[int][]githubcli.PRDetail{},
				viewErrors:    map[int]error{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				runResults: map[string][][]githubcli.WorkflowRun{
					"main": {{
						{Name: "CI", Status: "completed", Conclusion: "success", HeadSHA: "abc123", StartedAt: time.Now().Format(time.RFC3339)},
					}},
				},
			},
		},
		{
			name: "post-merge CI fails",
			op: &fakeOperator{
				viewResults:   map[int][]githubcli.PRDetail{},
				viewErrors:    map[int]error{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				runResults: map[string][][]githubcli.WorkflowRun{
					"main": {{
						{Name: "CI", Status: "completed", Conclusion: "failure", HeadSHA: "abc123", StartedAt: time.Now().Format(time.RFC3339)},
					}},
				},
			},
			wantErr: true,
		},
		{
			name: "unrelated runs are ignored until matching SHA completes",
			op: &fakeOperator{
				viewResults:   map[int][]githubcli.PRDetail{},
				viewErrors:    map[int]error{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				runResults: map[string][][]githubcli.WorkflowRun{
					"main": {
						{{Name: "CI", Status: "completed", Conclusion: "success", HeadSHA: "other-sha", StartedAt: time.Now().Format(time.RFC3339)}},
						{{Name: "CI", Status: "completed", Conclusion: "success", HeadSHA: "abc123", StartedAt: time.Now().Format(time.RFC3339)}},
					},
				},
			},
		},
		{
			name: "times out when matching SHA never appears",
			op: &fakeOperator{
				viewResults:   map[int][]githubcli.PRDetail{},
				viewErrors:    map[int]error{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				runResults: map[string][][]githubcli.WorkflowRun{
					"main": func() [][]githubcli.WorkflowRun {
						results := make([][]githubcli.WorkflowRun, 20)
						for i := range results {
							results[i] = []githubcli.WorkflowRun{{Name: "CI", Status: "completed", Conclusion: "success", HeadSHA: "other-sha", StartedAt: time.Now().Format(time.RFC3339)}}
						}
						return results
					}(),
				},
			},
			wantErr:   true,
			wantErrIs: ErrPostMergeTimeout,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

			cfg := testConfig()
			if tc.wantErrIs == ErrPostMergeTimeout {
				cfg = Config{PollInterval: time.Millisecond, PostMergeTimeout: 5 * time.Millisecond}
			}
			err := waitForPostMergeCI(ctx, tc.op, "owner/repo", "main", "abc123", cfg, log, nopProgress{})

			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
				t.Fatalf("error: got %v, want %v", err, tc.wantErrIs)
			}
		})
	}
}

func TestWaitForPostMergeCITerminalFailureConclusions(t *testing.T) {
	t.Parallel()

	for _, conclusion := range []string{"failure", "cancelled", "timed_out", "action_required", "startup_failure", "stale"} {
		conclusion := conclusion
		t.Run(conclusion, func(t *testing.T) {
			t.Parallel()

			op := &fakeOperator{
				viewResults:   map[int][]githubcli.PRDetail{},
				viewErrors:    map[int]error{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				runResults: map[string][][]githubcli.WorkflowRun{
					"main": {{
						{Name: "CI", Status: "completed", Conclusion: conclusion, HeadSHA: "abc123", StartedAt: time.Now().Format(time.RFC3339)},
					}},
				},
			}

			log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
			err := waitForPostMergeCI(context.Background(), op, "owner/repo", "main", "abc123", testConfig(), log, nopProgress{})
			if err == nil {
				t.Fatalf("expected error for conclusion %q", conclusion)
			}
		})
	}
}

func TestWaitForPostMergeCIReturnsEarlyOnCompletedFailure(t *testing.T) {
	t.Parallel()

	op := &fakeOperator{
		viewResults:   map[int][]githubcli.PRDetail{},
		viewErrors:    map[int]error{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		runResults: map[string][][]githubcli.WorkflowRun{
			"main": {
				{
					{Name: "failing", Status: "completed", Conclusion: "failure", HeadSHA: "abc123", StartedAt: time.Now().Format(time.RFC3339)},
					{Name: "still-running", Status: "in_progress", Conclusion: "", HeadSHA: "abc123", StartedAt: time.Now().Format(time.RFC3339)},
				},
			},
		},
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	err := waitForPostMergeCI(context.Background(), op, "owner/repo", "main", "abc123", testConfig(), log, nopProgress{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(op.runResults["main"]) != 0 {
		t.Fatalf("expected waitForPostMergeCI to stop after first failed poll, remaining polls = %d", len(op.runResults["main"]))
	}
}

func TestWaitForPostMergeCIShowChecksSanitizesRunNames(t *testing.T) {
	t.Parallel()

	maliciousName := "deploy\x1b[31m\x07"

	op := &fakeOperator{
		viewResults:   map[int][]githubcli.PRDetail{},
		viewErrors:    map[int]error{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		runResults: map[string][][]githubcli.WorkflowRun{
			"main": {
				{{Name: maliciousName, Status: "in_progress", HeadSHA: "abc123"}},
				{{Name: maliciousName, Status: "completed", Conclusion: "success", HeadSHA: "abc123"}},
			},
		},
	}

	cfg := testConfig()
	cfg.ShowChecks = true
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	spy := &spyProgress{}

	if err := waitForPostMergeCI(context.Background(), op, "owner/repo", "main", "abc123", cfg, log, spy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spy.mu.Lock()
	defer spy.mu.Unlock()

	for _, s := range spy.statuses {
		if strings.ContainsAny(s, "\x00\x07\x1b\x7f") {
			t.Fatalf("status %q contains unsanitized terminal control bytes from a GitHub-controlled workflow run name", s)
		}
	}
}

func TestWaitForBranchUpdate(t *testing.T) {
	t.Parallel()

	t.Run("already up to date", func(t *testing.T) {
		t.Parallel()
		op := &fakeOperator{
			viewResults:   map[int][]githubcli.PRDetail{},
			viewErrors:    map[int]error{},
			mergeErrors:   map[int]error{},
			commentErrors: map[int]error{},
			runResults:    map[string][][]githubcli.WorkflowRun{},
			compareResults: []githubcli.BranchComparison{
				{BehindBy: 0},
			},
		}
		cfg := Config{PollInterval: time.Millisecond, CheckTimeout: time.Second}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		err := waitForBranchUpdate(context.Background(), op, "owner/repo", "main", "feature", 1, cfg, log, nopProgress{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("behind then clean", func(t *testing.T) {
		t.Parallel()
		op := &fakeOperator{
			viewResults:   map[int][]githubcli.PRDetail{},
			viewErrors:    map[int]error{},
			mergeErrors:   map[int]error{},
			commentErrors: map[int]error{},
			runResults:    map[string][][]githubcli.WorkflowRun{},
			compareResults: []githubcli.BranchComparison{
				{BehindBy: 1},
				{BehindBy: 0},
			},
		}
		cfg := Config{PollInterval: time.Millisecond, CheckTimeout: time.Second}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		err := waitForBranchUpdate(context.Background(), op, "owner/repo", "main", "feature", 1, cfg, log, nopProgress{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("timeout while behind", func(t *testing.T) {
		t.Parallel()
		op := &fakeOperator{
			viewResults:   map[int][]githubcli.PRDetail{},
			viewErrors:    map[int]error{},
			mergeErrors:   map[int]error{},
			commentErrors: map[int]error{},
			runResults:    map[string][][]githubcli.WorkflowRun{},
			compareResults: func() []githubcli.BranchComparison {
				results := make([]githubcli.BranchComparison, 20)
				for i := range results {
					results[i] = githubcli.BranchComparison{BehindBy: 1}
				}
				return results
			}(),
		}
		cfg := Config{PollInterval: time.Millisecond, CheckTimeout: 5 * time.Millisecond}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		err := waitForBranchUpdate(context.Background(), op, "owner/repo", "main", "feature", 1, cfg, log, nopProgress{})
		if err == nil {
			t.Fatal("expected timeout error")
		}
		if !errors.Is(err, ErrBranchUpdateTimeout) {
			t.Fatalf("error: got %v, want %v", err, ErrBranchUpdateTimeout)
		}
	})
}

// repeatingViewOperator returns the same PR detail on every view.
type repeatingViewOperator struct {
	fakeOperator
	detail githubcli.PRDetail
	calls  int
}

func (r *repeatingViewOperator) ViewPullRequest(context.Context, string, int) (githubcli.PRDetail, error) {
	r.calls++
	return r.detail, nil
}

func TestWaitForChecksGracePeriod(t *testing.T) {
	t.Parallel()

	passing := githubcli.PRDetail{Number: 1, State: "OPEN", HeadRefOid: "sha-new", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}}
	failing := githubcli.PRDetail{Number: 1, State: "OPEN", HeadRefOid: "sha-new", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "failure"}}}
	empty := githubcli.PRDetail{Number: 1, State: "OPEN", HeadRefOid: "sha-new"}

	tests := []struct {
		name        string
		detail      githubcli.PRDetail
		freshCommit bool
		grace       time.Duration
		wantErrIs   error
		wantMinWait time.Duration
		wantCalls   int
	}{
		{name: "passing checks on existing commit are trusted immediately", detail: passing, grace: time.Hour, wantCalls: 1},
		{name: "passing checks on fresh commit wait for grace", detail: passing, freshCommit: true, grace: 20 * time.Millisecond, wantMinWait: 20 * time.Millisecond},
		{name: "no checks wait for grace", detail: empty, grace: 20 * time.Millisecond, wantMinWait: 20 * time.Millisecond},
		{name: "failed check on fresh commit stops immediately", detail: failing, freshCommit: true, grace: time.Hour, wantErrIs: ErrCheckFailed, wantCalls: 1},
		{name: "no checks within grace times out", detail: empty, grace: time.Hour, wantErrIs: ErrCheckTimeout},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			op := &repeatingViewOperator{detail: tc.detail}
			cfg := testConfig()
			cfg.CheckGrace = tc.grace
			log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

			start := time.Now()
			result, err := waitForChecks(context.Background(), op, "owner/repo", 1, tc.freshCommit, cfg, log, nopProgress{})
			elapsed := time.Since(start)

			if tc.wantErrIs != nil {
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("error = %v, want %v", err, tc.wantErrIs)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if result.HeadSHA != "sha-new" {
					t.Fatalf("HeadSHA = %q, want sha-new", result.HeadSHA)
				}
			}
			if elapsed < tc.wantMinWait {
				t.Fatalf("returned after %s, want at least %s", elapsed, tc.wantMinWait)
			}
			if tc.wantCalls > 0 && op.calls != tc.wantCalls {
				t.Fatalf("view calls = %d, want %d", op.calls, tc.wantCalls)
			}
		})
	}
}

type changingHeadOperator struct {
	fakeOperator
	details        []githubcli.PRDetail
	delay          time.Duration
	calls          int
	headObservedAt time.Time
}

func (o *changingHeadOperator) ViewPullRequest(ctx context.Context, _ string, _ int) (githubcli.PRDetail, error) {
	if o.calls > 0 && len(o.details) > 1 {
		timer := time.NewTimer(o.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return githubcli.PRDetail{}, ctx.Err()
		case <-timer.C:
		}
		o.details = o.details[1:]
		o.headObservedAt = time.Now()
	}
	o.calls++
	return o.details[0], nil
}

func TestWaitForChecksRestartsGraceOnHeadChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		checks      []githubcli.StatusCheck
		freshCommit bool
		admin       bool
		repeated    bool
	}{
		{name: "empty checks on changed head"},
		{name: "empty checks after rebase", freshCommit: true},
		{name: "passing checks on changed head", checks: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
		{name: "passing checks after rebase", checks: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}, freshCommit: true},
		{name: "admin failure on changed head", checks: []githubcli.StatusCheck{{Name: "ci", Conclusion: "failure"}}, admin: true},
		{name: "grace restarts on every head change", repeated: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig()
			cfg.CheckGrace = 20 * time.Millisecond
			cfg.CheckTimeout = time.Second
			cfg.Admin = tc.admin
			details := []githubcli.PRDetail{{HeadRefOid: "sha-old", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Status: "in_progress"}}}}
			if tc.repeated {
				details = append(details, githubcli.PRDetail{HeadRefOid: "sha-intermediate", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Status: "in_progress"}}})
			}
			details = append(details, githubcli.PRDetail{HeadRefOid: "sha-new", StatusCheckRollup: tc.checks})
			op := &changingHeadOperator{details: details, delay: cfg.CheckGrace + 10*time.Millisecond}

			result, err := waitForChecks(context.Background(), op, "owner/repo", 1, tc.freshCommit, cfg, testLogger(), nopProgress{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.HeadSHA != "sha-new" {
				t.Fatalf("HeadSHA = %q, want sha-new", result.HeadSHA)
			}
			if elapsed := time.Since(op.headObservedAt); elapsed < cfg.CheckGrace {
				t.Fatalf("trusted new head after %s, want at least %s", elapsed, cfg.CheckGrace)
			}
			if tc.admin && !reflect.DeepEqual(result.Failed, []checkFailure{{Name: "ci", Conclusion: "failure"}}) {
				t.Fatalf("failed checks = %#v, want admin failure", result.Failed)
			}
		})
	}
}

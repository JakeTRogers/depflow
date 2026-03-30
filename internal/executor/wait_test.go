package executor

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/JakeTRogers/depflow/internal/githubcli"
)

func TestWaitForChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		op        *fakeOperator
		wantErr   bool
		wantErrIs error
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
			wantErr:   true,
			wantErrIs: ErrCheckFailed,
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

			err := waitForChecks(ctx, tc.op, "owner/repo", 1, testConfig(), log, nopProgress{})

			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
				t.Errorf("error: got %v, want %v", err, tc.wantErrIs)
			}
		})
	}
}

func TestWaitForChecksTerminalFailureConclusions(t *testing.T) {
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
			err := waitForChecks(context.Background(), op, "owner/repo", 1, testConfig(), log, nopProgress{})
			if !errors.Is(err, ErrCheckFailed) {
				t.Fatalf("error for %q: got %v, want %v", conclusion, err, ErrCheckFailed)
			}
		})
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
	err := waitForChecks(context.Background(), op, "owner/repo", 1, cfg, log, nopProgress{})
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
	err := waitForChecks(ctx, op, "owner/repo", 1, testConfig(), log, nopProgress{})
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

package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/githubcli"
	"github.com/JakeTRogers/depflow/internal/planner"
)

func TestMain(m *testing.M) {
	sleepFunc = func(context.Context, time.Duration) error { return nil }
	os.Exit(m.Run())
}

type commentCall struct {
	Number int
	Body   string
}

type fakeOperator struct {
	mu             sync.Mutex
	viewResults    map[int][]githubcli.PRDetail
	viewErrors     map[int]error
	viewCalls      []int
	approveErrors  map[int]error
	mergeErrors    map[int]error
	commentErrors  map[int]error
	runResults     map[string][][]githubcli.WorkflowRun
	approveCalls   []int
	mergeCalls     []int
	mergeAdmin     []bool
	commentCalls   []commentCall
	callSequence   []string
	compareResults []githubcli.BranchComparison
	compareCalls   int
}

func (f *fakeOperator) ViewPullRequest(_ context.Context, _ string, number int) (githubcli.PRDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.viewCalls = append(f.viewCalls, number)

	if err, ok := f.viewErrors[number]; ok {
		return githubcli.PRDetail{}, err
	}

	results := f.viewResults[number]
	if len(results) == 0 {
		return githubcli.PRDetail{}, fmt.Errorf("no view results for PR #%d", number)
	}

	result := results[0]
	f.viewResults[number] = results[1:]
	return result, nil
}

func (f *fakeOperator) MergePullRequest(_ context.Context, _ string, number int, admin bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.callSequence = append(f.callSequence, fmt.Sprintf("merge:%d", number))
	f.mergeCalls = append(f.mergeCalls, number)
	f.mergeAdmin = append(f.mergeAdmin, admin)
	if err, ok := f.mergeErrors[number]; ok {
		return err
	}
	return nil
}

func (f *fakeOperator) ApprovePullRequest(_ context.Context, _ string, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.callSequence = append(f.callSequence, fmt.Sprintf("approve:%d", number))
	f.approveCalls = append(f.approveCalls, number)
	if err, ok := f.approveErrors[number]; ok {
		return err
	}
	return nil
}

func (f *fakeOperator) CommentOnPR(_ context.Context, _ string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.commentCalls = append(f.commentCalls, commentCall{Number: number, Body: body})
	if err, ok := f.commentErrors[number]; ok {
		return err
	}
	return nil
}

func (f *fakeOperator) ListWorkflowRuns(_ context.Context, _ string, branch string) ([]githubcli.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	results := f.runResults[branch]
	if len(results) == 0 {
		return nil, nil
	}
	result := results[0]
	f.runResults[branch] = results[1:]
	return result, nil
}

func (f *fakeOperator) CompareBranches(_ context.Context, _ string, _ string, _ string) (githubcli.BranchComparison, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.compareCalls >= len(f.compareResults) {
		return githubcli.BranchComparison{}, fmt.Errorf("no more compare results (call %d)", f.compareCalls)
	}
	result := f.compareResults[f.compareCalls]
	f.compareCalls++
	return result, nil
}

func newTestPlan(prs ...dependabot.PR) planner.Plan {
	items := make([]planner.PlannedPR, len(prs))
	for i, pr := range prs {
		items[i] = planner.PlannedPR{PR: pr, Bucket: planner.BucketPatch, Reason: "test"}
	}
	return planner.Plan{Items: items}
}

func testConfig() Config {
	return Config{
		PollInterval:     1 * time.Millisecond,
		CheckTimeout:     100 * time.Millisecond,
		PostMergeDelay:   1 * time.Millisecond,
		PostMergeTimeout: 100 * time.Millisecond,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		plan             planner.Plan
		op               *fakeOperator
		wantMerged       int
		wantSkipped      int
		wantFailed       bool
		wantErr          bool
		wantErrIs        []error
		wantFailedErrIs  []error
		wantApproveCalls []int
		wantMergeCalls   []int
		wantCommentCalls []commentCall
		wantCallSequence []string
	}{
		{
			name: "happy path merges all PRs",
			plan: newTestPlan(
				dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
				dependabot.PR{Number: 2, Title: "bump bar", BaseRef: "main"},
			),
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main",
							StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main"},
						{Number: 1, State: "MERGED", MergeCommit: githubcli.MergeCommit{OID: "merge-sha-1"}, BaseRefName: "main"},
					},
					2: {
						{Number: 2, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-2", BaseRefName: "main",
							StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 2, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 2, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-2", BaseRefName: "main"},
					},
				},
				compareResults: []githubcli.BranchComparison{
					{BehindBy: 0},
					{BehindBy: 0},
					{BehindBy: 1},
					{BehindBy: 0},
					{BehindBy: 0},
				},
				runResults: map[string][][]githubcli.WorkflowRun{
					"main": {
						{
							{Name: "CI", Status: "completed", Conclusion: "success", HeadSHA: "merge-sha-1", StartedAt: time.Now().Add(1 * time.Hour).Format(time.RFC3339)},
						},
					},
				},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				viewErrors:    map[int]error{},
			},
			wantMerged:       2,
			wantApproveCalls: []int{1, 2},
			wantMergeCalls:   []int{1, 2},
			wantCommentCalls: []commentCall{{Number: 2, Body: "@dependabot rebase"}},
			wantCallSequence: []string{"approve:1", "merge:1", "approve:2", "merge:2"},
		},
		{
			name: "PR already closed is skipped",
			plan: newTestPlan(
				dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
				dependabot.PR{Number: 2, Title: "bump bar", BaseRef: "main"},
			),
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {
						{Number: 1, State: "MERGED", Mergeable: "UNKNOWN", BaseRefName: "main"},
					},
					2: {
						{Number: 2, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-2", BaseRefName: "main",
							StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 2, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 2, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-2", BaseRefName: "main"},
					},
				},
				compareResults: []githubcli.BranchComparison{
					{BehindBy: 0},
					{BehindBy: 0},
				},
				runResults:    map[string][][]githubcli.WorkflowRun{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				viewErrors:    map[int]error{},
			},
			wantMerged:       1,
			wantSkipped:      1,
			wantApproveCalls: []int{2},
			wantMergeCalls:   []int{2},
			wantCallSequence: []string{"approve:2", "merge:2"},
		},
		{
			name: "branch behind triggers rebase comment",
			plan: newTestPlan(
				dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
			),
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main",
							StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main",
							StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main"},
					},
				},
				compareResults: []githubcli.BranchComparison{
					{BehindBy: 1},
					{BehindBy: 0},
					{BehindBy: 0},
				},
				runResults:    map[string][][]githubcli.WorkflowRun{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				viewErrors:    map[int]error{},
			},
			wantMerged:       1,
			wantApproveCalls: []int{1},
			wantMergeCalls:   []int{1},
			wantCommentCalls: []commentCall{{Number: 1, Body: "@dependabot rebase"}},
			wantCallSequence: []string{"approve:1", "merge:1"},
		},
		{
			name: "rebase comment failure stops execution",
			plan: newTestPlan(
				dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
			),
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {
						{Number: 1, State: "OPEN", HeadRefName: "feature-1", BaseRefName: "main"},
					},
				},
				compareResults: []githubcli.BranchComparison{
					{BehindBy: 1},
				},
				commentErrors: map[int]error{1: fmt.Errorf("permission denied")},
				viewErrors:    map[int]error{},
				mergeErrors:   map[int]error{},
				runResults:    map[string][][]githubcli.WorkflowRun{},
			},
			wantFailed: true,
			wantErr:    true,
		},
		{
			name: "approval failure stops execution before merge",
			plan: newTestPlan(
				dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
			),
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main",
							StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main"},
					},
				},
				compareResults: []githubcli.BranchComparison{
					{BehindBy: 0},
					{BehindBy: 0},
				},
				runResults:    map[string][][]githubcli.WorkflowRun{},
				approveErrors: map[int]error{1: fmt.Errorf("review denied")},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				viewErrors:    map[int]error{},
			},
			wantFailed:       true,
			wantErr:          true,
			wantApproveCalls: []int{1},
			wantMergeCalls:   []int{},
			wantCallSequence: []string{"approve:1"},
		},
		{
			name: "CI check failure stops execution",
			plan: newTestPlan(
				dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
			),
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main"},
						{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "failure"}}},
					},
				},
				compareResults: []githubcli.BranchComparison{
					{BehindBy: 0},
				},
				runResults:    map[string][][]githubcli.WorkflowRun{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				viewErrors:    map[int]error{},
			},
			wantFailed:       true,
			wantErr:          true,
			wantErrIs:        []error{ErrExecutionFailed, ErrCheckFailed},
			wantFailedErrIs:  []error{ErrCheckFailed},
			wantApproveCalls: []int{},
			wantMergeCalls:   []int{},
			wantCallSequence: []string{},
		},
		{
			name: "merge conflict stops execution",
			plan: newTestPlan(
				dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
			),
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main",
							StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 1, State: "OPEN", Mergeable: "CONFLICTING", HeadRefName: "feature-1", BaseRefName: "main"},
					},
				},
				compareResults: []githubcli.BranchComparison{
					{BehindBy: 0},
				},
				runResults:    map[string][][]githubcli.WorkflowRun{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				viewErrors:    map[int]error{},
			},
			wantFailed:      true,
			wantErr:         true,
			wantErrIs:       []error{ErrExecutionFailed, ErrMergeConflict},
			wantFailedErrIs: []error{ErrMergeConflict},
		},
		{
			name: "branch still behind after update stops execution",
			plan: newTestPlan(
				dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
			),
			op: &fakeOperator{
				viewResults: map[int][]githubcli.PRDetail{
					1: {
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main",
							StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
						{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main"},
					},
				},
				compareResults: []githubcli.BranchComparison{
					{BehindBy: 0},
					{BehindBy: 1},
				},
				runResults:    map[string][][]githubcli.WorkflowRun{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
				viewErrors:    map[int]error{},
			},
			wantFailed: true,
			wantErr:    true,
			wantErrIs:  []error{ErrExecutionFailed},
		},
		{
			name: "cancelled context returns error",
			plan: newTestPlan(
				dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
			),
			op: &fakeOperator{
				viewResults:   map[int][]githubcli.PRDetail{},
				viewErrors:    map[int]error{1: context.Canceled},
				runResults:    map[string][][]githubcli.WorkflowRun{},
				mergeErrors:   map[int]error{},
				commentErrors: map[int]error{},
			},
			wantFailed: true,
			wantErr:    true,
			wantErrIs:  []error{context.Canceled},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			result, err := Run(ctx, tc.op, tc.plan, "owner/repo", testConfig(), testLogger(), nopProgress{})

			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && !tc.wantFailed && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tc.wantErrIs {
				if !errors.Is(err, want) {
					t.Errorf("returned error: got %v, want error matching %v", err, want)
				}
			}

			merged := result.Merged()
			if len(merged) != tc.wantMerged {
				t.Errorf("merged count: got %d, want %d", len(merged), tc.wantMerged)
			}

			skipped := 0
			for _, pr := range result.Processed {
				if pr.Status == statusSkipped {
					skipped++
				}
			}
			if skipped != tc.wantSkipped {
				t.Errorf("skipped count: got %d, want %d", skipped, tc.wantSkipped)
			}

			if tc.wantFailed {
				failed := result.Failed()
				if failed == nil {
					t.Fatal("expected a failed PR result")
				}
				for _, want := range tc.wantFailedErrIs {
					if !errors.Is(failed.Error, want) {
						t.Errorf("failed error: got %v, want error matching %v", failed.Error, want)
					}
				}
			}

			if tc.wantMergeCalls != nil {
				if len(tc.op.mergeCalls) != len(tc.wantMergeCalls) {
					t.Errorf("merge calls: got %v, want %v", tc.op.mergeCalls, tc.wantMergeCalls)
				}
				for i, want := range tc.wantMergeCalls {
					if i < len(tc.op.mergeCalls) && tc.op.mergeCalls[i] != want {
						t.Errorf("merge call %d: got %d, want %d", i, tc.op.mergeCalls[i], want)
					}
				}
			}

			if tc.wantApproveCalls != nil {
				if len(tc.op.approveCalls) != len(tc.wantApproveCalls) {
					t.Errorf("approve calls: got %v, want %v", tc.op.approveCalls, tc.wantApproveCalls)
				}
				for i, want := range tc.wantApproveCalls {
					if i < len(tc.op.approveCalls) && tc.op.approveCalls[i] != want {
						t.Errorf("approve call %d: got %d, want %d", i, tc.op.approveCalls[i], want)
					}
				}
			}

			if tc.wantCommentCalls != nil {
				if len(tc.op.commentCalls) != len(tc.wantCommentCalls) {
					t.Errorf("comment calls: got %v, want %v", tc.op.commentCalls, tc.wantCommentCalls)
				}
				for i, want := range tc.wantCommentCalls {
					if i < len(tc.op.commentCalls) {
						if tc.op.commentCalls[i].Number != want.Number {
							t.Errorf("comment call %d number: got %d, want %d", i, tc.op.commentCalls[i].Number, want.Number)
						}
						if tc.op.commentCalls[i].Body != want.Body {
							t.Errorf("comment call %d body: got %q, want %q", i, tc.op.commentCalls[i].Body, want.Body)
						}
					}
				}
			}

			if tc.wantCallSequence != nil {
				if len(tc.op.callSequence) != len(tc.wantCallSequence) {
					t.Errorf("call sequence: got %v, want %v", tc.op.callSequence, tc.wantCallSequence)
				}
				for i, want := range tc.wantCallSequence {
					if i < len(tc.op.callSequence) && tc.op.callSequence[i] != want {
						t.Errorf("call sequence %d: got %q, want %q", i, tc.op.callSequence[i], want)
					}
				}
			}
		})
	}
}

func TestRunPostMergeDelayHonorsContextCancellation(t *testing.T) {
	originalSleepFunc := sleepFunc
	t.Cleanup(func() {
		sleepFunc = originalSleepFunc
	})

	sleepStarted := make(chan struct{})
	sleepFunc = func(ctx context.Context, _ time.Duration) error {
		close(sleepStarted)
		<-ctx.Done()
		return ctx.Err()
	}

	plan := newTestPlan(
		dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
		dependabot.PR{Number: 2, Title: "bump bar", BaseRef: "main"},
	)

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{
			1: {
				{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main",
					StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
				{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
				{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main"},
			},
		},
		compareResults: []githubcli.BranchComparison{
			{BehindBy: 0},
			{BehindBy: 0},
		},
		runResults:    map[string][][]githubcli.WorkflowRun{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		viewErrors:    map[int]error{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-sleepStarted
		cancel()
	}()

	result, err := Run(ctx, op, plan, "owner/repo", testConfig(), testLogger(), nopProgress{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: got %v, want %v", err, context.Canceled)
	}
	if len(result.Processed) != 1 {
		t.Fatalf("processed count: got %d, want 1", len(result.Processed))
	}
	if result.Processed[0].Status != statusMerged {
		t.Fatalf("first PR status: got %s, want %s", result.Processed[0].Status, statusMerged)
	}
	if len(op.mergeCalls) != 1 || op.mergeCalls[0] != 1 {
		t.Fatalf("merge calls: got %v, want [1]", op.mergeCalls)
	}
}

func TestRunNilLoggerUsesNoopLogger(t *testing.T) {
	t.Parallel()

	plan := newTestPlan(
		dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
	)

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{
			1: {
				{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main",
					StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
				{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
				{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main"},
			},
		},
		compareResults: []githubcli.BranchComparison{
			{BehindBy: 0},
			{BehindBy: 0},
		},
		runResults:    map[string][][]githubcli.WorkflowRun{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		viewErrors:    map[int]error{},
	}

	result, err := Run(context.Background(), op, plan, "owner/repo", testConfig(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Processed) != 1 {
		t.Fatalf("processed count: got %d, want 1", len(result.Processed))
	}
	if result.Processed[0].Status != statusMerged {
		t.Fatalf("status: got %s, want %s", result.Processed[0].Status, statusMerged)
	}
}

func TestRunAdminOverrideContinuesAfterFailedChecks(t *testing.T) {
	t.Parallel()

	plan := newTestPlan(
		dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
	)

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{
			1: {
				{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main"},
				{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "failure"}, {Name: "lint", Conclusion: "success"}}},
				{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main"},
			},
		},
		compareResults: []githubcli.BranchComparison{
			{BehindBy: 0},
			{BehindBy: 0},
		},
		runResults:    map[string][][]githubcli.WorkflowRun{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		viewErrors:    map[int]error{},
	}

	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := testConfig()
	cfg.Admin = true

	result, err := Run(context.Background(), op, plan, "owner/repo", cfg, log, nopProgress{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Merged()) != 1 {
		t.Fatalf("merged count: got %d, want 1", len(result.Merged()))
	}
	if len(op.mergeCalls) != 1 || op.mergeCalls[0] != 1 {
		t.Fatalf("merge calls: got %v, want [1]", op.mergeCalls)
	}
	if len(op.approveCalls) != 1 || op.approveCalls[0] != 1 {
		t.Fatalf("approve calls: got %v, want [1]", op.approveCalls)
	}
	if len(op.mergeAdmin) != 1 || !op.mergeAdmin[0] {
		t.Fatalf("merge admin flags: got %v, want [true]", op.mergeAdmin)
	}
	if len(op.callSequence) != 2 || op.callSequence[0] != "approve:1" || op.callSequence[1] != "merge:1" {
		t.Fatalf("call sequence: got %v, want [approve:1 merge:1]", op.callSequence)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "admin override: continuing despite failed CI checks") {
		t.Fatalf("log output = %q, want admin override summary warning", logOutput)
	}
	if !strings.Contains(logOutput, "admin override: bypassed failed check") {
		t.Fatalf("log output = %q, want per-check admin override warning", logOutput)
	}
	if !strings.Contains(logOutput, "check=ci") {
		t.Fatalf("log output = %q, want failed check name", logOutput)
	}
}

type spyProgress struct {
	mu       sync.Mutex
	statuses []string
	incs     int
}

func (s *spyProgress) SetStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses = append(s.statuses, status)
}

func (s *spyProgress) Increment() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.incs++
}

func TestRunProgressUpdates(t *testing.T) {
	t.Parallel()

	plan := newTestPlan(
		dependabot.PR{Number: 10, Title: "bump alpha", BaseRef: "main"},
		dependabot.PR{Number: 20, Title: "bump beta", BaseRef: "main"},
	)

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{
			10: {
				{Number: 10, State: "MERGED"},
			},
			20: {
				{Number: 20, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-20", BaseRefName: "main",
					StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
				{Number: 20, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
				{Number: 20, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-20", BaseRefName: "main"},
			},
		},
		compareResults: []githubcli.BranchComparison{
			{BehindBy: 0},
			{BehindBy: 0},
		},
		runResults:    map[string][][]githubcli.WorkflowRun{},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		viewErrors:    map[int]error{},
	}

	spy := &spyProgress{}
	result, err := Run(context.Background(), op, plan, "owner/repo", testConfig(), testLogger(), spy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Processed) != 2 {
		t.Fatalf("processed: got %d, want 2", len(result.Processed))
	}

	spy.mu.Lock()
	defer spy.mu.Unlock()

	if spy.incs != 2 {
		t.Errorf("increment calls: got %d, want 2", spy.incs)
	}

	if len(spy.statuses) == 0 {
		t.Fatal("expected at least one SetStatus call")
	}

	// Verify that skipped PR #10 still produced a status update
	foundSkipped := false
	for _, s := range spy.statuses {
		if s == "Skipped PR #10" {
			foundSkipped = true
			break
		}
	}
	if !foundSkipped {
		t.Errorf("expected 'Skipped PR #10' status, got: %v", spy.statuses)
	}

	// Verify that merged PR #20 produced a merge status
	foundMerged := false
	for _, s := range spy.statuses {
		if s == "Merged PR #20" {
			foundMerged = true
			break
		}
	}
	if !foundMerged {
		t.Errorf("expected 'Merged PR #20' status, got: %v", spy.statuses)
	}
}

func TestRunPostMergeFailureMarksMergedPRFailed(t *testing.T) {
	t.Parallel()

	plan := newTestPlan(
		dependabot.PR{Number: 1, Title: "bump foo", BaseRef: "main"},
		dependabot.PR{Number: 2, Title: "bump bar", BaseRef: "main"},
	)

	op := &fakeOperator{
		viewResults: map[int][]githubcli.PRDetail{
			1: {
				{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main",
					StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
				{Number: 1, State: "OPEN", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "success"}}},
				{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "feature-1", BaseRefName: "main"},
				{Number: 1, State: "MERGED", BaseRefName: "main", MergeCommit: githubcli.MergeCommit{OID: "merge-sha-1"}},
			},
		},
		compareResults: []githubcli.BranchComparison{
			{BehindBy: 0},
			{BehindBy: 0},
		},
		runResults: map[string][][]githubcli.WorkflowRun{
			"main": {{
				{Name: "CI", Status: "completed", Conclusion: "failure", HeadSHA: "merge-sha-1"},
			}},
		},
		mergeErrors:   map[int]error{},
		commentErrors: map[int]error{},
		viewErrors:    map[int]error{},
	}

	result, err := Run(context.Background(), op, plan, "owner/repo", testConfig(), testLogger(), nopProgress{})
	if !errors.Is(err, ErrExecutionFailed) {
		t.Fatalf("error: got %v, want %v", err, ErrExecutionFailed)
	}
	if len(result.Processed) != 1 {
		t.Fatalf("processed count: got %d, want 1", len(result.Processed))
	}
	if result.Processed[0].Status != statusFailed {
		t.Fatalf("status: got %s, want %s", result.Processed[0].Status, statusFailed)
	}
	if result.Failed() == nil {
		t.Fatal("expected failed result")
	}
	if errors.Is(result.Processed[0].Error, ErrPostMergeTimeout) {
		t.Fatalf("error: got %v, want non-timeout post-merge failure", result.Processed[0].Error)
	}
	if len(op.mergeCalls) != 1 || op.mergeCalls[0] != 1 {
		t.Fatalf("merge calls: got %v, want [1]", op.mergeCalls)
	}
}

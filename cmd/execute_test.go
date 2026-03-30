package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/executor"
	"github.com/JakeTRogers/depflow/internal/githubcli"
)

type fakeRepoResolver struct {
	repo  string
	err   error
	calls int
}

func (f *fakeRepoResolver) ResolveRepo(_ context.Context) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}

	return f.repo, nil
}

type fakeExecuteOperator struct {
	viewedRepos    []string
	comparedRepos  []string
	approvedRepos  []string
	mergedRepos    []string
	viewResults    []githubcli.PRDetail
	compareResults []githubcli.BranchComparison
	runResults     map[string][][]githubcli.WorkflowRun
}

func (f *fakeExecuteOperator) ViewPullRequest(_ context.Context, repo string, number int) (githubcli.PRDetail, error) {
	f.viewedRepos = append(f.viewedRepos, repo)
	if len(f.viewResults) > 0 {
		result := f.viewResults[0]
		f.viewResults = f.viewResults[1:]
		return result, nil
	}

	return githubcli.PRDetail{
		Number:      number,
		Title:       "Bump lodash from 4.17.20 to 4.17.21",
		State:       "OPEN",
		Mergeable:   "MERGEABLE",
		HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
		BaseRefName: "main",
		StatusCheckRollup: []githubcli.StatusCheck{{
			Name:       "ci",
			Conclusion: "success",
		}},
	}, nil
}

func (f *fakeExecuteOperator) MergePullRequest(_ context.Context, repo string, _ int) error {
	f.mergedRepos = append(f.mergedRepos, repo)
	return nil
}

func (f *fakeExecuteOperator) ApprovePullRequest(_ context.Context, repo string, _ int) error {
	f.approvedRepos = append(f.approvedRepos, repo)
	return nil
}

func (f *fakeExecuteOperator) CommentOnPR(context.Context, string, int, string) error {
	return nil
}

func (f *fakeExecuteOperator) ListWorkflowRuns(_ context.Context, _ string, branch string) ([]githubcli.WorkflowRun, error) {
	if len(f.runResults) == 0 {
		return nil, nil
	}
	results := f.runResults[branch]
	if len(results) == 0 {
		return nil, nil
	}
	result := results[0]
	f.runResults[branch] = results[1:]
	return result, nil
}

func (f *fakeExecuteOperator) CompareBranches(_ context.Context, repo string, _, _ string) (githubcli.BranchComparison, error) {
	f.comparedRepos = append(f.comparedRepos, repo)
	if len(f.compareResults) > 0 {
		result := f.compareResults[0]
		f.compareResults = f.compareResults[1:]
		return result, nil
	}
	return githubcli.BranchComparison{BehindBy: 0}, nil
}

func TestExecuteCommandResolvesRepoBeforeExecutorRun(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{{
			Number:      42,
			Title:       "Bump lodash from 4.17.20 to 4.17.21",
			URL:         "https://example.test/pr/42",
			Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
			Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
			HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
			BaseRefName: "main",
		}},
	}
	operator := &fakeExecuteOperator{}
	resolver := &fakeRepoResolver{repo: "owner/repo"}

	cmd := newRootCommand(commandDeps{lister: lister, operator: operator, resolver: resolver})
	cmd.SetArgs([]string{"execute"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if lister.repo != "" {
		t.Fatalf("lister repo = %q, want empty repo so gh can infer it during discovery", lister.repo)
	}
	if resolver.calls != 1 {
		t.Fatalf("ResolveRepo() calls = %d, want 1", resolver.calls)
	}
	assertAllReposEqual(t, operator.viewedRepos, "owner/repo")
	assertAllReposEqual(t, operator.comparedRepos, "owner/repo")
	assertAllReposEqual(t, operator.approvedRepos, "owner/repo")
	assertAllReposEqual(t, operator.mergedRepos, "owner/repo")
}

func TestExecuteCommandFailsWhenRepoCannotBeResolved(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{{
			Number:      42,
			Title:       "Bump lodash from 4.17.20 to 4.17.21",
			URL:         "https://example.test/pr/42",
			Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
			Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
			HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
			BaseRefName: "main",
		}},
	}
	operator := &fakeExecuteOperator{}
	resolver := &fakeRepoResolver{err: errors.New("not in a git repository")}

	cmd := newRootCommand(commandDeps{lister: lister, operator: operator, resolver: resolver})
	cmd.SetArgs([]string{"execute"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "resolving current repository") {
		t.Fatalf("error = %q, want repo resolution context", err)
	}
	if !strings.Contains(err.Error(), "--repo OWNER/REPO") {
		t.Fatalf("error = %q, want --repo hint", err)
	}
	if len(operator.viewedRepos) != 0 || len(operator.comparedRepos) != 0 || len(operator.approvedRepos) != 0 || len(operator.mergedRepos) != 0 {
		t.Fatalf("executor was called before repo resolution succeeded: viewed=%v compared=%v approved=%v merged=%v", operator.viewedRepos, operator.comparedRepos, operator.approvedRepos, operator.mergedRepos)
	}
}

func TestExecuteCommandRejectsNonPositiveDurations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		flag  string
		value string
	}{
		{name: "poll interval zero", flag: "--poll-interval", value: "0s"},
		{name: "check timeout negative", flag: "--check-timeout", value: "-1s"},
		{name: "post merge delay zero", flag: "--post-merge-delay", value: "0s"},
		{name: "post merge timeout negative", flag: "--post-merge-timeout", value: "-1s"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lister := &fakeLister{}
			cmd := newRootCommand(commandDeps{lister: lister})
			cmd.SetArgs([]string{"execute", tc.flag, tc.value})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			err := cmd.Execute()
			if err == nil {
				t.Fatal("Execute() error = nil, want non-nil")
			}

			want := "flag " + tc.flag + " must be greater than zero"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %q, want %q", err, want)
			}
			if len(lister.limits) != 0 {
				t.Fatalf("ListOpenPullRequests() should not be called for invalid durations, got limits %v", lister.limits)
			}
		})
	}
}

func TestExecuteCommandRejectsInvalidPollingRelationships(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "poll interval below minimum",
			args: []string{"execute", "--poll-interval", "4s"},
			want: "flag --poll-interval must be at least 5s",
		},
		{
			name: "check timeout must exceed poll interval",
			args: []string{"execute", "--poll-interval", "5s", "--check-timeout", "5s"},
			want: "flag --check-timeout must be greater than --poll-interval",
		},
		{
			name: "post merge timeout must exceed poll interval",
			args: []string{"execute", "--poll-interval", "5s", "--post-merge-timeout", "5s"},
			want: "flag --post-merge-timeout must be greater than --poll-interval",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lister := &fakeLister{}
			cmd := newRootCommand(commandDeps{lister: lister})
			cmd.SetArgs(tc.args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			err := cmd.Execute()
			if err == nil {
				t.Fatal("Execute() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want %q", err, tc.want)
			}
			if len(lister.limits) != 0 {
				t.Fatalf("ListOpenPullRequests() should not be called for invalid polling options, got limits %v", lister.limits)
			}
		})
	}
}

func TestExecuteCommandDryRunPrintsPlanWithoutResolvingRepoOrExecuting(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{{
			Number:      42,
			Title:       "Bump lodash from 4.17.20 to 4.17.21",
			URL:         "https://example.test/pr/42",
			Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
			Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
			HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
			BaseRefName: "main",
		}},
	}
	operator := &fakeExecuteOperator{}
	resolver := &fakeRepoResolver{repo: "owner/repo"}

	cmd := newRootCommand(commandDeps{lister: lister, operator: operator, resolver: resolver})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetArgs([]string{"execute", "--dry-run"})
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Dry run: 1 PR(s) would be processed in this order:") {
		t.Fatalf("stdout = %q, want dry-run header", stdout.String())
	}
	if !strings.Contains(stdout.String(), "1. #42 [patch] Bump lodash from 4.17.20 to 4.17.21") {
		t.Fatalf("stdout = %q, want planned PR entry", stdout.String())
	}
	if strings.Contains(stdout.String(), "Excluding major updates") {
		t.Fatalf("stdout = %q, should not render exclusion notice when no major PRs exist", stdout.String())
	}
	if resolver.calls != 0 {
		t.Fatalf("ResolveRepo() calls = %d, want 0 in dry-run mode", resolver.calls)
	}
	if len(operator.viewedRepos) != 0 || len(operator.comparedRepos) != 0 || len(operator.approvedRepos) != 0 || len(operator.mergedRepos) != 0 {
		t.Fatalf("executor should not run in dry-run mode: viewed=%v compared=%v approved=%v merged=%v", operator.viewedRepos, operator.comparedRepos, operator.approvedRepos, operator.mergedRepos)
	}
}

func TestExecuteCommandExcludesMajorUpdatesByDefault(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
			},
			{
				Number:      2,
				Title:       "Bump github.com/pkg/errors from 0.9.1 to 1.0.0",
				URL:         "https://example.test/pr/2",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/go_modules/github.com/pkg/errors-1.0.0",
				BaseRefName: "main",
			},
		},
	}
	operator := &fakeExecuteOperator{}
	resolver := &fakeRepoResolver{repo: "owner/repo"}

	cmd := newRootCommand(commandDeps{lister: lister, operator: operator, resolver: resolver})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetArgs([]string{"execute"})
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Excluding major updates (1):") {
		t.Fatalf("stdout = %q, want exclusion notice", stdout.String())
	}
	if !strings.Contains(stdout.String(), "#2 Bump github.com/pkg/errors from 0.9.1 to 1.0.0") {
		t.Fatalf("stdout = %q, want excluded major PR", stdout.String())
	}
	if resolver.calls != 1 {
		t.Fatalf("ResolveRepo() calls = %d, want 1", resolver.calls)
	}
	if len(operator.approvedRepos) != 1 || len(operator.mergedRepos) != 1 {
		t.Fatalf("executor should process only the non-major PR: approved=%v merged=%v", operator.approvedRepos, operator.mergedRepos)
	}
}

func TestExecuteCommandAllMajorIsNoOp(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{{
			Number:      2,
			Title:       "Bump github.com/pkg/errors from 0.9.1 to 1.0.0",
			URL:         "https://example.test/pr/2",
			Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
			Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
			HeadRefName: "dependabot/go_modules/github.com/pkg/errors-1.0.0",
			BaseRefName: "main",
		}},
	}
	operator := &fakeExecuteOperator{}
	resolver := &fakeRepoResolver{repo: "owner/repo"}

	cmd := newRootCommand(commandDeps{lister: lister, operator: operator, resolver: resolver})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetArgs([]string{"execute"})
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Excluding major updates (1):") {
		t.Fatalf("stdout = %q, want exclusion notice", stdout.String())
	}
	if !strings.Contains(stdout.String(), noEligiblePRsMessage) {
		t.Fatalf("stdout = %q, want no-op message", stdout.String())
	}
	if resolver.calls != 0 {
		t.Fatalf("ResolveRepo() calls = %d, want 0", resolver.calls)
	}
	if len(operator.viewedRepos) != 0 || len(operator.comparedRepos) != 0 || len(operator.approvedRepos) != 0 || len(operator.mergedRepos) != 0 {
		t.Fatalf("executor should not run for all-major no-op: viewed=%v compared=%v approved=%v merged=%v", operator.viewedRepos, operator.comparedRepos, operator.approvedRepos, operator.mergedRepos)
	}
}

func TestExecuteCommandDryRunWithExclusions(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
			},
			{
				Number:      2,
				Title:       "Bump github.com/pkg/errors from 0.9.1 to 1.0.0",
				URL:         "https://example.test/pr/2",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/go_modules/github.com/pkg/errors-1.0.0",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "execute", "--dry-run")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "Excluding major updates (1):") {
		t.Fatalf("stdout = %q, want exclusion notice", stdout)
	}
	if !strings.Contains(stdout, "Dry run: 1 PR(s) would be processed in this order:") {
		t.Fatalf("stdout = %q, want filtered dry-run header", stdout)
	}
	if !strings.Contains(stdout, "1. #1 [patch] Bump lodash from 4.17.20 to 4.17.21") {
		t.Fatalf("stdout = %q, want non-major dry-run item", stdout)
	}
}

func TestExecuteCommandIncludeMajorFlagIncludesAllPRs(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
			},
			{
				Number:      2,
				Title:       "Bump github.com/pkg/errors from 0.9.1 to 1.0.0",
				URL:         "https://example.test/pr/2",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/go_modules/github.com/pkg/errors-1.0.0",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "execute", "--dry-run", "-M")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if strings.Contains(stdout, "Excluding major updates") {
		t.Fatalf("stdout = %q, should not exclude majors with -M", stdout)
	}
	if !strings.Contains(stdout, "Dry run: 2 PR(s) would be processed in this order:") {
		t.Fatalf("stdout = %q, want full dry-run header", stdout)
	}
	if !strings.Contains(stdout, "2. #2 [major] Bump github.com/pkg/errors from 0.9.1 to 1.0.0") {
		t.Fatalf("stdout = %q, want included major dry-run item", stdout)
	}
}

func TestExecuteCommandPropagatesExecutorFailure(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{{
			Number:      42,
			Title:       "Bump lodash from 4.17.20 to 4.17.21",
			URL:         "https://example.test/pr/42",
			Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
			Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
			HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
			BaseRefName: "main",
		}},
	}
	operator := &fakeExecuteOperator{
		viewResults: []githubcli.PRDetail{
			{
				Number:      42,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				State:       "OPEN",
				Mergeable:   "MERGEABLE",
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
			},
			{
				Number:      42,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				State:       "OPEN",
				Mergeable:   "MERGEABLE",
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
				StatusCheckRollup: []githubcli.StatusCheck{{
					Name:       "ci",
					Conclusion: "failure",
				}},
			},
		},
		compareResults: []githubcli.BranchComparison{{BehindBy: 0}},
	}

	cmd := newRootCommand(commandDeps{
		lister:   lister,
		operator: operator,
		resolver: &fakeRepoResolver{repo: "owner/repo"},
	})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetArgs([]string{"execute"})
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if !errors.Is(err, executor.ErrExecutionFailed) {
		t.Fatalf("error = %v, want ErrExecutionFailed", err)
	}
	if len(operator.mergedRepos) != 0 {
		t.Fatalf("merged repos = %v, want no merge attempts after executor failure", operator.mergedRepos)
	}
	if !strings.Contains(stdout.String(), "Execution Summary") {
		t.Fatalf("stdout = %q, want execution summary", stdout.String())
	}
	if !strings.Contains(stdout.String(), "#42 Bump lodash from 4.17.20 to 4.17.21") || !strings.Contains(stdout.String(), "failed") {
		t.Fatalf("stdout = %q, want failed PR summary", stdout.String())
	}
}

func TestExecuteCommandReportsPostMergeFailure(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump alpha from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/npm_and_yarn/alpha-4.17.21",
				BaseRefName: "main",
			},
			{
				Number:      2,
				Title:       "Bump zeta from 5.0.0 to 5.0.1",
				URL:         "https://example.test/pr/2",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/npm_and_yarn/zeta-5.0.1",
				BaseRefName: "main",
			},
		},
	}
	operator := &fakeExecuteOperator{
		viewResults: []githubcli.PRDetail{
			{
				Number:      1,
				Title:       "Bump alpha from 4.17.20 to 4.17.21",
				State:       "OPEN",
				Mergeable:   "MERGEABLE",
				HeadRefName: "dependabot/npm_and_yarn/alpha-4.17.21",
				BaseRefName: "main",
				StatusCheckRollup: []githubcli.StatusCheck{{
					Name:       "ci",
					Conclusion: "success",
				}},
			},
			{
				Number:      1,
				Title:       "Bump alpha from 4.17.20 to 4.17.21",
				State:       "OPEN",
				Mergeable:   "MERGEABLE",
				HeadRefName: "dependabot/npm_and_yarn/alpha-4.17.21",
				BaseRefName: "main",
				StatusCheckRollup: []githubcli.StatusCheck{{
					Name:       "ci",
					Conclusion: "success",
				}},
			},
			{
				Number:      1,
				Title:       "Bump alpha from 4.17.20 to 4.17.21",
				State:       "OPEN",
				Mergeable:   "MERGEABLE",
				HeadRefName: "dependabot/npm_and_yarn/alpha-4.17.21",
				BaseRefName: "main",
			},
			{
				Number:      1,
				Title:       "Bump alpha from 4.17.20 to 4.17.21",
				State:       "MERGED",
				BaseRefName: "main",
				MergeCommit: githubcli.MergeCommit{OID: "merge-sha-1"},
			},
		},
		compareResults: []githubcli.BranchComparison{{BehindBy: 0}, {BehindBy: 0}},
		runResults: map[string][][]githubcli.WorkflowRun{
			"main": {{
				{Name: "CI", Status: "completed", Conclusion: "failure", HeadSHA: "merge-sha-1"},
			}},
		},
	}

	cmd := newRootCommand(commandDeps{
		lister:   lister,
		operator: operator,
		resolver: &fakeRepoResolver{repo: "owner/repo"},
	})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetArgs([]string{"execute"})
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if !errors.Is(err, executor.ErrExecutionFailed) {
		t.Fatalf("error = %v, want ErrExecutionFailed", err)
	}
	if strings.Contains(stdout.String(), "#1 Bump alpha from 4.17.20 to 4.17.21 — merged") {
		t.Fatalf("stdout = %q, want merged PR to be reported as failed after post-merge CI failure", stdout.String())
	}
	if !strings.Contains(stdout.String(), "#1 Bump alpha from 4.17.20 to 4.17.21 — failed") {
		t.Fatalf("stdout = %q, want failed PR summary", stdout.String())
	}
	if len(operator.mergedRepos) != 1 {
		t.Fatalf("merged repos = %v, want one merge attempt", operator.mergedRepos)
	}
}

func assertAllReposEqual(t *testing.T, got []string, want string) {
	t.Helper()

	if len(got) == 0 {
		t.Fatalf("repo list = %v, want at least one call with %q", got, want)
	}

	for _, repo := range got {
		if repo != want {
			t.Fatalf("repo = %q, want %q across all executor calls (%v)", repo, want, got)
		}
	}
}

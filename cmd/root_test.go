package cmd

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/githubcli"
)

type fakeLister struct {
	pullRequests []githubcli.PullRequest
	err          error
	repo         string
	limit        int
	limits       []int
	respectLimit bool
}

func (f *fakeLister) ListOpenPullRequests(_ context.Context, repo string, limit int) ([]githubcli.PullRequest, error) {
	f.repo = repo
	f.limit = limit
	f.limits = append(f.limits, limit)
	if f.err != nil {
		return nil, f.err
	}

	pullRequests := append([]githubcli.PullRequest(nil), f.pullRequests...)
	if f.respectLimit && limit < len(pullRequests) {
		return pullRequests[:limit], nil
	}

	return pullRequests, nil
}

func executeTestCommand(t *testing.T, lister prLister, args ...string) (string, error) {
	t.Helper()

	cmd := newRootCommand(commandDeps{lister: lister})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return stdout.String(), err
}

func TestVersionCommandOutput(t *testing.T) {
	t.Parallel()

	stdout, err := executeTestCommand(t, &fakeLister{}, "version")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := "depflow " + version + " (" + runtime.GOOS + "/" + runtime.GOARCH + ")"
	if !strings.Contains(stdout, want) {
		t.Fatalf("version output %q does not contain %q", stdout, want)
	}
}

func TestScanCommandFiltersDependabotPRs(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      42,
				Title:       "Bump github.com/golangci/golangci-lint from 1.58.0 to 1.59.0",
				URL:         "https://example.test/pr/42",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/go_modules/github.com/golangci/golangci-lint-1.59.0",
				BaseRefName: "main",
			},
			{
				Number:      77,
				Title:       "Feature work",
				URL:         "https://example.test/pr/77",
				Author:      githubcli.PullRequestAuthor{Login: "octocat"},
				HeadRefName: "feature/work",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "--repo", "owner/repo", "--limit", "25", "scan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if lister.repo != "owner/repo" {
		t.Fatalf("repo = %q, want owner/repo", lister.repo)
	}
	if lister.limit < 25 {
		t.Fatalf("limit = %d, want at least 25", lister.limit)
	}
	if !strings.Contains(stdout, "#42 Bump github.com/golangci/golangci-lint from 1.58.0 to 1.59.0") {
		t.Fatalf("scan output missing Dependabot PR: %q", stdout)
	}
	if strings.Contains(stdout, "Feature work") {
		t.Fatalf("scan output should not include non-Dependabot PRs: %q", stdout)
	}
	if !strings.Contains(stdout, "classification: ecosystem=go-modules change=minor grouped=no dev-tooling=yes infra-sensitive=no") {
		t.Fatalf("scan output missing classification details: %q", stdout)
	}
}

func TestScanCommandAppliesLimitAfterFiltering(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		respectLimit: true,
		pullRequests: []githubcli.PullRequest{
			{
				Number:      200,
				Title:       "Feature work",
				URL:         "https://example.test/pr/200",
				Author:      githubcli.PullRequestAuthor{Login: "octocat"},
				HeadRefName: "feature/work",
				BaseRefName: "main",
			},
			{
				Number:      150,
				Title:       "Maintenance cleanup",
				URL:         "https://example.test/pr/150",
				Author:      githubcli.PullRequestAuthor{Login: "hubot"},
				HeadRefName: "chore/cleanup",
				BaseRefName: "main",
			},
			{
				Number:      7,
				Title:       "Bump github.com/pkg/errors from 0.8.1 to 0.9.1",
				URL:         "https://example.test/pr/7",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/go_modules/github.com/pkg/errors-0.9.1",
				BaseRefName: "main",
			},
			{
				Number:      3,
				Title:       "Bump github.com/google/uuid from 1.5.0 to 1.5.1",
				URL:         "https://example.test/pr/3",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/go_modules/github.com/google/uuid-1.5.1",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "--repo", "owner/repo", "--limit", "1", "scan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if strings.Contains(stdout, "No open Dependabot pull requests found.") {
		t.Fatalf("scan output incorrectly reported an empty result: %q", stdout)
	}
	if !strings.Contains(stdout, "#3 Bump github.com/google/uuid from 1.5.0 to 1.5.1") {
		t.Fatalf("scan output missing the lowest-numbered Dependabot PR after filtering: %q", stdout)
	}
	if strings.Contains(stdout, "#7 Bump github.com/pkg/errors from 0.8.1 to 0.9.1") {
		t.Fatalf("scan output should truncate after deterministic sorting: %q", stdout)
	}
	if len(lister.limits) == 0 || lister.limits[len(lister.limits)-1] <= 1 {
		t.Fatalf("ListOpenPullRequests limits = %v, want a request larger than the user limit", lister.limits)
	}
	if lister.limit != lister.limits[len(lister.limits)-1] {
		t.Fatalf("final recorded limit = %d, want %d", lister.limit, lister.limits[len(lister.limits)-1])
	}
}

func TestPlanCommandOrdersPRs(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      5,
				Title:       "Bump lodash in the frontend group",
				URL:         "https://example.test/pr/5",
				Author:      githubcli.PullRequestAuthor{Login: "app/dependabot"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/npm_and_yarn/packages-a599cde353",
				BaseRefName: "main",
			},
			{
				Number:      2,
				Title:       "Bump github.com/google/uuid from 1.5.0 to 1.5.1",
				URL:         "https://example.test/pr/2",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/go_modules/github.com/google/uuid-1.5.1",
				BaseRefName: "main",
			},
			{
				Number:      1,
				Title:       "Bump actions/cache from 4.2.0 to 4.2.1",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/github_actions/actions/cache-4.2.1",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "plan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	first := strings.Index(stdout, "1. #1 [ci]")
	second := strings.Index(stdout, "2. #2 [patch]")
	third := strings.Index(stdout, "3. #5 [grouped]")
	if first == -1 || second == -1 || third == -1 {
		t.Fatalf("plan output missing expected order markers: %q", stdout)
	}
	if first >= second || second >= third {
		t.Fatalf("plan output order is not deterministic: %q", stdout)
	}
	if !strings.Contains(stdout, "reason: grouped update sorts after simple low-risk updates") {
		t.Fatalf("plan output missing grouped rationale: %q", stdout)
	}
	if strings.Contains(stdout, "Excluded major updates") {
		t.Fatalf("plan output should not render excluded section when no major PRs exist: %q", stdout)
	}
	if !strings.Contains(stdout, "signals: ecosystem=npm-and-yarn change=unknown grouped=yes dev-tooling=no infra-sensitive=no") {
		t.Fatalf("plan output missing grouped signal details: %q", stdout)
	}
	if !strings.Contains(stdout, "dependency: lodash (frontend group)") {
		t.Fatalf("plan output missing grouped dependency summary: %q", stdout)
	}
}

func TestPlanCommandGroupedSummaryWithoutParseableVersionsRemainsIncluded(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{{
			Number:      13,
			Title:       "Bump the npm_and_yarn group with 2 updates",
			Body:        "Updates dependencies to the latest versions.",
			URL:         "https://example.test/pr/13",
			Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
			Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
			HeadRefName: "dependabot/npm_and_yarn/group-frontend-deps",
			BaseRefName: "main",
		}},
	}

	stdout, err := executeTestCommand(t, lister, "plan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "Planned order for 1 Dependabot pull request(s)") {
		t.Fatalf("plan output missing grouped summary plan header: %q", stdout)
	}
	if !strings.Contains(stdout, "1. #13 [grouped] Bump the npm_and_yarn group with 2 updates") {
		t.Fatalf("plan output missing included grouped summary PR: %q", stdout)
	}
	if strings.Contains(stdout, "Excluded major updates") {
		t.Fatalf("plan output should not exclude grouped summaries without parseable versions: %q", stdout)
	}
	if strings.Contains(stdout, noOpenDependabotPRsMessage) {
		t.Fatalf("plan output incorrectly reported no PRs: %q", stdout)
	}
}

func TestPlanCommandExcludesMajorUpdatesByDefault(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump actions/cache from 4.2.0 to 4.2.1",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/github_actions/actions/cache-4.2.1",
				BaseRefName: "main",
			},
			{
				Number:      9,
				Title:       "Bump the npm_and_yarn group with 2 updates",
				Body:        "Updates `next` from 14.2.0 to 15.0.0\nUpdates `react` from 18.3.0 to 18.3.1",
				URL:         "https://example.test/pr/9",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/npm_and_yarn/group-frontend-deps",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "plan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "Planned order for 1 Dependabot pull request(s)") {
		t.Fatalf("plan output missing filtered plan header: %q", stdout)
	}
	if !strings.Contains(stdout, "1. #1 [ci] Bump actions/cache from 4.2.0 to 4.2.1") {
		t.Fatalf("plan output missing included PR: %q", stdout)
	}
	if !strings.Contains(stdout, "Excluded major updates (1):") {
		t.Fatalf("plan output missing excluded section: %q", stdout)
	}
	if !strings.Contains(stdout, "#9 Bump the npm_and_yarn group with 2 updates") {
		t.Fatalf("plan output missing excluded major PR: %q", stdout)
	}
}

func TestPlanCommandIncludeMajorFlagIncludesAllPRs(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump actions/cache from 4.2.0 to 4.2.1",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/github_actions/actions/cache-4.2.1",
				BaseRefName: "main",
			},
			{
				Number:      9,
				Title:       "Bump the npm_and_yarn group with 2 updates",
				Body:        "Updates `next` from 14.2.0 to 15.0.0\nUpdates `react` from 18.3.0 to 18.3.1",
				URL:         "https://example.test/pr/9",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/npm_and_yarn/group-frontend-deps",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "plan", "-M")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "Planned order for 2 Dependabot pull request(s)") {
		t.Fatalf("plan output missing full plan header: %q", stdout)
	}
	if !strings.Contains(stdout, "2. #9 [major] Bump the npm_and_yarn group with 2 updates") {
		t.Fatalf("plan output missing included grouped major PR: %q", stdout)
	}
	if strings.Contains(stdout, "Excluded major updates") {
		t.Fatalf("plan output should not render excluded section with -M: %q", stdout)
	}
}

func TestPlanCommandAllMajorShowsZeroPlanAndExcludedSection(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{{
			Number:      21,
			Title:       "Bump github.com/pkg/errors from 0.9.1 to 1.0.0",
			URL:         "https://example.test/pr/21",
			Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
			Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
			HeadRefName: "dependabot/go_modules/github.com/pkg/errors-1.0.0",
			BaseRefName: "main",
		}},
	}

	stdout, err := executeTestCommand(t, lister, "plan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "Planned order for 0 Dependabot pull request(s)") {
		t.Fatalf("plan output missing zero-plan header: %q", stdout)
	}
	if !strings.Contains(stdout, "Excluded major updates (1):") {
		t.Fatalf("plan output missing excluded section: %q", stdout)
	}
	if strings.Contains(stdout, noOpenDependabotPRsMessage) {
		t.Fatalf("plan output incorrectly reported no PRs: %q", stdout)
	}
}

func TestScanCommandShowsGroupedDependencySummary(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      8,
				Title:       "Bump lodash from 4.17.20 to 4.17.21 in the frontend group",
				URL:         "https://example.test/pr/8",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
				HeadRefName: "dependabot/npm_and_yarn/packages-a599cde353",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "scan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "dependency: lodash (frontend group)") {
		t.Fatalf("scan output missing grouped dependency summary: %q", stdout)
	}
	if !strings.Contains(stdout, "classification: ecosystem=npm-and-yarn change=patch grouped=yes dev-tooling=no infra-sensitive=no") {
		t.Fatalf("scan output missing grouped classification details: %q", stdout)
	}
	if strings.Contains(stdout, "dependency: packages-a599cde353") {
		t.Fatalf("scan output should not fall back to opaque grouped branch slug: %q", stdout)
	}
}

func TestScanCommandNoResults(t *testing.T) {
	t.Parallel()

	stdout, err := executeTestCommand(t, &fakeLister{}, "scan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.TrimSpace(stdout) != "No open Dependabot pull requests found." {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestScanCommandStillShowsMajorPRs(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{{
			Number:      11,
			Title:       "Bump github.com/pkg/errors from 0.9.1 to 1.0.0",
			URL:         "https://example.test/pr/11",
			Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
			Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
			HeadRefName: "dependabot/go_modules/github.com/pkg/errors-1.0.0",
			BaseRefName: "main",
		}},
	}

	stdout, err := executeTestCommand(t, lister, "scan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "#11 Bump github.com/pkg/errors from 0.9.1 to 1.0.0") {
		t.Fatalf("scan output missing major PR: %q", stdout)
	}
	if !strings.Contains(stdout, "classification: ecosystem=go-modules change=major grouped=no dev-tooling=no infra-sensitive=no") {
		t.Fatalf("scan output missing major classification details: %q", stdout)
	}
}

func TestPlanCommandNoResults(t *testing.T) {
	t.Parallel()

	stdout, err := executeTestCommand(t, &fakeLister{}, "plan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.TrimSpace(stdout) != "No open Dependabot pull requests found." {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestScanCommandRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	_, err := executeTestCommand(t, &fakeLister{}, "--limit", "0", "scan")
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "limit must be greater than zero") {
		t.Fatalf("error = %q", err)
	}
}

func TestDiscoveryErrorIncludesRepoHint(t *testing.T) {
	t.Parallel()

	_, err := executeTestCommand(t, &fakeLister{err: context.DeadlineExceeded}, "scan")
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "rerun with --repo OWNER/REPO") {
		t.Fatalf("error = %q", err)
	}
}

func TestFormattingHelpers(t *testing.T) {
	t.Parallel()

	if got := displayOrUnknown(""); got != "unknown" {
		t.Fatalf("displayOrUnknown(\"\") = %q, want unknown", got)
	}
	if got := formatLabels(nil); got != "(none)" {
		t.Fatalf("formatLabels(nil) = %q, want (none)", got)
	}
}

func TestVerboseFlagParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantCount string
	}{
		{"no verbose flag", []string{"version"}, "0"},
		{"single -v", []string{"-v", "version"}, "1"},
		{"double -vv", []string{"-vv", "version"}, "2"},
		{"triple -vvv", []string{"-vvv", "version"}, "3"},
		{"long form once", []string{"--verbose", "version"}, "1"},
		{"long form twice", []string{"--verbose", "--verbose", "version"}, "2"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd := newRootCommand(commandDeps{lister: &fakeLister{}})
			cmd.SetArgs(tc.args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			got := cmd.PersistentFlags().Lookup("verbose").Value.String()
			if got != tc.wantCount {
				t.Errorf("verbose count = %q, want %q", got, tc.wantCount)
			}
		})
	}
}

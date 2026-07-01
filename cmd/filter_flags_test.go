package cmd

import (
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/githubcli"
)

func TestPlanCommandSkipsDraftsByDefault(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
				IsDraft:     true,
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "plan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "Excluded by filters (1):") {
		t.Fatalf("plan output missing excluded section for draft PR: %q", stdout)
	}
	if !strings.Contains(stdout, "draft PR (use --include-drafts to include)") {
		t.Fatalf("plan output missing draft exclusion reason: %q", stdout)
	}
}

func TestPlanCommandIncludeDraftsFlagIncludesDrafts(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
				IsDraft:     true,
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "plan", "--include-drafts")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if strings.Contains(stdout, "Excluded by filters") {
		t.Fatalf("plan output should not exclude drafts with --include-drafts: %q", stdout)
	}
	if !strings.Contains(stdout, "Planned order for 1 Dependabot pull request(s)") {
		t.Fatalf("plan output missing planned draft PR: %q", stdout)
	}
}

func TestScanCommandAlwaysShowsDrafts(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
				IsDraft:     true,
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "scan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "Found 1 open Dependabot pull request(s)") {
		t.Fatalf("scan output should always include draft PRs: %q", stdout)
	}
}

func TestExecuteCommandSkipsDraftsByDefault(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
				IsDraft:     true,
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "execute", "--dry-run")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "draft PR (use --include-drafts to include)") {
		t.Fatalf("execute output missing draft exclusion reason: %q", stdout)
	}
	if !strings.Contains(stdout, noEligiblePRsMessage) {
		t.Fatalf("execute output should report nothing to do: %q", stdout)
	}
}

func TestPlanCommandEcosystemAllowList(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
			},
			{
				Number:      2,
				Title:       "Bump actions/cache from 4.2.0 to 4.2.1",
				URL:         "https://example.test/pr/2",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				HeadRefName: "dependabot/github_actions/actions/cache-4.2.1",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "plan", "--ecosystem=github-actions")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "Planned order for 1 Dependabot pull request(s)") {
		t.Fatalf("plan output should only include the github-actions PR: %q", stdout)
	}
	if !strings.Contains(stdout, "#2 [ci] Bump actions/cache") {
		t.Fatalf("plan output missing allow-listed ecosystem PR: %q", stdout)
	}
	if !strings.Contains(stdout, `ecosystem "npm-and-yarn" not in --ecosystem allow-list`) {
		t.Fatalf("plan output missing ecosystem exclusion reason: %q", stdout)
	}
}

func TestPlanCommandRequireAndExcludeLabel(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}, {Name: "go"}},
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
			},
			{
				Number:      2,
				Title:       "Bump react from 18.3.0 to 18.3.1",
				URL:         "https://example.test/pr/2",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}, {Name: "do-not-merge"}},
				HeadRefName: "dependabot/npm_and_yarn/react-18.3.1",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "plan", "--require-label=dependencies", "--exclude-label=do-not-merge")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "Planned order for 1 Dependabot pull request(s)") {
		t.Fatalf("plan output should only include PR #1: %q", stdout)
	}
	if !strings.Contains(stdout, `label "do-not-merge" excluded by --exclude-label`) {
		t.Fatalf("plan output missing label exclusion reason: %q", stdout)
	}
}

func TestPlanCommandSkipGrouped(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      8,
				Title:       "Bump the frontend group with 2 updates",
				URL:         "https://example.test/pr/8",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				HeadRefName: "dependabot/npm_and_yarn/group-frontend-deps",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "plan", "--skip-grouped")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "grouped update excluded by --skip-grouped") {
		t.Fatalf("plan output missing skip-grouped exclusion reason: %q", stdout)
	}
}

func TestPlanCommandLimitAppliesAfterFilteringNotBeforeIt(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{
		pullRequests: []githubcli.PullRequest{
			{
				Number:      1,
				Title:       "Bump left-pad from 1.0.0 to 1.0.1",
				URL:         "https://example.test/pr/1",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				HeadRefName: "dependabot/npm_and_yarn/left-pad-1.0.1",
				BaseRefName: "main",
				IsDraft:     true,
			},
			{
				Number:      2,
				Title:       "Bump right-pad from 1.0.0 to 1.0.1",
				URL:         "https://example.test/pr/2",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				HeadRefName: "dependabot/npm_and_yarn/right-pad-1.0.1",
				BaseRefName: "main",
				IsDraft:     true,
			},
			{
				Number:      3,
				Title:       "Bump lodash from 4.17.20 to 4.17.21",
				URL:         "https://example.test/pr/3",
				Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
				HeadRefName: "dependabot/npm_and_yarn/lodash-4.17.21",
				BaseRefName: "main",
			},
		},
	}

	stdout, err := executeTestCommand(t, lister, "--limit", "1", "plan")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(stdout, "Planned order for 1 Dependabot pull request(s)") {
		t.Fatalf("plan output should still find the eligible PR beyond the two drafts: %q", stdout)
	}
	if !strings.Contains(stdout, "#3 [patch] Bump lodash from 4.17.20 to 4.17.21") {
		t.Fatalf("plan output missing the eligible PR #3, --limit should apply after filtering: %q", stdout)
	}
}

func TestPlanCommandInvalidChangeKindReturnsError(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{}

	_, err := executeTestCommand(t, lister, "plan", "--change-kind=bogus")
	if err == nil {
		t.Fatal("expected error for invalid --change-kind value")
	}
	if !strings.Contains(err.Error(), `invalid --change-kind value "bogus"`) {
		t.Fatalf("error = %v, want invalid change-kind message", err)
	}
}

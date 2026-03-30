package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/executor"
	"github.com/JakeTRogers/depflow/internal/githubcli"
	"github.com/JakeTRogers/depflow/internal/planner"
)

func TestSanitizeStripsTerminalControlBytes(t *testing.T) {
	t.Parallel()

	input := "safe\x00\x1b]8;;https://example.test\x07link\x1b]8;;\x07\tline\nnext\rend\x7f"
	got := sanitize(input)
	want := "safe]8;;https://example.testlink]8;;\tline\nnext\rend"
	if got != want {
		t.Fatalf("sanitize() = %q, want %q", got, want)
	}
}

func TestWriteScannedPRSanitizesGitHubFields(t *testing.T) {
	t.Parallel()

	pr := dependabot.PR{
		Number:  42,
		Title:   "Bump \x1b[31mlodash\x07",
		URL:     "https://example.test/pr/42\x1b]8;;evil\x07",
		Author:  "dependabot[bot]\x1b[0m",
		BaseRef: "main\x7f",
		HeadRef: "dependabot/npm_and_yarn/lodash-4.17.21\x1b[31m",
		Labels:  []string{"dependencies\x1b[0m", "security\x07"},
		Classification: dependabot.Classification{
			DependencyName:          "lodash\x1b[0m",
			Ecosystem:               "npm-and-yarn\x07",
			ChangeKind:              dependabot.ChangePatch,
			DeveloperTooling:        true,
			InfrastructureSensitive: false,
		},
	}

	var output bytes.Buffer
	if err := writeScannedPR(&output, pr); err != nil {
		t.Fatalf("writeScannedPR() error = %v", err)
	}

	got := output.String()
	if strings.ContainsAny(got, "\x00\x07\x1b\x7f") {
		t.Fatalf("scan output contains terminal control bytes: %q", got)
	}
	if !strings.Contains(got, "#42 Bump [31mlodash") {
		t.Fatalf("scan output = %q, want sanitized title", got)
	}
	if !strings.Contains(got, "dependency: lodash[0m") {
		t.Fatalf("scan output = %q, want sanitized dependency", got)
	}
	if !strings.Contains(got, "labels: dependencies[0m, security") {
		t.Fatalf("scan output = %q, want sanitized labels", got)
	}
	if !strings.Contains(got, "url: https://example.test/pr/42]8;;evil") {
		t.Fatalf("scan output = %q, want sanitized url", got)
	}
}

func TestWritePlannedPRSanitizesGitHubFields(t *testing.T) {
	t.Parallel()

	item := planner.PlannedPR{
		PR: dependabot.PR{
			Number: 7,
			Title:  "Bump \x1b[31mgithub.com/pkg/errors\x07",
			URL:    "https://example.test/pr/7\x1b]8;;evil\x07",
			Classification: dependabot.Classification{
				DependencyName: "github.com/pkg/errors\x1b[0m",
				Ecosystem:      "go-modules\x07",
				ChangeKind:     dependabot.ChangeMinor,
			},
		},
		Bucket: planner.BucketMinor,
		Reason: "minor update from 0.8.1 to 0.9.1\x1b[0m",
	}

	var output bytes.Buffer
	if err := writePlannedPR(&output, 1, item); err != nil {
		t.Fatalf("writePlannedPR() error = %v", err)
	}

	got := output.String()
	if strings.ContainsAny(got, "\x00\x07\x1b\x7f") {
		t.Fatalf("plan output contains terminal control bytes: %q", got)
	}
	if !strings.Contains(got, "1. #7 [minor] Bump [31mgithub.com/pkg/errors") {
		t.Fatalf("plan output = %q, want sanitized title", got)
	}
	if !strings.Contains(got, "dependency: github.com/pkg/errors[0m") {
		t.Fatalf("plan output = %q, want sanitized dependency", got)
	}
	if !strings.Contains(got, "reason: minor update from 0.8.1 to 0.9.1[0m") {
		t.Fatalf("plan output = %q, want sanitized reason", got)
	}
	if !strings.Contains(got, "url: https://example.test/pr/7]8;;evil") {
		t.Fatalf("plan output = %q, want sanitized url", got)
	}
}

func TestWriteExcludedMajorUpdatesSanitizesTitles(t *testing.T) {
	t.Parallel()

	excluded := []dependabot.PR{{
		Number: 8,
		Title:  "Bump \x1b[31mnext\x07 from 14.2.0 to 15.0.0",
	}}

	var output bytes.Buffer
	if err := writeExcludedMajorUpdates(&output, "Excluded major updates", excluded); err != nil {
		t.Fatalf("writeExcludedMajorUpdates() error = %v", err)
	}

	got := output.String()
	if strings.ContainsAny(got, "\x00\x07\x1b\x7f") {
		t.Fatalf("excluded output contains terminal control bytes: %q", got)
	}
	if !strings.Contains(got, "Excluded major updates (1):") {
		t.Fatalf("excluded output = %q, want heading", got)
	}
	if !strings.Contains(got, "#8 Bump [31mnext from 14.2.0 to 15.0.0") {
		t.Fatalf("excluded output = %q, want sanitized title", got)
	}
}

func TestPrintDryRunAndResultSanitizeTitlesAndErrors(t *testing.T) {
	t.Parallel()

	plan := planner.Plan{Items: []planner.PlannedPR{{
		PR: dependabot.PR{
			Number: 5,
			Title:  "Bump \x1b[31mactions/cache\x07",
		},
		Bucket: planner.BucketCI,
		Reason: "GitHub Actions ecosystem update sorts first\x1b[0m",
	}}}

	var dryRunOutput bytes.Buffer
	if err := printDryRun(&dryRunOutput, plan); err != nil {
		t.Fatalf("printDryRun() error = %v", err)
	}
	if strings.ContainsAny(dryRunOutput.String(), "\x00\x07\x1b\x7f") {
		t.Fatalf("dry-run output contains terminal control bytes: %q", dryRunOutput.String())
	}

	result := &executor.Result{Processed: []executor.PRResult{{
		Item:  planner.PlannedPR{PR: dependabot.PR{Number: 5, Title: "Bump \x1b[31mactions/cache\x07"}},
		Error: errors.New("merge failed at https://example.test/pr/5\x1b]8;;evil\x07"),
	}}}

	var resultOutput bytes.Buffer
	if err := printResult(&resultOutput, result); err != nil {
		t.Fatalf("printResult() error = %v", err)
	}

	got := resultOutput.String()
	if strings.ContainsAny(got, "\x00\x07\x1b\x7f") {
		t.Fatalf("result output contains terminal control bytes: %q", got)
	}
	if !strings.Contains(got, "#5 Bump [31mactions/cache") {
		t.Fatalf("result output = %q, want sanitized title", got)
	}
	if !strings.Contains(got, "merge failed at https://example.test/pr/5]8;;evil") {
		t.Fatalf("result output = %q, want sanitized error", got)
	}
}

func TestListOpenPullRequestsForDiscoveryCapsGrowth(t *testing.T) {
	t.Parallel()

	pullRequests := make([]githubcli.PullRequest, maxDiscoveryPullRequestLimit*2)
	lister := &fakeLister{pullRequests: pullRequests, respectLimit: true}
	opts := &commandOptions{limit: 1}

	prs, err := listOpenPullRequestsForDiscovery(t.Context(), commandDeps{lister: lister}, opts)
	if err != nil {
		t.Fatalf("listOpenPullRequestsForDiscovery() error = %v", err)
	}
	if len(prs) != maxDiscoveryPullRequestLimit {
		t.Fatalf("len(prs) = %d, want %d", len(prs), maxDiscoveryPullRequestLimit)
	}
	if len(lister.limits) == 0 {
		t.Fatal("ListOpenPullRequests() was not called")
	}
	if got := lister.limits[len(lister.limits)-1]; got != maxDiscoveryPullRequestLimit {
		t.Fatalf("final discovery limit = %d, want %d", got, maxDiscoveryPullRequestLimit)
	}
	for _, limit := range lister.limits {
		if limit > maxDiscoveryPullRequestLimit {
			t.Fatalf("discovery requested limit %d above cap %d", limit, maxDiscoveryPullRequestLimit)
		}
	}
}

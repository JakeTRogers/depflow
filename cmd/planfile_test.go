package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/githubcli"
)

type fakeEditor struct {
	edit  func(path string) error
	calls int
	paths []string
}

func (f *fakeEditor) Edit(_ context.Context, path string) error {
	f.calls++
	f.paths = append(f.paths, path)
	if f.edit == nil {
		return nil
	}
	return f.edit(path)
}

func dependabotPullRequest(number int, title, headRef string, draft bool) githubcli.PullRequest {
	return githubcli.PullRequest{
		Number:      number,
		Title:       title,
		URL:         "https://example.test/pr/" + title,
		Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
		Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
		HeadRefName: headRef,
		BaseRefName: "main",
		IsDraft:     draft,
	}
}

// planFileFixture returns PRs whose default plan is #13 [ci], #10 [patch], #11 [minor], with
// #12 (major) and #14 (draft) held back by the default filters.
func planFileFixture() *fakeLister {
	return &fakeLister{pullRequests: []githubcli.PullRequest{
		dependabotPullRequest(10, "Bump lodash from 4.17.20 to 4.17.21", "dependabot/npm_and_yarn/lodash-4.17.21", false),
		dependabotPullRequest(11, "Bump axios from 1.6.0 to 1.7.0", "dependabot/npm_and_yarn/axios-1.7.0", false),
		dependabotPullRequest(12, "Bump react from 17.0.2 to 18.0.0", "dependabot/npm_and_yarn/react-18.0.0", false),
		dependabotPullRequest(13, "Bump actions/checkout from 4.1.0 to 4.1.1", "dependabot/github_actions/actions/checkout-4.1.1", false),
		dependabotPullRequest(14, "Bump express from 4.18.1 to 4.18.2", "dependabot/npm_and_yarn/express-4.18.2", true),
	}}
}

type commandRun struct {
	stdout string
	stderr string
	err    error
}

func runWithDeps(t *testing.T, deps commandDeps, stdin string, args ...string) commandRun {
	t.Helper()

	cmd := newRootCommand(deps)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return commandRun{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func writeTestPlan(t *testing.T, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "plan.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func assertOperatorUnused(t *testing.T, operator *fakeExecuteOperator) {
	t.Helper()

	if len(operator.viewedRepos) != 0 || len(operator.approvedRepos) != 0 || len(operator.mergedRepos) != 0 {
		t.Fatalf("operator was called: viewed=%v approved=%v merged=%v", operator.viewedRepos, operator.approvedRepos, operator.mergedRepos)
	}
}

func TestPlanOutputStdoutRendersPicksAndDefaultExclusions(t *testing.T) {
	t.Parallel()

	run := runWithDeps(t, commandDeps{lister: planFileFixture()}, "", "--repo", "owner/repo", "plan", "-o", "-")
	if run.err != nil {
		t.Fatalf("Execute() error = %v", run.err)
	}

	want := []string{
		"repo owner/repo\n",
		"\npick #13 [ci] Bump actions/checkout from 4.1.0 to 4.1.1\npick #10 [patch] Bump lodash from 4.17.20 to 4.17.21\npick #11 [minor] Bump axios from 1.6.0 to 1.7.0\n",
		"skip #14 [patch] Bump express from 4.18.1 to 4.18.2  # draft PR (use --include-drafts to include)\n",
		"skip #12 [major] Bump react from 17.0.2 to 18.0.0  # change-kind \"major\" not in --change-kind allow-list\n",
	}
	for _, fragment := range want {
		if !strings.Contains(run.stdout, fragment) {
			t.Fatalf("plan -o - output missing %q:\n%s", fragment, run.stdout)
		}
	}
	if strings.Contains(run.stdout, "Planned order") {
		t.Fatalf("plan -o - should write only the plan file:\n%s", run.stdout)
	}
}

func TestPlanOutputOmitsPRsExcludedByTypedFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		absent  []string
		present []string
	}{
		{
			name:    "typed change-kind",
			args:    []string{"--change-kind", "patch"},
			absent:  []string{"#11", "#12"},
			present: []string{"pick #10", "pick #13", "skip #14"},
		},
		{
			name:    "typed include-drafts",
			args:    []string{"--include-drafts"},
			absent:  []string{"skip #14"},
			present: []string{"pick #14", "skip #12"},
		},
		{
			name:    "typed ecosystem exclusion",
			args:    []string{"--exclude-ecosystem", "npm-and-yarn"},
			absent:  []string{"#10", "#11", "#12", "#14"},
			present: []string{"pick #13"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			args := append([]string{"--repo", "owner/repo", "plan", "-o", "-"}, test.args...)
			run := runWithDeps(t, commandDeps{lister: planFileFixture()}, "", args...)
			if run.err != nil {
				t.Fatalf("Execute() error = %v", run.err)
			}
			for _, fragment := range test.absent {
				if strings.Contains(run.stdout, fragment) {
					t.Fatalf("output should not contain %q:\n%s", fragment, run.stdout)
				}
			}
			for _, fragment := range test.present {
				if !strings.Contains(run.stdout, fragment) {
					t.Fatalf("output missing %q:\n%s", fragment, run.stdout)
				}
			}
		})
	}
}

func TestPlanOutputFileWritesPlanAndHint(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "plan.txt")
	run := runWithDeps(t, commandDeps{lister: planFileFixture()}, "", "--repo", "owner/repo", "plan", "-o", path)
	if run.err != nil {
		t.Fatalf("Execute() error = %v", run.err)
	}

	wantStdout := "Wrote plan for 3 PR(s) to " + path + " (2 more listed as skip).\nEdit it, then run: depflow --repo owner/repo execute --plan " + path + "\n"
	if run.stdout != wantStdout {
		t.Fatalf("stdout = %q, want %q", run.stdout, wantStdout)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.HasPrefix(string(content), "repo owner/repo\n") || !strings.Contains(string(content), "pick #13 [ci]") {
		t.Fatalf("plan file content unexpected:\n%s", content)
	}
}

func TestPlanOutputResolvesRepoWhenNotGiven(t *testing.T) {
	t.Parallel()

	resolver := &fakeRepoResolver{repo: "owner/inferred"}
	run := runWithDeps(t, commandDeps{lister: planFileFixture(), resolver: resolver}, "", "plan", "-o", "-")
	if run.err != nil {
		t.Fatalf("Execute() error = %v", run.err)
	}
	if !strings.HasPrefix(run.stdout, "repo owner/inferred\n") {
		t.Fatalf("stdout = %q, want resolved repo line", run.stdout)
	}
	if resolver.calls != 1 {
		t.Fatalf("ResolveRepo() calls = %d, want 1", resolver.calls)
	}
}

func TestPlanOutputReportsEmptyResultsOnStderr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		lister *fakeLister
		args   []string
		want   string
	}{
		{name: "no PRs", lister: &fakeLister{}, want: noOpenDependabotPRsMessage},
		{name: "all filtered", lister: planFileFixture(), args: []string{"--ecosystem", "docker"}, want: noEligiblePRsMessage},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			args := append([]string{"--repo", "owner/repo", "plan", "-o", "-"}, test.args...)
			run := runWithDeps(t, commandDeps{lister: test.lister}, "", args...)
			if run.err != nil {
				t.Fatalf("Execute() error = %v", run.err)
			}
			if run.stdout != "" {
				t.Fatalf("stdout = %q, want empty", run.stdout)
			}
			if !strings.Contains(run.stderr, test.want) {
				t.Fatalf("stderr = %q, want %q", run.stderr, test.want)
			}
		})
	}
}

func TestPlanOutputFileRefusesToOverwrite(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "plan.txt")
	const edited = "repo owner/repo\npick #10 [patch] hand-edited\n"
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	run := runWithDeps(t, commandDeps{lister: planFileFixture()}, "", "--repo", "owner/repo", "plan", "-o", path)
	if run.err == nil || !strings.Contains(run.err.Error(), "already exists; pass --force to overwrite it") {
		t.Fatalf("error = %v, want already exists error", run.err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != edited {
		t.Fatalf("existing plan changed: %q, %v", content, err)
	}

	run = runWithDeps(t, commandDeps{lister: planFileFixture()}, "", "--repo", "owner/repo", "plan", "-o", path, "--force")
	if run.err != nil {
		t.Fatalf("Execute() with --force error = %v", run.err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(content), "hand-edited") || !strings.Contains(string(content), "pick #13 [ci]") {
		t.Fatalf("--force did not replace the plan:\n%s", content)
	}
}

func TestPlanForceRequiresOutputFile(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"plan", "--force"}, {"plan", "-o", "-", "--force"}} {
		run := runWithDeps(t, commandDeps{lister: planFileFixture()}, "", append([]string{"--repo", "owner/repo"}, args...)...)
		if run.err == nil || !strings.Contains(run.err.Error(), "--force only applies") {
			t.Fatalf("%v: error = %v, want --force usage error", args, run.err)
		}
	}
}

func TestPlanOutputFileCreateError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing-dir", "plan.txt")
	run := runWithDeps(t, commandDeps{lister: planFileFixture()}, "", "--repo", "owner/repo", "plan", "-o", path)
	if run.err == nil || !strings.Contains(run.err.Error(), "creating plan file") {
		t.Fatalf("error = %v, want creating plan file error", run.err)
	}
}

func TestExecutePlanFileDryRunFollowsFileOrder(t *testing.T) {
	t.Parallel()

	path := writeTestPlan(t,
		"repo owner/repo",
		"# comment",
		"pick #12 [major] react",
		"skip #13 [ci]",
		"pick #11 [minor]",
		"p 10",
	)
	operator := &fakeExecuteOperator{}
	run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: operator}, "", "--repo", "owner/repo", "execute", "--plan", path, "--dry-run")
	if run.err != nil {
		t.Fatalf("Execute() error = %v", run.err)
	}

	wantOrder := "Dry run: 3 PR(s) would be processed in this order:\n\n" +
		"1. #12 [major] Bump react from 17.0.2 to 18.0.0\n"
	if !strings.Contains(run.stdout, wantOrder) {
		t.Fatalf("stdout missing %q:\n%s", wantOrder, run.stdout)
	}
	if !strings.Contains(run.stdout, "2. #11 [minor]") || !strings.Contains(run.stdout, "3. #10 [patch]") {
		t.Fatalf("stdout has wrong order:\n%s", run.stdout)
	}
	if strings.Contains(run.stdout, "reason:") {
		t.Fatalf("plan file dry run should not print planner reasons:\n%s", run.stdout)
	}
	if !strings.Contains(run.stdout, "Left alone: 1 open Dependabot PR(s) not listed in the plan file") {
		t.Fatalf("stdout missing unlisted notice:\n%s", run.stdout)
	}
	assertOperatorUnused(t, operator)
}

func TestExecutePlanFileReportsDroppedAndDriftedPRs(t *testing.T) {
	t.Parallel()

	path := writeTestPlan(t,
		"repo OWNER/Repo",
		"pick #99 [patch] merged since planning",
		"pick #11 [patch] was patch when planned",
		"pick #10 [patch]",
		"skip #12",
		"skip #13",
		"skip #14",
	)
	run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: &fakeExecuteOperator{}}, "", "--repo", "github.com/owner/repo", "execute", "--plan", path, "--dry-run")
	if run.err != nil {
		t.Fatalf("Execute() error = %v", run.err)
	}

	for _, fragment := range []string{
		"Not processed (not an open Dependabot PR): #99\n",
		"Warning: #11 is now [minor], was [patch] when planned\n",
		"Dry run: 2 PR(s) would be processed in this order:",
	} {
		if !strings.Contains(run.stdout, fragment) {
			t.Fatalf("stdout missing %q:\n%s", fragment, run.stdout)
		}
	}
	if strings.Contains(run.stdout, "Left alone") {
		t.Fatalf("stdout should not report unlisted PRs when every PR is listed:\n%s", run.stdout)
	}
}

func TestExecutePlanFileBlocksPicksThatBecameMajor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		lines      []string
		wantStdout []string
		wantAbsent []string
	}{
		{
			name:  "drift into major is not processed",
			lines: []string{"repo owner/repo", "pick #12 [minor] react", "pick #10 [patch] lodash"},
			wantStdout: []string{
				"Not processed: #12 is now [major], was [minor] when planned; change its bucket to [major] in the plan file to include it\n",
				"Dry run: 1 PR(s) would be processed in this order:",
				"1. #10 [patch]",
			},
			wantAbsent: []string{"1. #12", "2. #12", "Warning: #12"},
		},
		{
			name:       "recording the major bucket re-confirms the pick",
			lines:      []string{"repo owner/repo", "pick #12 [Major] react", "pick #10 [patch] lodash"},
			wantStdout: []string{"Dry run: 2 PR(s) would be processed in this order:", "1. #12 [major]"},
			wantAbsent: []string{"Not processed", "Warning"},
		},
		{
			name:       "every pick blocked",
			lines:      []string{"repo owner/repo", "pick #12 [patch] react"},
			wantStdout: []string{"Not processed: #12 is now [major]", noOpenPickedPRsMessage},
			wantAbsent: []string{"Dry run:"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			operator := &fakeExecuteOperator{}
			path := writeTestPlan(t, test.lines...)
			run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: operator}, "", "--repo", "owner/repo", "execute", "--plan", path, "--dry-run")
			if run.err != nil {
				t.Fatalf("Execute() error = %v", run.err)
			}
			for _, fragment := range test.wantStdout {
				if !strings.Contains(run.stdout, fragment) {
					t.Fatalf("stdout missing %q:\n%s", fragment, run.stdout)
				}
			}
			for _, fragment := range test.wantAbsent {
				if strings.Contains(run.stdout, fragment) {
					t.Fatalf("stdout should not contain %q:\n%s", fragment, run.stdout)
				}
			}
			if len(operator.approvedRepos) != 0 || len(operator.mergedRepos) != 0 {
				t.Fatalf("dry run approved %v, merged %v", operator.approvedRepos, operator.mergedRepos)
			}
		})
	}
}

func TestExecutePlanFileNothingToDo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{name: "no pick lines", lines: []string{"repo owner/repo", "skip #10"}, want: noPickLinesMessage},
		{name: "no open picks", lines: []string{"repo owner/repo", "pick #98", "pick #99"}, want: noOpenPickedPRsMessage},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			operator := &fakeExecuteOperator{}
			resolver := &fakeRepoResolver{repo: "owner/repo"}
			path := writeTestPlan(t, test.lines...)
			run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: operator, resolver: resolver}, "", "execute", "--plan", path)
			if run.err != nil {
				t.Fatalf("Execute() error = %v", run.err)
			}
			if !strings.Contains(run.stdout, test.want) {
				t.Fatalf("stdout = %q, want %q", run.stdout, test.want)
			}
			assertOperatorUnused(t, operator)
		})
	}
}

func TestExecutePlanFileExecutesPickedPR(t *testing.T) {
	t.Parallel()

	operator := &fakeExecuteOperator{}
	resolver := &fakeRepoResolver{repo: "owner/repo"}
	path := writeTestPlan(t, "repo owner/repo", "pick #12")
	run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: operator, resolver: resolver}, "", "execute", "--plan", path)
	if run.err != nil {
		t.Fatalf("Execute() error = %v", run.err)
	}

	if !strings.Contains(run.stdout, "Processing 1 PR(s) in this order:\n1. #12 [major] Bump react from 17.0.2 to 18.0.0\n") {
		t.Fatalf("stdout missing execution order:\n%s", run.stdout)
	}
	if !strings.Contains(run.stdout, "Merged: 1") {
		t.Fatalf("stdout missing merge summary:\n%s", run.stdout)
	}
	if resolver.calls != 1 {
		t.Fatalf("ResolveRepo() calls = %d, want 1", resolver.calls)
	}
	assertAllReposEqual(t, operator.mergedRepos, "owner/repo")
}

func TestExecutePlanFileReadsStdin(t *testing.T) {
	t.Parallel()

	run := runWithDeps(t, commandDeps{lister: planFileFixture()}, "repo owner/repo\npick #13\n", "--repo", "owner/repo", "execute", "--plan", "-", "--dry-run")
	if run.err != nil {
		t.Fatalf("Execute() error = %v", run.err)
	}
	if !strings.Contains(run.stdout, "1. #13 [ci]") {
		t.Fatalf("stdout = %q, want #13 planned", run.stdout)
	}
}

func TestExecutePlanFileErrors(t *testing.T) {
	t.Parallel()

	validPlan := writeTestPlan(t, "repo owner/repo", "pick #10")
	tests := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{
			name: "repo mismatch",
			args: []string{"--repo", "other/repo", "execute", "--plan", validPlan},
			want: "plan file is for owner/repo but the target repository is other/repo",
		},
		{
			name: "filter flag",
			args: []string{"--repo", "owner/repo", "--ecosystem", "npm-and-yarn", "execute", "--plan", validPlan},
			want: "flag --ecosystem cannot be used with --plan",
		},
		{
			name: "change-kind flag",
			args: []string{"--repo", "owner/repo", "execute", "--plan", validPlan, "--change-kind", "all"},
			want: "flag --change-kind cannot be used with --plan",
		},
		{
			name: "edit and plan",
			args: []string{"--repo", "owner/repo", "execute", "--plan", validPlan, "--edit"},
			want: "none of the others can be",
		},
		{
			name: "missing file",
			args: []string{"--repo", "owner/repo", "execute", "--plan", filepath.Join(t.TempDir(), "nope.txt")},
			want: "reading plan file",
		},
		{
			name:  "invalid stdin",
			stdin: "repo owner/repo\nsquash #1\n",
			args:  []string{"--repo", "owner/repo", "execute", "--plan", "-"},
			want:  `reading plan from stdin: line 2: unknown command "squash"`,
		},
		{
			name: "repo resolution failure",
			args: []string{"execute", "--plan", validPlan},
			want: "resolving current repository",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			operator := &fakeExecuteOperator{}
			run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: operator}, test.stdin, test.args...)
			if run.err == nil || !strings.Contains(run.err.Error(), test.want) {
				t.Fatalf("error = %v, want it to contain %q", run.err, test.want)
			}
			assertOperatorUnused(t, operator)
		})
	}
}

func TestExecuteEditReordersAndIncludesSkippedPRs(t *testing.T) {
	t.Parallel()

	var rendered string
	editor := &fakeEditor{edit: func(path string) error {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rendered = string(content)
		return os.WriteFile(path, []byte("repo owner/repo\npick #12\nskip #13\npick #10\n"), 0o600)
	}}
	operator := &fakeExecuteOperator{}
	run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: operator, editor: editor}, "", "--repo", "owner/repo", "execute", "--edit", "--dry-run")
	if run.err != nil {
		t.Fatalf("Execute() error = %v", run.err)
	}

	if !strings.Contains(rendered, "skip #12 [major]") || !strings.Contains(rendered, "pick #13 [ci]") {
		t.Fatalf("editor received unexpected plan:\n%s", rendered)
	}
	if got := dryRunNumbers(run.stdout); !reflect.DeepEqual(got, []string{"#12", "#10"}) {
		t.Fatalf("dry-run order = %v, want [#12 #10]\n%s", got, run.stdout)
	}
	if strings.Contains(run.stdout, "Left alone") {
		t.Fatalf("--edit should not report unlisted PRs:\n%s", run.stdout)
	}
	if _, err := os.Stat(editor.paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp plan file should be removed, stat error = %v", err)
	}
	assertOperatorUnused(t, operator)
}

func TestExecuteEditKeepsInvalidPlanForRerun(t *testing.T) {
	t.Parallel()

	editor := &fakeEditor{edit: func(path string) error {
		return os.WriteFile(path, []byte("repo owner/repo\nsquash #10\n"), 0o600)
	}}
	run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: &fakeExecuteOperator{}, editor: editor}, "", "--repo", "owner/repo", "execute", "--edit")
	if run.err == nil {
		t.Fatal("Execute() error = nil, want parse error")
	}
	path := editor.paths[0]
	t.Cleanup(func() { _ = os.Remove(path) })

	want := `edited plan: line 2: unknown command "squash" (want pick or skip) (kept at ` + path + `; fix it and rerun with --plan ` + path + `)`
	if run.err.Error() != want {
		t.Fatalf("error = %q, want %q", run.err, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("invalid plan should be kept, stat error = %v", err)
	}
}

func TestExecuteEditAbortsWhenEditorFails(t *testing.T) {
	t.Parallel()

	editor := &fakeEditor{edit: func(string) error { return errors.New("exit status 1") }}
	operator := &fakeExecuteOperator{}
	run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: operator, editor: editor}, "", "--repo", "owner/repo", "execute", "--edit")
	if run.err == nil || run.err.Error() != "editing plan: exit status 1" {
		t.Fatalf("error = %v, want editing plan error", run.err)
	}
	if _, err := os.Stat(editor.paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp plan file should be removed, stat error = %v", err)
	}
	assertOperatorUnused(t, operator)
}

func TestExecuteEditSkipsEditorWhenNothingToEdit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		lister *fakeLister
		args   []string
		want   string
	}{
		{name: "no PRs", lister: &fakeLister{}, want: noOpenDependabotPRsMessage},
		{name: "all filtered", lister: planFileFixture(), args: []string{"--ecosystem", "docker"}, want: noEligiblePRsMessage},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			editor := &fakeEditor{}
			args := append([]string{"--repo", "owner/repo", "execute", "--edit"}, test.args...)
			run := runWithDeps(t, commandDeps{lister: test.lister, editor: editor}, "", args...)
			if run.err != nil {
				t.Fatalf("Execute() error = %v", run.err)
			}
			if !strings.Contains(run.stdout, test.want) {
				t.Fatalf("stdout = %q, want %q", run.stdout, test.want)
			}
			if editor.calls != 0 {
				t.Fatalf("editor calls = %d, want 0", editor.calls)
			}
		})
	}
}

func TestExecuteEditWithoutEditorFails(t *testing.T) {
	t.Parallel()

	run := runWithDeps(t, commandDeps{lister: planFileFixture()}, "", "--repo", "owner/repo", "execute", "--edit")
	if run.err == nil || !strings.Contains(run.err.Error(), "no editor configured") {
		t.Fatalf("error = %v, want no editor configured", run.err)
	}
}

func TestExecuteEditNoPicksIsNoOp(t *testing.T) {
	t.Parallel()

	editor := &fakeEditor{edit: func(path string) error {
		return os.WriteFile(path, []byte("repo owner/repo\n"), 0o600)
	}}
	operator := &fakeExecuteOperator{}
	run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: operator, editor: editor}, "", "--repo", "owner/repo", "execute", "--edit")
	if run.err != nil {
		t.Fatalf("Execute() error = %v", run.err)
	}
	if !strings.Contains(run.stdout, noPickLinesMessage) {
		t.Fatalf("stdout = %q, want %q", run.stdout, noPickLinesMessage)
	}
	assertOperatorUnused(t, operator)
}

func TestSameRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		left, right string
		want        bool
	}{
		{left: "owner/repo", right: "owner/repo", want: true},
		{left: "Owner/Repo", right: "owner/repo", want: true},
		{left: "github.com/owner/repo", right: "owner/repo", want: true},
		{left: "github.com/owner/repo", right: "GITHUB.COM/owner/repo/", want: true},
		{left: "github.com/owner/repo", right: "ghe.example.com/owner/repo", want: false},
		{left: "owner/repo", right: "owner/other", want: false},
		{left: "repo", right: "owner/repo", want: false},
		{left: "", right: "", want: false},
	}

	for _, test := range tests {
		if got := sameRepo(test.left, test.right); got != test.want {
			t.Errorf("sameRepo(%q, %q) = %v, want %v", test.left, test.right, got, test.want)
		}
	}
}

func TestEditorCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		goos string
		want []string
	}{
		{name: "visual wins", env: map[string]string{"VISUAL": "code --wait", "EDITOR": "nano"}, goos: "linux", want: []string{"code", "--wait"}},
		{name: "editor fallback", env: map[string]string{"VISUAL": "  ", "EDITOR": "nano"}, goos: "linux", want: []string{"nano"}},
		{name: "unix default", goos: "darwin", want: []string{"vi"}},
		{name: "windows default", goos: "windows", want: []string{"notepad"}},
	}

	for _, test := range tests {
		getenv := func(name string) string { return test.env[name] }
		if got := editorCommand(getenv, test.goos); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s: editorCommand() = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestTerminalEditorRequiresTerminal(t *testing.T) {
	t.Parallel()

	notTerminal, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	t.Cleanup(func() {
		if err := notTerminal.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	for _, stdin := range []*os.File{notTerminal, nil} {
		editor := terminalEditor{getenv: func(string) string { return "" }, stdin: stdin}
		err := editor.Edit(context.Background(), "plan.txt")
		if err == nil || !strings.Contains(err.Error(), "--edit needs an interactive terminal") {
			t.Fatalf("Edit() error = %v, want terminal error", err)
		}
	}
}

func TestTerminalEditorRunsEditorOnPath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("uses /dev/null and a shell script")
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("Open(%s) error = %v", os.DevNull, err)
	}
	t.Cleanup(func() {
		if err := devNull.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	dir := t.TempDir()
	script := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"$1 $2\" > \"$2\"\n"), 0o700); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	target := filepath.Join(dir, "plan.txt")

	editor := terminalEditor{
		getenv: func(name string) string {
			if name == "EDITOR" {
				return script + " --flag"
			}
			return ""
		},
		goos:   runtime.GOOS,
		stdin:  devNull,
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}
	if err := editor.Edit(context.Background(), target); err != nil {
		t.Fatalf("Edit() error = %v", err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.TrimSpace(string(content)) != "--flag "+target {
		t.Fatalf("editor args = %q, want flag then path", content)
	}

	failing := editor
	failing.getenv = func(string) string { return filepath.Join(dir, "missing-editor") }
	if err := failing.Edit(context.Background(), target); err == nil || !strings.Contains(err.Error(), "running editor") {
		t.Fatalf("Edit() error = %v, want running editor error", err)
	}
}

func dryRunNumbers(stdout string) []string {
	var numbers []string
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasSuffix(fields[0], ".") && strings.HasPrefix(fields[1], "#") {
			numbers = append(numbers, fields[1])
		}
	}
	return numbers
}

func TestLimitKeepsFirstPRsInPlanOrder(t *testing.T) {
	t.Parallel()

	// planFileFixture plans #13 [ci], #10 [patch], #11 [minor]; the lowest-numbered PRs (#10, #11)
	// are not the first ones in plan order.
	tests := []struct {
		name       string
		args       []string
		wantStdout []string
		wantAbsent []string
	}{
		{
			name: "plan",
			args: []string{"--limit", "1", "plan"},
			wantStdout: []string{
				"Planned order for 1 Dependabot pull request(s)",
				"#13",
				"Not included: 2 more eligible PR(s) beyond --limit 1\n",
			},
			wantAbsent: []string{"#10", "#11"},
		},
		{
			name: "execute dry run",
			args: []string{"--limit", "2", "execute", "--dry-run"},
			wantStdout: []string{
				"Not included: 1 more eligible PR(s) beyond --limit 2\n\nDry run: 2 PR(s) would be processed in this order:",
				"1. #13 [ci]",
				"2. #10 [patch]",
			},
			wantAbsent: []string{"#11 [minor]"},
		},
		{
			name: "plan file lists cut PRs as skips",
			args: []string{"--limit", "1", "plan", "-o", "-"},
			wantStdout: []string{
				"pick #13 [ci]",
				"skip #10 [patch] Bump lodash from 4.17.20 to 4.17.21  # beyond --limit 1\nskip #11 [minor] Bump axios from 1.6.0 to 1.7.0  # beyond --limit 1\nskip #14 [patch]",
			},
			wantAbsent: []string{"pick #10", "pick #11"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			operator := &fakeExecuteOperator{}
			args := append([]string{"--repo", "owner/repo"}, test.args...)
			run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: operator}, "", args...)
			if run.err != nil {
				t.Fatalf("Execute() error = %v", run.err)
			}
			for _, fragment := range test.wantStdout {
				if !strings.Contains(run.stdout, fragment) {
					t.Fatalf("stdout missing %q:\n%s", fragment, run.stdout)
				}
			}
			for _, fragment := range test.wantAbsent {
				if strings.Contains(run.stdout, fragment) {
					t.Fatalf("stdout should not contain %q:\n%s", fragment, run.stdout)
				}
			}
		})
	}
}

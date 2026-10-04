package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/executor"
	"github.com/JakeTRogers/depflow/internal/githubcli"
	"github.com/JakeTRogers/depflow/internal/planfile"
	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/spf13/cobra"
)

func TestResumeItems(t *testing.T) {
	t.Parallel()

	plan := planner.Plan{}
	for _, number := range []int{1, 2, 3, 4} {
		plan.Items = append(plan.Items, planner.PlannedPR{PR: dependabot.PR{Number: number}})
	}
	item := func(number int) planner.PlannedPR { return plan.Items[number-1] }
	failure := errors.New("boom")

	tests := []struct {
		name             string
		result           *executor.Result
		wantResume       []int
		wantNotAttempted []int
	}{
		{
			name:             "failed PR is retried before the rest",
			result:           &executor.Result{Processed: []executor.PRResult{{Item: item(1), Merged: true}, {Item: item(2), Error: failure}}},
			wantResume:       []int{2, 3, 4},
			wantNotAttempted: []int{3, 4},
		},
		{
			name:             "PR merged before post-merge CI failed is not retried",
			result:           &executor.Result{Processed: []executor.PRResult{{Item: item(1), Merged: true, Error: failure}}},
			wantResume:       []int{2, 3, 4},
			wantNotAttempted: []int{2, 3, 4},
		},
		{
			name:             "skipped PR is not retried",
			result:           &executor.Result{Processed: []executor.PRResult{{Item: item(1)}, {Item: item(2), Error: failure}}},
			wantResume:       []int{2, 3, 4},
			wantNotAttempted: []int{3, 4},
		},
		{
			name:             "nothing processed",
			result:           nil,
			wantResume:       []int{1, 2, 3, 4},
			wantNotAttempted: []int{1, 2, 3, 4},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			resume, notAttempted := resumeItems(plan, test.result)
			var resumed []int
			for _, item := range resume {
				resumed = append(resumed, item.PR.Number)
			}
			if !reflect.DeepEqual(resumed, test.wantResume) {
				t.Fatalf("resume = %v, want %v", resumed, test.wantResume)
			}
			if !reflect.DeepEqual(notAttempted, test.wantNotAttempted) {
				t.Fatalf("notAttempted = %v, want %v", notAttempted, test.wantNotAttempted)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		goos  string
		want  string
	}{
		{input: "/tmp/depflow-resume-1.txt", goos: "linux", want: "/tmp/depflow-resume-1.txt"},
		{input: "owner/repo", goos: "linux", want: "owner/repo"},
		{input: "5s", goos: "linux", want: "5s"},
		{input: "/my plans/p.txt", goos: "linux", want: "'/my plans/p.txt'"},
		{input: "it's", goos: "linux", want: `'it'\''s'`},
		{goos: "linux", want: "''"},
		{input: "/my plans/it's.txt", goos: "darwin", want: `'/my plans/it'\''s.txt'`},
		{input: `C:\Temp\depflow-resume-1.txt`, goos: "windows", want: `'C:\Temp\depflow-resume-1.txt'`},
		{input: `C:\my plans\it's.txt`, goos: "windows", want: `'C:\my plans\it''s.txt'`},
		{input: `C:\$HOME\%TEMP%\a&b.txt`, goos: "windows", want: `'C:\$HOME\%TEMP%\a&b.txt'`},
		{input: "owner/repo", goos: "windows", want: "'owner/repo'"},
		{input: "5s", goos: "windows", want: "'5s'"},
		{goos: "windows", want: "''"},
	}
	for _, tc := range tests {
		if got := shellQuote(tc.input, tc.goos); got != tc.want {
			t.Errorf("shellQuote(%q, %q) = %q, want %q", tc.input, tc.goos, got, tc.want)
		}
	}
}

func TestResumeHint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goos   string
		path   string
		config string
		want   string
	}{
		{
			goos:   "linux",
			path:   "/my plans/it's.txt",
			config: "/config files/settings.yaml",
			want:   `To continue, run: depflow --repo=owner/repo --config='/config files/settings.yaml' --admin --show-timing=false execute --plan '/my plans/it'\''s.txt'`,
		},
		{
			goos:   "windows",
			path:   `C:\my plans\it's.txt`,
			config: `C:\config files\it's.yaml`,
			want:   `To continue, run in PowerShell: depflow --repo='owner/repo' --config='C:\config files\it''s.yaml' --admin --show-timing='false' execute --plan 'C:\my plans\it''s.txt'`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			cmd.Flags().String("repo", "", "")
			cmd.Flags().String("config", "", "")
			cmd.Flags().Bool("admin", false, "")
			cmd.Flags().Bool("show-timing", false, "")
			for name, value := range map[string]string{"repo": "owner/repo", "config": tc.config, "admin": "true", "show-timing": "false"} {
				if err := cmd.Flags().Set(name, value); err != nil {
					t.Fatalf("setting %s: %v", name, err)
				}
			}
			if got := resumeHint(cmd, tc.path, tc.goos); got != tc.want {
				t.Fatalf("resumeHint() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExecuteWritesResumePlanAfterFailure(t *testing.T) {
	t.Parallel()

	// planFileFixture plans #13, #10, #11; #13 fails CI, so #10 and #11 are never attempted.
	failing := &fakeExecuteOperator{viewResults: []githubcli.PRDetail{
		{Number: 13, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "branch", BaseRefName: "main"},
		{Number: 13, State: "OPEN", Mergeable: "MERGEABLE", HeadRefName: "branch", BaseRefName: "main", StatusCheckRollup: []githubcli.StatusCheck{{Name: "ci", Conclusion: "failure"}}},
	}}
	dir := filepath.Join(t.TempDir(), "my plans")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: failing, resumeDir: dir}, "",
		"--repo", "owner/repo", "execute", "--merge-method", "squash", "--show-timing", "--poll-interval", "5s")
	if !errors.Is(run.err, executor.ErrExecutionFailed) {
		t.Fatalf("error = %v, want ErrExecutionFailed", run.err)
	}

	hintPrefix := "To continue, run: depflow --repo=owner/repo --merge-method=squash --poll-interval=5s --show-timing execute --plan "
	if runtime.GOOS == "windows" {
		hintPrefix = "To continue, run in PowerShell: depflow --repo='owner/repo' --merge-method='squash' --poll-interval='5s' --show-timing execute --plan "
	}
	for _, fragment := range []string{
		"Not attempted: #10, #11\n",
		"Resume plan for 3 PR(s) written to ",
		"(#13 failed and is listed first; change it to skip to leave it out).\n",
		hintPrefix,
	} {
		if !strings.Contains(run.stdout, fragment) {
			t.Fatalf("stdout missing %q:\n%s", fragment, run.stdout)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("resume files = %v, error = %v, want one file", entries, err)
	}
	path := filepath.Join(dir, entries[0].Name())
	if !strings.Contains(run.stdout, hintPrefix+shellQuote(path, runtime.GOOS)+"\n") {
		t.Fatalf("resume hint missing quoted path %s:\n%s", path, run.stdout)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	parsed, err := planfile.Parse(file)
	if closeErr := file.Close(); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var picks []int
	for _, entry := range parsed.Picks() {
		picks = append(picks, entry.Number)
	}
	if parsed.Repo != "owner/repo" || !reflect.DeepEqual(picks, []int{13, 10, 11}) {
		t.Fatalf("resume plan repo = %q picks = %v, want owner/repo [13 10 11]", parsed.Repo, picks)
	}

	resumed := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: &fakeExecuteOperator{}}, "",
		"--repo", "owner/repo", "execute", "--plan", path, "--dry-run")
	if resumed.err != nil || !strings.Contains(resumed.stdout, "Dry run: 3 PR(s) would be processed in this order:") || !strings.Contains(resumed.stdout, "1. #13 [ci]") {
		t.Fatalf("resumed dry run = %v:\n%s", resumed.err, resumed.stdout)
	}
}

func TestExecuteWritesNoResumePlanAfterSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	run := runWithDeps(t, commandDeps{lister: planFileFixture(), operator: &fakeExecuteOperator{}, resumeDir: dir}, "",
		"--repo", "owner/repo", "--limit", "1", "execute")
	if run.err != nil {
		t.Fatalf("Execute() error = %v\n%s", run.err, run.stdout)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 || strings.Contains(run.stdout, "Resume plan") || strings.Contains(run.stdout, "Not attempted") {
		t.Fatalf("successful run left resume output: entries=%v err=%v\n%s", entries, err, run.stdout)
	}
}

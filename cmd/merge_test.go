// Merge preference tests exercise the same entry points used by shell commands.
package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/config"
	"github.com/JakeTRogers/depflow/internal/githubcli"
)

func TestExecuteMergePreferences(t *testing.T) {
	t.Setenv("DEPFLOW_MERGE_METHOD", "rebase")
	for _, test := range []struct {
		name, env, override, want, source string
		repoPreference                    bool
	}{
		{"global", "", "", "squash", "global preference", false},
		{"repository", "", "", "rebase", "repository preference", true},
		{"environment", "merge", "", "merge", "DEPFLOW_MERGE_METHOD", true},
		{"flag", "merge", "squash", "squash", "--merge-method", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.env == "" {
				if err := os.Unsetenv("DEPFLOW_MERGE_METHOD"); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("DEPFLOW_MERGE_METHOD", test.env)
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := config.Update(path, func(document *config.Document) error {
				if err := document.Set("", config.MergeMethod, "squash"); err != nil {
					return err
				}
				if test.repoPreference {
					return document.Set("git.example.com/acme/tool", config.MergeMethod, "rebase")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			for _, mode := range []string{"normal", "plan", "edit"} {
				t.Run(mode, func(t *testing.T) {
					operator := &fakeExecuteOperator{}
					lister := &fakeLister{pullRequests: []githubcli.PullRequest{dependabotPullRequest(42, "Bump foo from 1.0.0 to 1.0.1", "dependabot/go_modules/foo-1.0.1", false)}}
					args := []string{"--config", path, "--repo", "git.example.com/acme/tool", "execute"}
					if test.override != "" {
						args = append(args, "--merge-method", test.override)
					}
					if mode == "plan" {
						args = append(args, "--plan", writeTestPlan(t, "repo git.example.com/acme/tool", "pick #42"))
					}
					if mode == "edit" {
						args = append(args, "--edit")
					}
					run := runWithDeps(t, commandDeps{lister: lister, operator: operator, editor: &fakeEditor{}}, "", args...)
					if run.err != nil {
						t.Fatal(run.err)
					}
					if !reflect.DeepEqual(operator.mergedMethods, []string{test.want}) || !strings.Contains(run.stderr, test.source) {
						t.Fatalf("methods=%v, stderr=%s", operator.mergedMethods, run.stderr)
					}
				})
			}
		})
	}
}

func TestMergePreflightFailures(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Update(path, func(document *config.Document) error { return document.Set("", config.MergeMethod, "merge") }); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                     string
		caps                     *githubcli.MergeCapabilities
		capabilityErr, policyErr error
		want                     string
		dryOK                    bool
	}{
		{"disabled", &githubcli.MergeCapabilities{Repo: "github.com/owner/repo", Methods: []string{"squash"}}, nil, nil, "--merge-method squash", false},
		{"none", &githubcli.MergeCapabilities{Repo: "github.com/owner/repo"}, nil, nil, "methods: none", false},
		{"lookup", nil, errors.New("offline"), nil, "offline", true},
		{"queue", nil, nil, githubcli.ErrMergeQueueUnsupported, "queue-managed", false},
		{"policy lookup", nil, nil, errors.New("permission denied"), "permission denied", true},
		{"cancellation", nil, context.Canceled, nil, "context canceled", false},
	} {
		for _, dry := range []bool{false, true} {
			for _, mode := range []string{"normal", "plan", "edit"} {
				operator := &fakeExecuteOperator{capabilities: test.caps, capabilityErr: test.capabilityErr, policyErr: test.policyErr}
				lister := &fakeLister{pullRequests: []githubcli.PullRequest{dependabotPullRequest(42, "Bump foo from 1.0.0 to 1.0.1", "dependabot/go_modules/foo-1.0.1", false)}}
				args := []string{"--config", path, "--repo", "owner/repo", "execute"}
				if dry {
					args = append(args, "--dry-run")
				}
				if mode == "plan" {
					args = append(args, "--plan", writeTestPlan(t, "repo owner/repo", "pick #42"))
				}
				if mode == "edit" {
					args = append(args, "--edit")
				}
				run := runWithDeps(t, commandDeps{lister: lister, operator: operator, editor: &fakeEditor{}}, "", args...)
				if (run.err == nil) != (dry && test.dryOK) {
					t.Fatalf("%s dry=%v mode=%s: %v", test.name, dry, mode, run.err)
				}
				text := run.stderr
				if run.err != nil {
					text += run.err.Error()
				}
				if !strings.Contains(text, test.want) {
					t.Fatalf("%s: %s", test.name, text)
				}
				assertOperatorUnused(t, operator)
			}
		}
	}
}

type preflightOperator struct {
	fakeExecuteOperator
	policyErrors map[int]error
	checkedPRs   []int
}

func (operator *preflightOperator) CheckMergeAllowed(_ context.Context, _ string, number int, _ string) error {
	operator.checkedPRs = append(operator.checkedPRs, number)
	return operator.policyErrors[number]
}

func TestMergePreflightContinuesAfterUnverifiedDryRunPolicy(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Update(path, func(document *config.Document) error { return document.Set("", config.MergeMethod, "merge") }); err != nil {
		t.Fatal(err)
	}
	conflict := &githubcli.MethodNotAllowedError{
		Method:       "merge",
		Capabilities: githubcli.MergeCapabilities{Repo: "github.com/owner/repo", Methods: []string{"squash"}},
	}
	for _, test := range []struct {
		name               string
		policyErr, wantErr error
		warnings           int
	}{
		{"allowed", nil, nil, 1},
		{"unverified", errors.New("second lookup failed"), nil, 2},
		{"disabled", conflict, conflict, 1},
		{"queue", githubcli.ErrMergeQueueUnsupported, githubcli.ErrMergeQueueUnsupported, 1},
		{"cancellation", context.Canceled, context.Canceled, 1},
	} {
		for _, mode := range []string{"normal", "plan", "edit"} {
			t.Run(test.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				operator := &preflightOperator{policyErrors: map[int]error{
					42: errors.New("first lookup failed"),
					43: test.policyErr,
				}}
				lister := &fakeLister{pullRequests: []githubcli.PullRequest{
					dependabotPullRequest(42, "Bump foo from 1.0.0 to 1.0.1", "dependabot/go_modules/foo-1.0.1", false),
					dependabotPullRequest(43, "Bump foo from 1.0.0 to 1.0.1", "dependabot/go_modules/foo-1.0.1", false),
				}}
				args := []string{"--config", path, "--repo", "owner/repo", "execute", "--dry-run", "--merge-method", "merge"}
				if mode == "plan" {
					args = append(args, "--plan", writeTestPlan(t, "repo owner/repo", "pick #42", "pick #43"))
				}
				if mode == "edit" {
					args = append(args, "--edit")
				}
				run := runWithDeps(t, commandDeps{lister: lister, operator: operator, editor: &fakeEditor{}}, "", args...)
				if !reflect.DeepEqual(operator.checkedPRs, []int{42, 43}) {
					t.Fatalf("checked PRs = %v, want [42 43]", operator.checkedPRs)
				}
				if !errors.Is(run.err, test.wantErr) {
					t.Fatalf("error = %v, want %v", run.err, test.wantErr)
				}
				if warnings := strings.Count(run.stderr, "Warning: merge policy unverified:"); warnings != test.warnings {
					t.Fatalf("warning count = %d, want %d; stderr = %s", warnings, test.warnings, run.stderr)
				}
				if !strings.Contains(run.stderr, "first lookup failed") {
					t.Fatalf("stderr = %s, want first lookup warning", run.stderr)
				}
				assertOperatorUnused(t, &operator.fakeExecuteOperator)
			})
		}
	}
}

type completionOperator struct {
	fakeExecuteOperator
	wait bool
	repo string
}

func (operator *completionOperator) ReadMergeCapabilities(ctx context.Context, repo string) (githubcli.MergeCapabilities, error) {
	operator.repo = repo
	if operator.wait {
		<-ctx.Done()
		return githubcli.MergeCapabilities{}, ctx.Err()
	}
	return operator.fakeExecuteOperator.ReadMergeCapabilities(ctx, repo)
}

func TestMergeCompletion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		methods    []string
		fail, wait bool
		want       []string
	}{
		{"restricted", []string{"squash"}, false, false, []string{"squash"}},
		{"none", nil, false, false, nil},
		{"offline", nil, true, false, []string{"merge", "squash", "rebase"}},
		{"timeout", nil, false, true, []string{"merge", "squash", "rebase"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			operator := &completionOperator{fakeExecuteOperator: fakeExecuteOperator{capabilities: &githubcli.MergeCapabilities{Methods: test.methods}}, wait: test.wait}
			if test.fail {
				operator.capabilityErr = errors.New("offline")
			}
			run := runWithDeps(t, commandDeps{operator: operator}, "", "__complete", "execute", "--repo", "git.example.com/acme/tool", "--merge-method", "")
			if run.err != nil || operator.repo != "git.example.com/acme/tool" {
				t.Fatalf("%+v repo=%s", run, operator.repo)
			}
			var names []string
			for _, line := range strings.Split(run.stdout, "\n") {
				if strings.Contains(line, "\t") {
					names = append(names, strings.SplitN(line, "\t", 2)[0])
				}
			}
			if !reflect.DeepEqual(names, test.want) || !strings.Contains(run.stdout, ":4") || strings.Contains(run.stdout, "offline") {
				t.Fatalf("%q", run.stdout)
			}
		})
	}
}

func TestInvalidMergeFlagBeforeDiscovery(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"bad", ""} {
		lister := &fakeLister{}
		run := runWithDeps(t, commandDeps{lister: lister}, "", "execute", "--merge-method="+value)
		if run.err == nil || len(lister.limits) != 0 {
			t.Fatalf("%s: %+v", value, run)
		}
	}
}

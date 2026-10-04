package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/githubcli"
	"github.com/JakeTRogers/depflow/internal/planfile"
)

type fakeAlerts struct {
	alerts []githubcli.DependabotAlert
	err    error
	repos  []string
}

func (f *fakeAlerts) ListOpenDependabotAlerts(_ context.Context, repo string) ([]githubcli.DependabotAlert, error) {
	f.repos = append(f.repos, repo)
	return f.alerts, f.err
}

// axiosAlert marks #11 (axios, minor) in planFileFixture as a high-severity security update.
func axiosAlert() *fakeAlerts {
	return &fakeAlerts{alerts: []githubcli.DependabotAlert{{Ecosystem: "npm", Package: "axios", Severity: "high"}}}
}

func TestSecuritySignalInOutputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		alerts     *fakeAlerts
		args       []string
		wantStdout []string
		wantAbsent []string
	}{
		{
			name:       "compact table shows SECURITY column",
			alerts:     axiosAlert(),
			args:       []string{"plan"},
			wantStdout: []string{"CHANGE  SECURITY\n", "#11  minor   npm-and-yarn    axios             minor   high\n", "#10  patch   npm-and-yarn    lodash            patch   -\n"},
		},
		{
			name:       "no SECURITY column without security updates",
			alerts:     &fakeAlerts{},
			args:       []string{"plan"},
			wantAbsent: []string{"SECURITY"},
		},
		{
			name:       "details show signal and reason",
			alerts:     axiosAlert(),
			args:       []string{"plan", "--details"},
			wantStdout: []string{"infra-sensitive=no security=high\n", "reason: minor update from 1.6.0 to 1.7.0; fixes a high-severity Dependabot alert\n", "infra-sensitive=no security=no\n"},
		},
		{
			name:       "scan shows unknown when alerts are unreadable",
			alerts:     &fakeAlerts{err: errors.New("HTTP 403")},
			args:       []string{"scan"},
			wantStdout: []string{"security=unknown\n"},
			wantAbsent: []string{"security=no", "HTTP 403"},
		},
		{
			name:       "security-only filters to security updates",
			alerts:     axiosAlert(),
			args:       []string{"plan", "--security-only"},
			wantStdout: []string{"Planned order for 1 Dependabot pull request(s)", "#11", "#10 Bump lodash from 4.17.20 to 4.17.21 — not a security update (--security-only)"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			run := runWithDeps(t, commandDeps{lister: planFileFixture(), alerts: test.alerts}, "", append([]string{"--repo", "owner/repo"}, test.args...)...)
			if run.err != nil {
				t.Fatalf("Execute() error = %v", run.err)
			}
			for _, fragment := range test.wantStdout {
				if !strings.Contains(run.stdout, fragment) {
					t.Fatalf("stdout missing %q:\n%s", fragment, run.stdout)
				}
			}
			for _, fragment := range test.wantAbsent {
				if strings.Contains(run.stdout+run.stderr, fragment) {
					t.Fatalf("output should not contain %q:\n%s%s", fragment, run.stdout, run.stderr)
				}
			}
			if len(test.alerts.repos) != 1 || test.alerts.repos[0] != "owner/repo" {
				t.Fatalf("alert lookups = %v, want one for owner/repo", test.alerts.repos)
			}
		})
	}
}

func TestSecurityOnlyRequiresReadableAlerts(t *testing.T) {
	t.Parallel()

	for _, deps := range []commandDeps{
		{lister: planFileFixture(), alerts: &fakeAlerts{err: errors.New("HTTP 403")}},
		{lister: planFileFixture()},
	} {
		run := runWithDeps(t, deps, "", "--repo", "owner/repo", "plan", "--security-only")
		if run.err == nil || !strings.Contains(run.err.Error(), "--security-only needs") {
			t.Fatalf("error = %v, want --security-only access error", run.err)
		}
	}
}

func TestPlanFileNotesSecurityUpdates(t *testing.T) {
	t.Parallel()

	run := runWithDeps(t, commandDeps{lister: planFileFixture(), alerts: axiosAlert()}, "", "--repo", "owner/repo", "plan", "-o", "-")
	if run.err != nil {
		t.Fatalf("Execute() error = %v", run.err)
	}
	if !strings.Contains(run.stdout, "pick #11 [minor] Bump axios from 1.6.0 to 1.7.0  # security: high\n") {
		t.Fatalf("plan file missing security note:\n%s", run.stdout)
	}
	if _, err := planfile.Parse(strings.NewReader(run.stdout)); err != nil {
		t.Fatalf("plan file with security notes does not parse: %v", err)
	}
}

func TestSecurityOnlyRejectedWithPlanFile(t *testing.T) {
	t.Parallel()

	path := writeTestPlan(t, "repo owner/repo", "pick #10")
	run := runWithDeps(t, commandDeps{lister: planFileFixture()}, "", "--repo", "owner/repo", "--security-only", "execute", "--plan", path)
	if run.err == nil || !strings.Contains(run.err.Error(), "flag --security-only cannot be used with --plan") {
		t.Fatalf("error = %v, want --plan conflict", run.err)
	}
}

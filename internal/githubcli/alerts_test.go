package githubcli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestListOpenDependabotAlerts(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{output: []byte(`{"ecosystem":"npm","package":"lodash","severity":"high"}
{"ecosystem":"pip","package":"requests","severity":"medium"}
`)}
	alerts, err := newClient(executor).ListOpenDependabotAlerts(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListOpenDependabotAlerts() error = %v", err)
	}
	want := []DependabotAlert{{Ecosystem: "npm", Package: "lodash", Severity: "high"}, {Ecosystem: "pip", Package: "requests", Severity: "medium"}}
	if !reflect.DeepEqual(alerts, want) {
		t.Fatalf("alerts = %+v, want %+v", alerts, want)
	}
	wantArgs := []string{"api", "repos/owner/repo/dependabot/alerts?state=open&per_page=100", "--paginate", "--jq", alertsJQ}
	if !reflect.DeepEqual(executor.calls[0], wantArgs) {
		t.Fatalf("args = %#v, want %#v", executor.calls[0], wantArgs)
	}
}

func TestListOpenDependabotAlertsEmpty(t *testing.T) {
	t.Parallel()

	alerts, err := newClient(&stubExecutor{}).ListOpenDependabotAlerts(context.Background(), "owner/repo")
	if err != nil || len(alerts) != 0 {
		t.Fatalf("ListOpenDependabotAlerts() = %v, %v; want no alerts", alerts, err)
	}
}

func TestListOpenDependabotAlertsErrors(t *testing.T) {
	t.Parallel()

	_, err := newClient(&stubExecutor{err: &commandError{err: errors.New("exit status 1"), output: "gh: You are not authorized to perform this operation. (HTTP 403)"}}).ListOpenDependabotAlerts(context.Background(), "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "listing Dependabot alerts: running gh api repos/owner/repo/dependabot/alerts") || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("error = %v, want wrapped gh error", err)
	}

	_, err = newClient(&stubExecutor{output: []byte("not json")}).ListOpenDependabotAlerts(context.Background(), "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "decoding Dependabot alerts") {
		t.Fatalf("error = %v, want decoding error", err)
	}

	_, err = newClient(&stubExecutor{}).ListOpenDependabotAlerts(context.Background(), "a/b/c/d")
	if err == nil || !strings.Contains(err.Error(), "OWNER/REPO") {
		t.Fatalf("error = %v, want repo format error", err)
	}
}

func TestDependabotAlertsArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		repo string
		want []string
	}{
		{repo: "", want: []string{"api", "repos/{owner}/{repo}/dependabot/alerts?state=open&per_page=100", "--paginate", "--jq", alertsJQ}},
		{repo: "git.example.com/acme/tool", want: []string{"api", "repos/acme/tool/dependabot/alerts?state=open&per_page=100", "--hostname", "git.example.com", "--paginate", "--jq", alertsJQ}},
	}
	for _, test := range tests {
		got, err := dependabotAlertsArgs(test.repo)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("dependabotAlertsArgs(%q) = %#v, %v; want %#v", test.repo, got, err, test.want)
		}
	}
	if _, err := dependabotAlertsArgs("badformat"); err == nil {
		t.Fatal("dependabotAlertsArgs(badformat) error = nil, want format error")
	}
}

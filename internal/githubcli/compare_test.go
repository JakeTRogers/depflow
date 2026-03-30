package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCompareBranches(t *testing.T) {
	t.Parallel()

	result := BranchComparison{BehindBy: 2}
	data, _ := json.Marshal(result)

	executor := &stubExecutor{output: data}
	client := newClient(executor)

	got, err := client.CompareBranches(context.Background(), "owner/repo", "main", "feature")
	if err != nil {
		t.Fatalf("CompareBranches() error = %v", err)
	}

	if got.BehindBy != 2 {
		t.Errorf("BehindBy = %d, want 2", got.BehindBy)
	}

	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	args := executor.calls[0]
	if args[0] != "api" {
		t.Errorf("args[0] = %q, want %q", args[0], "api")
	}
	if !strings.Contains(args[1], "compare/main...feature") {
		t.Errorf("args[1] = %q, want to contain compare path", args[1])
	}
	if args[2] != "--jq" || args[3] != "{behind_by: .behind_by}" {
		t.Errorf("args = %#v, want jq projection for behind_by only", args)
	}
}

func TestCompareBranchesEscapesRefs(t *testing.T) {
	t.Parallel()

	result := BranchComparison{BehindBy: 0}
	data, _ := json.Marshal(result)

	executor := &stubExecutor{output: data}
	client := newClient(executor)

	_, err := client.CompareBranches(context.Background(), "owner/repo", "main", "dependabot/npm_and_yarn/lodash-4.17.21")
	if err != nil {
		t.Fatalf("CompareBranches() error = %v", err)
	}

	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}

	gotPath := executor.calls[0][1]
	wantPath := "repos/owner/repo/compare/main...dependabot%2Fnpm_and_yarn%2Flodash-4.17.21"
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestCompareBranchesEnterpriseRepoUsesHostname(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{output: []byte(`{"behind_by":0}`)}
	client := newClient(executor)

	_, err := client.CompareBranches(context.Background(), "github.example.com/owner/repo", "main", "feature")
	if err != nil {
		t.Fatalf("CompareBranches() error = %v", err)
	}

	wantArgs := []string{
		"api",
		"--hostname",
		"github.example.com",
		"repos/owner/repo/compare/main...feature",
		"--jq",
		"{behind_by: .behind_by}",
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	if got := executor.calls[0]; !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %#v, want %#v", got, wantArgs)
	}
}

func TestCompareBranchesError(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{err: errors.New("boom")})

	_, err := client.CompareBranches(context.Background(), "owner/repo", "main", "feature")
	if err == nil {
		t.Fatal("CompareBranches() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "comparing branches") {
		t.Errorf("error %q does not contain expected context", err)
	}
}

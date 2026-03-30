package githubcli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMergePullRequest(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{}
	client := newClient(executor)

	err := client.MergePullRequest(context.Background(), "", 42)
	if err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}

	wantArgs := []string{
		"pr",
		"merge",
		"42",
		"--merge",
		"--delete-branch",
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	if !reflect.DeepEqual(executor.calls[0], wantArgs) {
		t.Fatalf("args = %#v, want %#v", executor.calls[0], wantArgs)
	}
}

func TestMergePullRequestWithRepo(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{}
	client := newClient(executor)

	err := client.MergePullRequest(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}

	wantArgs := []string{
		"pr",
		"merge",
		"42",
		"--merge",
		"--delete-branch",
		"--repo",
		"owner/repo",
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	if !reflect.DeepEqual(executor.calls[0], wantArgs) {
		t.Fatalf("args = %#v, want %#v", executor.calls[0], wantArgs)
	}
}

func TestMergePullRequestWrapsErrors(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{err: errors.New("boom")})

	err := client.MergePullRequest(context.Background(), "", 42)
	if err == nil {
		t.Fatal("MergePullRequest() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "merging pull request #42") {
		t.Fatalf("error %q does not include context", err)
	}
}

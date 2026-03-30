package githubcli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestApprovePullRequest(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{}
	client := newClient(executor)

	err := client.ApprovePullRequest(context.Background(), "", 42)
	if err != nil {
		t.Fatalf("ApprovePullRequest() error = %v", err)
	}

	wantArgs := []string{
		"pr",
		"review",
		"42",
		"--approve",
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	if !reflect.DeepEqual(executor.calls[0], wantArgs) {
		t.Fatalf("args = %#v, want %#v", executor.calls[0], wantArgs)
	}
}

func TestApprovePullRequestWithRepo(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{}
	client := newClient(executor)

	err := client.ApprovePullRequest(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("ApprovePullRequest() error = %v", err)
	}

	wantArgs := []string{
		"pr",
		"review",
		"42",
		"--approve",
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

func TestApprovePullRequestWrapsErrors(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{err: errors.New("boom")})

	err := client.ApprovePullRequest(context.Background(), "", 42)
	if err == nil {
		t.Fatal("ApprovePullRequest() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "approving pull request #42") {
		t.Fatalf("error %q does not include context", err)
	}
}

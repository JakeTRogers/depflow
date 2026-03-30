package githubcli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCommentOnPR(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{}
	client := newClient(executor)

	err := client.CommentOnPR(context.Background(), "", 42, "test comment")
	if err != nil {
		t.Fatalf("CommentOnPR() error = %v", err)
	}

	wantArgs := []string{
		"pr",
		"comment",
		"42",
		"--body",
		"test comment",
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	if !reflect.DeepEqual(executor.calls[0], wantArgs) {
		t.Fatalf("args = %#v, want %#v", executor.calls[0], wantArgs)
	}
}

func TestCommentOnPRWithRepo(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{}
	client := newClient(executor)

	err := client.CommentOnPR(context.Background(), "owner/repo", 42, "@dependabot rebase")
	if err != nil {
		t.Fatalf("CommentOnPR() error = %v", err)
	}

	wantArgs := []string{
		"pr",
		"comment",
		"42",
		"--body",
		"@dependabot rebase",
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

func TestCommentOnPRWrapsErrors(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{err: errors.New("boom")})

	err := client.CommentOnPR(context.Background(), "", 42, "test")
	if err == nil {
		t.Fatal("CommentOnPR() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "commenting on pull request #42") {
		t.Fatalf("error %q does not include context", err)
	}
}

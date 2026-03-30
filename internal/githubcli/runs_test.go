package githubcli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestListWorkflowRuns(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{output: []byte(`[
		{"name":"CI","status":"completed","conclusion":"success","headSha":"abc123","startedAt":"2026-03-29T10:00:00Z"}
	]`)}
	client := newClient(executor)

	runs, err := client.ListWorkflowRuns(context.Background(), "owner/repo", "dependabot/go/go-1.22")
	if err != nil {
		t.Fatalf("ListWorkflowRuns() error = %v", err)
	}

	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	if runs[0].Name != "CI" {
		t.Fatalf("Name = %q, want CI", runs[0].Name)
	}
	if runs[0].Conclusion != "success" {
		t.Fatalf("Conclusion = %q, want success", runs[0].Conclusion)
	}
	if runs[0].HeadSHA != "abc123" {
		t.Fatalf("HeadSHA = %q, want abc123", runs[0].HeadSHA)
	}

	wantArgs := []string{
		"run",
		"list",
		"--branch",
		"dependabot/go/go-1.22",
		"--json",
		"name,status,conclusion,headSha,startedAt",
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

func TestListWorkflowRunsWrapsExecutionErrors(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{err: errors.New("boom")})

	_, err := client.ListWorkflowRuns(context.Background(), "", "main")
	if err == nil {
		t.Fatal("ListWorkflowRuns() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "listing workflow runs for branch main") {
		t.Fatalf("error %q does not include context", err)
	}
	if !strings.Contains(err.Error(), "running gh run list") {
		t.Fatalf("error %q does not include command context", err)
	}
}

func TestListWorkflowRunsWrapsJSONErrors(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{output: []byte(`{`)})

	_, err := client.ListWorkflowRuns(context.Background(), "", "main")
	if err == nil {
		t.Fatal("ListWorkflowRuns() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "decoding gh run list") || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("error %q does not include decode context", err)
	}
}

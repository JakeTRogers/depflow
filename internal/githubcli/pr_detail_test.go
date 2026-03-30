package githubcli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestViewPullRequest(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{output: []byte(`{
		"number":42,
		"title":"Bump go from 1.21 to 1.22",
		"state":"OPEN",
		"mergeable":"MERGEABLE",
		"mergeCommit":{"oid":"abc123"},
		"headRefName":"dependabot/go/go-1.22",
		"baseRefName":"main",
		"statusCheckRollup":[{"name":"ci","context":"ci","state":"SUCCESS","status":"COMPLETED","conclusion":"SUCCESS"}]
	}`)}
	client := newClient(executor)

	detail, err := client.ViewPullRequest(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("ViewPullRequest() error = %v", err)
	}

	if detail.Number != 42 {
		t.Fatalf("Number = %d, want 42", detail.Number)
	}
	if detail.Title != "Bump go from 1.21 to 1.22" {
		t.Fatalf("Title = %q, want %q", detail.Title, "Bump go from 1.21 to 1.22")
	}
	if detail.Mergeable != "MERGEABLE" {
		t.Fatalf("Mergeable = %q, want MERGEABLE", detail.Mergeable)
	}
	if detail.MergeCommit.OID != "abc123" {
		t.Fatalf("MergeCommit.OID = %q, want abc123", detail.MergeCommit.OID)
	}
	if len(detail.StatusCheckRollup) != 1 {
		t.Fatalf("len(StatusCheckRollup) = %d, want 1", len(detail.StatusCheckRollup))
	}
	if detail.StatusCheckRollup[0].Conclusion != "SUCCESS" {
		t.Fatalf("StatusCheckRollup[0].Conclusion = %q, want SUCCESS", detail.StatusCheckRollup[0].Conclusion)
	}

	wantArgs := []string{
		"pr",
		"view",
		"42",
		"--json",
		"number,title,state,mergeable,mergeCommit,headRefName,baseRefName,statusCheckRollup",
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

func TestViewPullRequestWrapsExecutionErrors(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{err: errors.New("boom")})

	_, err := client.ViewPullRequest(context.Background(), "", 42)
	if err == nil {
		t.Fatal("ViewPullRequest() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "viewing pull request #42") {
		t.Fatalf("error %q does not include context", err)
	}
	if !strings.Contains(err.Error(), "running gh pr view") {
		t.Fatalf("error %q does not include command context", err)
	}
}

func TestViewPullRequestWrapsJSONErrors(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{output: []byte(`{`)})

	_, err := client.ViewPullRequest(context.Background(), "", 42)
	if err == nil {
		t.Fatal("ViewPullRequest() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "decoding gh pr view") || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("error %q does not include decode context", err)
	}
}

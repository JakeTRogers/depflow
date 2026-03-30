package githubcli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type stubExecutor struct {
	output []byte
	err    error
	calls  [][]string
}

func (s *stubExecutor) Run(_ context.Context, args ...string) ([]byte, error) {
	copyArgs := append([]string(nil), args...)
	s.calls = append(s.calls, copyArgs)
	if s.err != nil {
		return nil, s.err
	}

	return s.output, nil
}

func TestListOpenPullRequests(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{output: []byte(`[
		{"number":1,"title":"Bump actions/cache from 4.2.0 to 4.2.1","body":"- Bumps [actions/cache](https://github.com/actions/cache) from 4.2.0 to 4.2.1.","url":"https://example.test/pr/1","isDraft":false,"author":{"login":"dependabot[bot]"},"labels":[{"name":"dependencies"}],"headRefName":"dependabot/github_actions/actions/cache-4.2.1","baseRefName":"main"}
	]`)}
	client := newClient(executor)

	pullRequests, err := client.ListOpenPullRequests(context.Background(), "owner/repo", 50)
	if err != nil {
		t.Fatalf("ListOpenPullRequests() error = %v", err)
	}

	if len(pullRequests) != 1 {
		t.Fatalf("len(pullRequests) = %d, want 1", len(pullRequests))
	}
	if pullRequests[0].Number != 1 {
		t.Fatalf("pullRequests[0].Number = %d, want 1", pullRequests[0].Number)
	}
	if pullRequests[0].Body == "" {
		t.Fatal("pullRequests[0].Body = empty, want decoded body text")
	}

	wantArgs := []string{
		"pr",
		"list",
		"--state",
		"open",
		"--limit",
		"50",
		"--json",
		"number,title,body,url,isDraft,author,labels,headRefName,baseRefName",
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

func TestListOpenPullRequestsWrapsExecutionErrors(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{err: errors.New("boom")})

	_, err := client.ListOpenPullRequests(context.Background(), "", 10)
	if err == nil {
		t.Fatal("ListOpenPullRequests() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "listing open pull requests") {
		t.Fatalf("error %q does not include context", err)
	}
	if !strings.Contains(err.Error(), "running gh pr list") {
		t.Fatalf("error %q does not include command context", err)
	}
}

func TestListOpenPullRequestsWrapsJSONErrors(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{output: []byte(`{`)})

	_, err := client.ListOpenPullRequests(context.Background(), "", 10)
	if err == nil {
		t.Fatal("ListOpenPullRequests() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "decoding gh pr list") || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("error %q does not include decode context", err)
	}
}

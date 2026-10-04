package githubcli

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	original := execLookPath
	execLookPath = func(file string) (string, error) {
		if file != "gh" {
			t.Fatalf("LookPath() file = %q, want gh", file)
		}
		return "/usr/bin/gh", nil
	}
	t.Cleanup(func() {
		execLookPath = original
	})

	gotClient, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if gotClient == nil {
		t.Fatal("NewClient() = nil, want non-nil")
	}

	impl, ok := gotClient.(*client)
	if !ok {
		t.Fatalf("NewClient() concrete type = %T, want *client", gotClient)
	}

	ghExec, ok := impl.exec.(ghExecutor)
	if !ok {
		t.Fatalf("client.exec type = %T, want ghExecutor", impl.exec)
	}
	if ghExec.path != "/usr/bin/gh" {
		t.Fatalf("gh path = %q, want /usr/bin/gh", ghExec.path)
	}
}

func TestNewClientReturnsErrorWhenGHIsMissing(t *testing.T) {
	original := execLookPath
	execLookPath = func(string) (string, error) {
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() {
		execLookPath = original
	})

	_, err := NewClient()
	if err == nil {
		t.Fatal("NewClient() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "gh CLI not found on PATH") {
		t.Fatalf("error = %q, want not-found context", err)
	}
}

func TestGHExecutorRunSuccess(t *testing.T) {
	original := execCommandContext
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "printf '[1]'")
	}
	defer func() {
		execCommandContext = original
	}()

	output, err := ghExecutor{path: "/usr/bin/gh"}.Run(context.Background(), "pr", "list")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if string(output) != "[1]" {
		t.Fatalf("output = %q, want [1]", string(output))
	}
}

func TestGHExecutorRunIncludesStderr(t *testing.T) {
	original := execCommandContext
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "echo auth failed 1>&2; exit 1")
	}
	defer func() {
		execCommandContext = original
	}()

	_, err := ghExecutor{path: "/usr/bin/gh"}.Run(context.Background(), "pr", "list")
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("error = %q, want stderr content", err)
	}
}

func TestGHExecutorRunTruncatesStderr(t *testing.T) {
	original := execCommandContext
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "printf '%*s' 600 '' | tr ' ' x 1>&2; exit 1")
	}
	defer func() {
		execCommandContext = original
	}()

	_, err := ghExecutor{path: "/usr/bin/gh"}.Run(context.Background(), "pr", "list")
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}

	truncated := truncateOutput(strings.Repeat("x", 600), maxCommandErrorOutput)
	if !strings.Contains(err.Error(), truncated) {
		t.Fatalf("error = %q, want truncated stderr content", err)
	}
	if strings.Contains(err.Error(), strings.Repeat("x", 520)) {
		t.Fatalf("error = %q, want stderr to be truncated", err)
	}
}

func TestGHExecutorRunNotFound(t *testing.T) {
	original := execCommandContext
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "definitely-not-installed-gh")
	}
	defer func() {
		execCommandContext = original
	}()

	_, err := ghExecutor{path: "definitely-not-installed-gh"}.Run(context.Background(), "pr", "list")
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "gh CLI not found on PATH") {
		t.Fatalf("error = %q, want not-found context", err)
	}
}

func TestResolveRepo(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{output: []byte(`{"url":"https://git.example.com/owner/repo"}`)}
	client := newClient(executor)

	repo, err := client.ResolveRepo(context.Background())
	if err != nil {
		t.Fatalf("ResolveRepo() error = %v", err)
	}
	if repo != "git.example.com/owner/repo" {
		t.Fatalf("repo = %q, want git.example.com/owner/repo", repo)
	}

	wantArgs := []string{"repo", "view", "--json", "url"}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	if !reflect.DeepEqual(executor.calls[0], wantArgs) {
		t.Fatalf("args = %#v, want %#v", executor.calls[0], wantArgs)
	}
}

func TestGHExecutorRunReportsGHMessageAndAuthFailures(t *testing.T) {
	original := execCommandContext
	defer func() {
		execCommandContext = original
	}()

	tests := []struct {
		name     string
		script   string
		wantText string
		wantAuth bool
	}{
		{name: "auth required", script: "echo 'To get started with GitHub CLI, please run:  gh auth login' 1>&2; exit 4", wantText: "To get started with GitHub CLI, please run:  gh auth login", wantAuth: true},
		{name: "other failure", script: "echo 'GraphQL: Could not resolve to a Repository' 1>&2; exit 1", wantText: "GraphQL: Could not resolve to a Repository"},
		{name: "silent failure", script: "exit 1", wantText: "exit status 1"},
	}

	for _, test := range tests {
		execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", test.script)
		}

		_, err := ghExecutor{path: "/usr/bin/gh"}.Run(context.Background(), "pr", "list")
		if err == nil || err.Error() != test.wantText {
			t.Fatalf("%s: error = %v, want %q", test.name, err, test.wantText)
		}
		if got := errors.Is(err, ErrAuthRequired); got != test.wantAuth {
			t.Fatalf("%s: errors.Is(err, ErrAuthRequired) = %v, want %v", test.name, got, test.wantAuth)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("%s: error does not wrap *exec.ExitError: %v", test.name, err)
		}
	}
}

func TestRunJSONErrorNamesCommandWithoutFlags(t *testing.T) {
	t.Parallel()

	client := newClient(&stubExecutor{err: &commandError{err: errors.New("exit status 1"), output: "GraphQL: boom"}})
	_, err := client.ListOpenPullRequests(context.Background(), "owner/repo", 100)
	if err == nil {
		t.Fatal("ListOpenPullRequests() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "running gh pr list: GraphQL: boom") || strings.Contains(err.Error(), "--json") {
		t.Fatalf("error = %q, want short command name and gh message", err)
	}
}

func TestCommandSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"pr", "list", "--state", "open", "--json", "number,title"}, want: "pr list"},
		{args: []string{"pr", "view", "42", "--json", "state"}, want: "pr view 42"},
		{args: []string{"api", "repos/o/r/compare/main...dep"}, want: "api repos/o/r/compare/main...dep"},
		{args: []string{"api", "graphql", "-f", "query=x"}, want: "api graphql"},
	}
	for _, test := range tests {
		if got := commandSummary(test.args); got != test.want {
			t.Errorf("commandSummary(%v) = %q, want %q", test.args, got, test.want)
		}
	}
}

package githubcli

import (
	"context"
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

	executor := &stubExecutor{output: []byte(`{"nameWithOwner":"owner/repo"}`)}
	client := newClient(executor)

	repo, err := client.ResolveRepo(context.Background())
	if err != nil {
		t.Fatalf("ResolveRepo() error = %v", err)
	}
	if repo != "owner/repo" {
		t.Fatalf("repo = %q, want owner/repo", repo)
	}

	wantArgs := []string{"repo", "view", "--json", "nameWithOwner"}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	if !reflect.DeepEqual(executor.calls[0], wantArgs) {
		t.Fatalf("args = %#v, want %#v", executor.calls[0], wantArgs)
	}
}

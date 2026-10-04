// Package githubcli wraps the GitHub CLI for repository and pull request operations.
package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const maxCommandErrorOutput = 512

// ghExitAuthRequired is the exit status gh uses when authentication is required.
const ghExitAuthRequired = 4

// ErrAuthRequired reports that gh is not authenticated for the target host.
var ErrAuthRequired = errors.New("gh authentication required")

// commandError reports a failed gh invocation by gh's own message, falling back to the process
// error (such as "exit status 1") when gh printed nothing.
type commandError struct {
	err    error
	output string
}

func (e *commandError) Error() string {
	if e.output != "" {
		return e.output
	}
	return e.err.Error()
}

func (e *commandError) Unwrap() []error {
	var coded interface{ ExitCode() int }
	if errors.As(e.err, &coded) && coded.ExitCode() == ghExitAuthRequired {
		return []error{e.err, ErrAuthRequired}
	}
	return []error{e.err}
}

type executor interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

var execCommandContext = exec.CommandContext
var execLookPath = exec.LookPath

type ghExecutor struct {
	path string
}

func newGHExecutor() (ghExecutor, error) {
	path, err := execLookPath("gh")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ghExecutor{}, fmt.Errorf("gh CLI not found on PATH: %w", err)
		}
		return ghExecutor{}, fmt.Errorf("locating gh CLI: %w", err)
	}

	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(path)
		if err != nil {
			return ghExecutor{}, fmt.Errorf("resolving absolute gh CLI path: %w", err)
		}
	}

	return ghExecutor{path: path}, nil
}

func (g ghExecutor) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := execCommandContext(ctx, g.path, args...)
	cmd.Env = append(os.Environ(), "GH_PAGER=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("gh CLI not found on PATH: %w", err)
		}

		return nil, &commandError{err: err, output: truncateOutput(strings.TrimSpace(string(output)), maxCommandErrorOutput)}
	}

	return output, nil
}

type client struct {
	exec executor
}

// Client provides the GitHub operations exposed by this package.
type Client interface {
	ListOpenPullRequests(ctx context.Context, repo string, limit int) ([]PullRequest, error)
	ViewPullRequest(ctx context.Context, repo string, number int) (PRDetail, error)
	ApprovePullRequest(ctx context.Context, repo string, number int) error
	MergePullRequest(ctx context.Context, repo string, number int, admin bool, method, headSHA string) error
	ReadMergeCapabilities(ctx context.Context, repo string) (MergeCapabilities, error)
	CheckMergeAllowed(ctx context.Context, repo string, number int, method string) error
	CommentOnPR(ctx context.Context, repo string, number int, body string) error
	ListWorkflowRuns(ctx context.Context, repo string, branch string) ([]WorkflowRun, error)
	CompareBranches(ctx context.Context, repo string, base string, head string) (BranchComparison, error)
	ResolveRepo(ctx context.Context) (string, error)
}

// NewClient returns a GitHub CLI client backed by the `gh` executable.
func NewClient() (Client, error) {
	exec, err := newGHExecutor()
	if err != nil {
		return nil, err
	}

	return newClient(exec), nil
}

// NewLazyClient defers locating gh until a GitHub operation is requested.
func NewLazyClient() Client {
	return newClient(&lazyExecutor{})
}

type lazyExecutor struct {
	once sync.Once
	exec ghExecutor
	err  error
}

func (lazy *lazyExecutor) Run(ctx context.Context, args ...string) ([]byte, error) {
	lazy.once.Do(func() { lazy.exec, lazy.err = newGHExecutor() })
	if lazy.err != nil {
		return nil, lazy.err
	}
	return lazy.exec.Run(ctx, args...)
}

func newClient(exec executor) *client {
	return &client{exec: exec}
}

// ResolveRepo returns the current GitHub repository inferred by the gh CLI.
func (c *client) ResolveRepo(ctx context.Context) (string, error) {
	var repo struct {
		URL string `json:"url"`
	}

	if err := c.runJSON(ctx, &repo, "repo", "view", "--json", "url"); err != nil {
		return "", fmt.Errorf("resolving repository: %w", err)
	}

	return repositoryFromURL(repo.URL)
}

func truncateOutput(output string, limit int) string {
	if len(output) <= limit {
		return output
	}

	const suffix = "..."
	if limit <= len(suffix) {
		return suffix[:limit]
	}

	return output[:limit-len(suffix)] + suffix
}

func (c *client) runJSON(ctx context.Context, destination any, args ...string) error {
	output, err := c.exec.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("running gh %s: %w", commandSummary(args), err)
	}

	if err := json.Unmarshal(output, destination); err != nil {
		return fmt.Errorf("decoding gh %s JSON: %w", commandSummary(args), err)
	}

	return nil
}

// commandSummary names a gh invocation by its leading non-flag arguments, such as "pr view 42",
// so errors stay readable without repeating every flag and JSON field.
func commandSummary(args []string) string {
	for i, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return strings.Join(args[:i], " ")
		}
	}
	return strings.Join(args, " ")
}

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
)

const maxCommandErrorOutput = 512

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

		trimmedOutput := truncateOutput(strings.TrimSpace(string(output)), maxCommandErrorOutput)
		if trimmedOutput != "" {
			return nil, fmt.Errorf("%w: %s", err, trimmedOutput)
		}

		return nil, err
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
	MergePullRequest(ctx context.Context, repo string, number int) error
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

func newClient(exec executor) *client {
	return &client{exec: exec}
}

// ResolveRepo returns the current GitHub repository inferred by the gh CLI.
func (c *client) ResolveRepo(ctx context.Context) (string, error) {
	var repo struct {
		NameWithOwner string `json:"nameWithOwner"`
	}

	if err := c.runJSON(ctx, &repo, "repo", "view", "--json", "nameWithOwner"); err != nil {
		return "", fmt.Errorf("resolving repository: %w", err)
	}

	if strings.TrimSpace(repo.NameWithOwner) == "" {
		return "", errors.New("gh repo view returned an empty repository name")
	}

	return repo.NameWithOwner, nil
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
		return fmt.Errorf("running gh %s: %w", strings.Join(args, " "), err)
	}

	if err := json.Unmarshal(output, destination); err != nil {
		return fmt.Errorf("decoding gh %s JSON: %w", strings.Join(args, " "), err)
	}

	return nil
}

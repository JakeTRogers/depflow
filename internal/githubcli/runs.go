package githubcli

import (
	"context"
	"fmt"
)

// WorkflowRun represents a GitHub Actions workflow run.
type WorkflowRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSHA    string `json:"headSha"`
	StartedAt  string `json:"startedAt"`
}

// ListWorkflowRuns returns recent workflow runs for a branch.
func (c *client) ListWorkflowRuns(ctx context.Context, repo string, branch string) ([]WorkflowRun, error) {
	args := []string{
		"run",
		"list",
		"--branch",
		branch,
		"--json",
		"name,status,conclusion,headSha,startedAt",
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}

	var runs []WorkflowRun
	if err := c.runJSON(ctx, &runs, args...); err != nil {
		return nil, fmt.Errorf("listing workflow runs for branch %s: %w", branch, err)
	}

	return runs, nil
}

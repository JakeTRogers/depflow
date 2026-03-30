package githubcli

import (
	"context"
	"fmt"
	"strconv"
)

// ApprovePullRequest submits an approval review for the PR.
func (c *client) ApprovePullRequest(ctx context.Context, repo string, number int) error {
	args := []string{
		"pr",
		"review",
		strconv.Itoa(number),
		"--approve",
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}

	if _, err := c.exec.Run(ctx, args...); err != nil {
		return fmt.Errorf("approving pull request #%d: %w", number, err)
	}

	return nil
}

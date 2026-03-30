package githubcli

import (
	"context"
	"fmt"
	"strconv"
)

// MergePullRequest merge-commits the PR and deletes the head branch.
func (c *client) MergePullRequest(ctx context.Context, repo string, number int) error {
	args := []string{
		"pr",
		"merge",
		strconv.Itoa(number),
		"--merge",
		"--delete-branch",
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}

	if _, err := c.exec.Run(ctx, args...); err != nil {
		return fmt.Errorf("merging pull request #%d: %w", number, err)
	}

	return nil
}

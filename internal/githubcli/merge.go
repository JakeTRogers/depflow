package githubcli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/JakeTRogers/depflow/internal/config"
)

// MergePullRequest merges the PR using the selected method and deletes the head branch. A
// non-empty headSHA makes GitHub reject the merge if the head commit has moved since it was
// verified.
func (c *client) MergePullRequest(ctx context.Context, repo string, number int, admin bool, method, headSHA string) error {
	if err := config.ValidateMethod(method); err != nil {
		return err
	}
	args := []string{
		"pr",
		"merge",
		strconv.Itoa(number),
		"--" + method,
		"--delete-branch",
	}
	if headSHA != "" {
		args = append(args, "--match-head-commit", headSHA)
	}
	if admin {
		args = append(args, "--admin")
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}

	if _, err := c.exec.Run(ctx, args...); err != nil {
		return fmt.Errorf("merging pull request #%d: %w", number, err)
	}

	return nil
}

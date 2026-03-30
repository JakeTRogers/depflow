package githubcli

import (
	"context"
	"fmt"
	"strconv"
)

// CommentOnPR posts a comment on the given pull request.
func (c *client) CommentOnPR(ctx context.Context, repo string, number int, body string) error {
	args := []string{
		"pr",
		"comment",
		strconv.Itoa(number),
		"--body",
		body,
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}

	if _, err := c.exec.Run(ctx, args...); err != nil {
		return fmt.Errorf("commenting on pull request #%d: %w", number, err)
	}

	return nil
}

package githubcli

import (
	"context"
	"fmt"
	"strconv"
)

// PullRequest captures the open PR fields required for milestone 1.
type PullRequest struct {
	Number      int                `json:"number"`
	Title       string             `json:"title"`
	Body        string             `json:"body"`
	URL         string             `json:"url"`
	IsDraft     bool               `json:"isDraft"`
	Author      PullRequestAuthor  `json:"author"`
	Labels      []PullRequestLabel `json:"labels"`
	HeadRefName string             `json:"headRefName"`
	BaseRefName string             `json:"baseRefName"`
}

// PullRequestAuthor describes the PR author returned by `gh pr list`.
type PullRequestAuthor struct {
	Login string `json:"login"`
}

// PullRequestLabel describes a PR label returned by `gh pr list`.
type PullRequestLabel struct {
	Name string `json:"name"`
}

// ListOpenPullRequests returns open PRs for the current repo or an explicit repo override.
func (c *client) ListOpenPullRequests(ctx context.Context, repo string, limit int) ([]PullRequest, error) {
	if limit < 1 {
		return nil, fmt.Errorf("listing open pull requests: limit must be greater than zero")
	}

	args := []string{
		"pr",
		"list",
		"--state",
		"open",
		"--limit",
		strconv.Itoa(limit),
		"--json",
		"number,title,body,url,isDraft,author,labels,headRefName,baseRefName",
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}

	var pullRequests []PullRequest
	if err := c.runJSON(ctx, &pullRequests, args...); err != nil {
		return nil, fmt.Errorf("listing open pull requests: %w", err)
	}

	return pullRequests, nil
}

package githubcli

import (
	"context"
	"fmt"
	"strconv"
)

// PRDetail captures the detailed PR state needed for execute operations.
type PRDetail struct {
	Number            int           `json:"number"`
	Title             string        `json:"title"`
	State             string        `json:"state"`
	Mergeable         string        `json:"mergeable"`
	MergeCommit       MergeCommit   `json:"mergeCommit"`
	HeadRefName       string        `json:"headRefName"`
	BaseRefName       string        `json:"baseRefName"`
	StatusCheckRollup []StatusCheck `json:"statusCheckRollup"`
}

// MergeCommit represents the merge commit attached to a merged PR.
type MergeCommit struct {
	OID string `json:"oid"`
}

// StatusCheck represents a single CI status check from the PR's statusCheckRollup.
type StatusCheck struct {
	Name       string `json:"name"`
	Context    string `json:"context"`
	State      string `json:"state"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// ViewPullRequest returns detailed PR state including mergeability and check status.
func (c *client) ViewPullRequest(ctx context.Context, repo string, number int) (PRDetail, error) {
	args := []string{
		"pr",
		"view",
		strconv.Itoa(number),
		"--json",
		"number,title,state,mergeable,mergeCommit,headRefName,baseRefName,statusCheckRollup",
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}

	var detail PRDetail
	if err := c.runJSON(ctx, &detail, args...); err != nil {
		return PRDetail{}, fmt.Errorf("viewing pull request #%d: %w", number, err)
	}

	return detail, nil
}

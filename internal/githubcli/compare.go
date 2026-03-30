package githubcli

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// BranchComparison holds the result of comparing two branches.
type BranchComparison struct {
	BehindBy int `json:"behind_by"`
}

// CompareBranches compares head against base and returns how far behind it is.
func (c *client) CompareBranches(ctx context.Context, repo string, base string, head string) (BranchComparison, error) {
	args, err := compareBranchesArgs(repo, base, head)
	if err != nil {
		return BranchComparison{}, fmt.Errorf("comparing branches %s...%s: %w", base, head, err)
	}

	var result BranchComparison
	if err := c.runJSON(ctx, &result, args...); err != nil {
		return BranchComparison{}, fmt.Errorf("comparing branches %s...%s: %w", base, head, err)
	}

	return result, nil
}

func compareBranchesArgs(repo string, base string, head string) ([]string, error) {
	comparePath := fmt.Sprintf(
		"repos/%s/compare/%s...%s",
		repo,
		url.PathEscape(base),
		url.PathEscape(head),
	)

	switch strings.Count(repo, "/") {
	case 1:
		return []string{"api", comparePath, "--jq", "{behind_by: .behind_by}"}, nil
	case 2:
		parts := strings.SplitN(repo, "/", 3)
		comparePath = fmt.Sprintf(
			"repos/%s/%s/compare/%s...%s",
			parts[1],
			parts[2],
			url.PathEscape(base),
			url.PathEscape(head),
		)
		return []string{"api", "--hostname", parts[0], comparePath, "--jq", "{behind_by: .behind_by}"}, nil
	default:
		return nil, fmt.Errorf("repo must be in OWNER/REPO or HOST/OWNER/REPO format")
	}
}

package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/githubcli"
)

func discoverDependabotPRs(ctx context.Context, deps commandDeps, opts *commandOptions) ([]dependabot.PR, error) {
	if opts.limit < 1 {
		return nil, fmt.Errorf("limit must be greater than zero")
	}

	prs, err := listOpenPullRequestsForDiscovery(ctx, deps, opts)
	if err != nil {
		if opts.repo == "" {
			return nil, fmt.Errorf("discovering open pull requests: %w; %s", err, rerunWithRepoHint("if the current repository cannot be inferred"))
		}
		return nil, fmt.Errorf("discovering open pull requests: %w", err)
	}

	dependabotPRs := make([]dependabot.PR, 0, len(prs))
	for _, pr := range prs {
		dependabotPR, ok := dependabot.Normalize(pr)
		if !ok {
			continue
		}
		dependabotPRs = append(dependabotPRs, dependabotPR)
	}

	sort.Slice(dependabotPRs, func(i, j int) bool {
		return dependabotPRs[i].Number < dependabotPRs[j].Number
	})

	return dependabotPRs, nil
}

// applyLimit caps prs to opts.limit. Callers apply this after classification filtering so
// --limit bounds the eligible result set rather than the raw discovered set.
func applyLimit(prs []dependabot.PR, opts *commandOptions) []dependabot.PR {
	if len(prs) > opts.limit {
		return prs[:opts.limit]
	}
	return prs
}

func listOpenPullRequestsForDiscovery(ctx context.Context, deps commandDeps, opts *commandOptions) ([]githubcli.PullRequest, error) {
	requestLimit := opts.limit
	if requestLimit < defaultPullRequestLimit {
		requestLimit = defaultPullRequestLimit
	}
	if requestLimit > maxDiscoveryPullRequestLimit {
		requestLimit = maxDiscoveryPullRequestLimit
	}

	previousCount := -1
	for {
		prs, err := deps.lister.ListOpenPullRequests(ctx, opts.repo, requestLimit)
		if err != nil {
			return nil, err
		}

		if len(prs) < requestLimit || len(prs) == previousCount || requestLimit == maxDiscoveryPullRequestLimit {
			return prs, nil
		}

		previousCount = len(prs)
		requestLimit *= 2
		if requestLimit > maxDiscoveryPullRequestLimit {
			requestLimit = maxDiscoveryPullRequestLimit
		}
	}
}

// defaultChangeKindValues mirrors the historical default of excluding major version updates
// unless explicitly requested.
var defaultChangeKindValues = []string{"patch", "minor", "unknown"}

const changeKindAll = "all"

// parseChangeKinds parses --change-kind flag values into an allow-list. The special value
// "all" (anywhere in the list) disables change-kind filtering entirely (returns nil, nil).
func parseChangeKinds(values []string) ([]dependabot.ChangeKind, error) {
	kinds := make([]dependabot.ChangeKind, 0, len(values))
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), changeKindAll) {
			return nil, nil
		}
		kind, ok := dependabot.ParseChangeKind(value)
		if !ok {
			return nil, fmt.Errorf("invalid --change-kind value %q (want patch, minor, major, unknown, or all)", value)
		}
		kinds = append(kinds, kind)
	}
	return kinds, nil
}

func buildFilterOptions(opts *commandOptions, changeKinds []dependabot.ChangeKind, includeDrafts, applyDraftFilter bool) dependabot.FilterOptions {
	return dependabot.FilterOptions{
		ChangeKinds:         changeKinds,
		Ecosystems:          opts.ecosystems,
		ExcludeEcosystems:   opts.excludeEcosystems,
		Dependencies:        opts.dependencies,
		ExcludeDependencies: opts.excludeDependencies,
		RequireLabels:       opts.requireLabels,
		ExcludeLabels:       opts.excludeLabels,
		SkipGrouped:         opts.skipGrouped,
		IncludeDrafts:       includeDrafts,
		ApplyDraftFilter:    applyDraftFilter,
	}
}

func displayOrUnknown(value string) string {
	value = sanitize(value)
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}

	return value
}

func formatLabels(labels []string) string {
	if len(labels) == 0 {
		return "(none)"
	}

	formatted := make([]string, 0, len(labels))
	for _, label := range labels {
		label = sanitize(label)
		if strings.TrimSpace(label) == "" {
			continue
		}
		formatted = append(formatted, label)
	}

	if len(formatted) == 0 {
		return "(none)"
	}

	return strings.Join(formatted, ", ")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}

	return "no"
}

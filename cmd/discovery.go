package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/githubcli"
	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/spf13/cobra"
)

func discoverDependabotPRs(ctx context.Context, deps commandDeps, opts *commandOptions) ([]dependabot.PR, error) {
	if opts.limit < 1 {
		return nil, fmt.Errorf("limit must be greater than zero")
	}

	prs, err := listOpenPullRequestsForDiscovery(ctx, deps, opts)
	if err != nil {
		if opts.repo == "" && !errors.Is(err, githubcli.ErrAuthRequired) {
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

	if err := markSecurityUpdates(ctx, deps, opts, dependabotPRs); err != nil {
		return nil, err
	}

	return dependabotPRs, nil
}

// markSecurityUpdates records which PRs fix open Dependabot alerts. Alerts need extra access, so
// when they cannot be read the PRs keep an unknown status, unless --security-only depends on it.
func markSecurityUpdates(ctx context.Context, deps commandDeps, opts *commandOptions, prs []dependabot.PR) error {
	if len(prs) == 0 {
		return nil
	}
	if deps.alerts == nil {
		if opts.securityOnly {
			return errors.New("--security-only needs the repository's Dependabot alerts, but no alert source is configured")
		}
		return nil
	}

	raw, err := deps.alerts.ListOpenDependabotAlerts(ctx, opts.repo)
	if err != nil {
		if opts.securityOnly {
			return fmt.Errorf("--security-only needs read access to the repository's Dependabot alerts: %w", err)
		}
		return nil
	}

	alerts := make([]dependabot.Alert, 0, len(raw))
	for _, alert := range raw {
		alerts = append(alerts, dependabot.Alert{Ecosystem: alert.Ecosystem, Package: alert.Package, Severity: alert.Severity})
	}
	dependabot.MarkSecurityUpdates(prs, alerts)
	return nil
}

// securityLabel describes a PR's security status: its alert severity, "no", or "unknown" when
// alerts could not be read.
func securityLabel(status dependabot.SecurityStatus) string {
	switch {
	case !status.Checked:
		return "unknown"
	case !status.Update:
		return "no"
	case status.Severity == "":
		return "yes"
	default:
		return sanitize(status.Severity)
	}
}

// applyLimit caps prs to opts.limit. Callers apply this after classification filtering so
// --limit bounds the eligible result set rather than the raw discovered set.
func applyLimit(prs []dependabot.PR, opts *commandOptions) []dependabot.PR {
	if len(prs) > opts.limit {
		return prs[:opts.limit]
	}
	return prs
}

// warnUnmatchedEcosystems flags --ecosystem and --exclude-ecosystem values that match none of the
// discovered PRs, which usually means a typo; for an exclusion that means nothing was excluded.
func warnUnmatchedEcosystems(writer io.Writer, prs []dependabot.PR, opts *commandOptions) error {
	if len(prs) == 0 {
		return nil
	}

	found := make(map[string]struct{}, len(prs))
	for _, pr := range prs {
		found[dependabot.NormalizeEcosystem(pr.Classification.Ecosystem)] = struct{}{}
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, displayOrUnknown(name))
	}
	sort.Strings(names)

	for _, flag := range []struct {
		name   string
		values []string
	}{
		{name: "ecosystem", values: opts.ecosystems},
		{name: "exclude-ecosystem", values: opts.excludeEcosystems},
	} {
		for _, value := range flag.values {
			if _, ok := found[dependabot.NormalizeEcosystem(value)]; ok {
				continue
			}
			if _, err := fmt.Fprintf(writer, "Warning: --%s %q matches no open Dependabot PRs (ecosystems found: %s)\n", flag.name, sanitize(value), sanitize(strings.Join(names, ", "))); err != nil {
				return fmt.Errorf("writing ecosystem warning: %w", err)
			}
		}
	}
	return nil
}

// limitPlan keeps the first opts.limit items of an ordered plan, so --limit selects the PRs that
// would be processed first, and returns the items it cut.
func limitPlan(plan planner.Plan, opts *commandOptions) (planner.Plan, []planner.PlannedPR) {
	if len(plan.Items) <= opts.limit {
		return plan, nil
	}
	return planner.Plan{Items: plan.Items[:opts.limit:opts.limit]}, plan.Items[opts.limit:]
}

// limitReason explains why a PR cut by --limit was left out.
func limitReason(opts *commandOptions) string {
	return fmt.Sprintf("beyond --limit %d", opts.limit)
}

func writeLimitNotice(writer io.Writer, cut int, opts *commandOptions) error {
	if cut == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(writer, "Not included: %d more eligible PR(s) %s\n", cut, limitReason(opts)); err != nil {
		return fmt.Errorf("writing limit notice: %w", err)
	}
	return nil
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

// changeKindCompletions provides shell completion for the --change-kind flag values.
var changeKindCompletions = cobra.FixedCompletions(
	[]string{
		cobra.CompletionWithDesc("patch", "patch version updates (e.g. 1.2.3 -> 1.2.4)"),
		cobra.CompletionWithDesc("minor", "minor version updates (e.g. 1.2.3 -> 1.3.0)"),
		cobra.CompletionWithDesc("major", "major version updates (e.g. 1.2.3 -> 2.0.0)"),
		cobra.CompletionWithDesc("unknown", "updates whose version bump could not be classified"),
		cobra.CompletionWithDesc(changeKindAll, "disable change-kind filtering entirely"),
	},
	cobra.ShellCompDirectiveNoFileComp,
)

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
		SecurityOnly:        opts.securityOnly,
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

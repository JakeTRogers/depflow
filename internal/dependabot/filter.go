package dependabot

import (
	"fmt"
	"strings"
)

// FilterOptions describes the operator-selected subset of Dependabot PRs to act on.
// Allow-list fields (ChangeKinds, Ecosystems, Dependencies) impose no restriction when empty.
type FilterOptions struct {
	ChangeKinds         []ChangeKind
	Ecosystems          []string
	ExcludeEcosystems   []string
	Dependencies        []string
	ExcludeDependencies []string
	RequireLabels       []string
	ExcludeLabels       []string
	SkipGrouped         bool
	IncludeDrafts       bool
	ApplyDraftFilter    bool
}

// ExcludedPR pairs a filtered-out PR with the reason it was excluded.
type ExcludedPR struct {
	PR     PR
	Reason string
}

// Filter partitions prs into included and excluded sets based on opts. The first matching
// exclusion rule determines the reported reason.
func Filter(prs []PR, opts FilterOptions) (included []PR, excluded []ExcludedPR) {
	included = make([]PR, 0, len(prs))
	for _, pr := range prs {
		if reason, ok := exclusionReason(pr, opts); ok {
			excluded = append(excluded, ExcludedPR{PR: pr, Reason: reason})
			continue
		}
		included = append(included, pr)
	}
	return included, excluded
}

func exclusionReason(pr PR, opts FilterOptions) (string, bool) {
	if opts.ApplyDraftFilter && !opts.IncludeDrafts && pr.Draft {
		return "draft PR (use --include-drafts to include)", true
	}

	if len(opts.ChangeKinds) > 0 {
		kind := pr.Classification.ChangeKind
		if pr.Classification.HasMajorVersionBump() {
			kind = ChangeMajor
		}
		if !containsChangeKind(opts.ChangeKinds, kind) {
			return fmt.Sprintf("change-kind %q not in --change-kind allow-list", kind), true
		}
	}

	ecosystem := strings.ToLower(pr.Classification.Ecosystem)
	if matchesAny(ecosystem, opts.ExcludeEcosystems) {
		return fmt.Sprintf("ecosystem %q excluded by --exclude-ecosystem", pr.Classification.Ecosystem), true
	}
	if len(opts.Ecosystems) > 0 && !matchesAny(ecosystem, opts.Ecosystems) {
		return fmt.Sprintf("ecosystem %q not in --ecosystem allow-list", pr.Classification.Ecosystem), true
	}

	dependency := strings.ToLower(pr.Classification.DependencyName)
	if containsAnySubstring(dependency, opts.ExcludeDependencies) {
		return fmt.Sprintf("dependency %q excluded by --exclude-dependency", pr.Classification.DependencyName), true
	}
	if len(opts.Dependencies) > 0 && !containsAnySubstring(dependency, opts.Dependencies) {
		return fmt.Sprintf("dependency %q not in --dependency allow-list", pr.Classification.DependencyName), true
	}

	if label, ok := firstMatchingLabel(pr.Labels, opts.ExcludeLabels); ok {
		return fmt.Sprintf("label %q excluded by --exclude-label", label), true
	}
	if missing, ok := firstMissingLabel(pr.Labels, opts.RequireLabels); ok {
		return fmt.Sprintf("missing required label %q", missing), true
	}

	if opts.SkipGrouped && pr.Classification.Grouped {
		return "grouped update excluded by --skip-grouped", true
	}

	return "", false
}

func containsChangeKind(kinds []ChangeKind, kind ChangeKind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

func matchesAny(value string, candidates []string) bool {
	for _, c := range candidates {
		if value == strings.ToLower(strings.TrimSpace(c)) {
			return true
		}
	}
	return false
}

func containsAnySubstring(value string, candidates []string) bool {
	for _, c := range candidates {
		c = strings.ToLower(strings.TrimSpace(c))
		if c != "" && strings.Contains(value, c) {
			return true
		}
	}
	return false
}

func firstMatchingLabel(labels []string, candidates []string) (string, bool) {
	for _, c := range candidates {
		for _, label := range labels {
			if strings.EqualFold(label, c) {
				return label, true
			}
		}
	}
	return "", false
}

func firstMissingLabel(labels []string, required []string) (string, bool) {
	for _, r := range required {
		found := false
		for _, label := range labels {
			if strings.EqualFold(label, r) {
				found = true
				break
			}
		}
		if !found {
			return r, true
		}
	}
	return "", false
}

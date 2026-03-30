// Package dependabot normalizes and classifies Dependabot pull requests.
package dependabot

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ChangeKind describes the semantic version impact inferred from a PR title.
type ChangeKind string

const (
	// ChangeUnknown indicates the semantic version impact could not be inferred.
	ChangeUnknown ChangeKind = "unknown"
	// ChangePatch indicates a patch version change.
	ChangePatch ChangeKind = "patch"
	// ChangeMinor indicates a minor version change.
	ChangeMinor ChangeKind = "minor"
	// ChangeMajor indicates a major version change.
	ChangeMajor ChangeKind = "major"
)

// Classification contains the milestone-1 signals used by the planner.
type Classification struct {
	Ecosystem               string
	DependencyName          string
	PreviousVersion         string
	NextVersion             string
	ChangeKind              ChangeKind
	Grouped                 bool
	CI                      bool
	DeveloperTooling        bool
	InfrastructureSensitive bool
	DevToolingKeywords      []string
	InfraSensitiveKeywords  []string
}

var (
	fromToPattern                 = regexp.MustCompile(`(?i)\bfrom\s+([^\s]+)\s+to\s+([^\s]+)\b`)
	bumpTitlePattern              = regexp.MustCompile(`(?i)^bump\s+(.+?)\s+from\b`)
	groupedDependencyTitlePattern = regexp.MustCompile(`(?i)^bump\s+(.+?)(?:\s+from\s+[^\s]+\s+to\s+[^\s]+)?\s+in\s+(?:the\s+)?(.+?)\s+group\b`)
	groupedSummaryTitlePattern    = regexp.MustCompile(`(?i)^bump\s+(?:the\s+)?(.+?)\s+group\b`)
	conventionalCommitPattern     = regexp.MustCompile(`^[a-z]+(?:\([^)]+\))?!?:\s*`)
	versionPattern                = regexp.MustCompile(`(?i)^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?`)
	headVersionPattern            = regexp.MustCompile(`-(v?\d+(?:\.\d+){0,2}[^/]*)$`)

	canonicalDependabotAuthors = map[string]struct{}{
		"app/dependabot":          {},
		"dependabot":              {},
		"dependabot-preview[bot]": {},
		"dependabot[bot]":         {},
	}

	devToolingKeywords = []string{
		"black",
		"coverage",
		"cypress",
		"eslint",
		"flake8",
		"gofumpt",
		"golangci-lint",
		"goimports",
		"gomock",
		"hadolint",
		"isort",
		"jest",
		"mockery",
		"mypy",
		"playwright",
		"prettier",
		"pytest",
		"rspec",
		"rubocop",
		"ruff",
		"shellcheck",
		"staticcheck",
		"testify",
		"tox",
		"vitest",
	}

	infraSensitiveKeywords = []string{
		"aks",
		"ansible",
		"aws",
		"azure",
		"container",
		"docker",
		"eks",
		"gcp",
		"gke",
		"google-cloud",
		"helm",
		"istio",
		"k8s",
		"kubernetes",
		"oci",
		"pulumi",
		"terraform",
		"terragrunt",
	}
)

type semanticVersion struct {
	major int
	minor int
	patch int
}

type groupedTitleMatch struct {
	groupName      string
	leadDependency string
}

func isDependabotAuthor(login string) bool {
	normalized := strings.ToLower(strings.TrimSpace(login))
	if normalized == "" {
		return false
	}

	_, ok := canonicalDependabotAuthors[normalized]
	return ok
}

func classify(title, headRef string, labels []string) Classification {
	ecosystem := inferEcosystem(headRef, title, labels)
	dependencyName := inferDependencyName(title, headRef, ecosystem)
	previousVersion, nextVersion := inferVersionRange(title)
	changeKind := inferChangeKind(previousVersion, nextVersion)
	grouped := inferGrouped(title, headRef, labels)
	signalText := buildSignalText(title, headRef, labels, ecosystem, dependencyName)
	devMatches := matchKeywords(signalText, devToolingKeywords)
	infraMatches := matchKeywords(signalText, infraSensitiveKeywords)

	return Classification{
		Ecosystem:               ecosystem,
		DependencyName:          dependencyName,
		PreviousVersion:         previousVersion,
		NextVersion:             nextVersion,
		ChangeKind:              changeKind,
		Grouped:                 grouped,
		CI:                      ecosystem == "github-actions",
		DeveloperTooling:        len(devMatches) > 0,
		InfrastructureSensitive: len(infraMatches) > 0,
		DevToolingKeywords:      devMatches,
		InfraSensitiveKeywords:  infraMatches,
	}
}

func inferEcosystem(headRef, title string, labels []string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(headRef)), "/")
	if len(parts) >= 2 && parts[0] == "dependabot" && parts[1] != "" {
		return strings.ReplaceAll(parts[1], "_", "-")
	}

	labelText := strings.ToLower(strings.Join(labels, " "))
	if strings.Contains(labelText, "github-actions") || strings.Contains(labelText, "github_actions") {
		return "github-actions"
	}

	titleLower := strings.ToLower(title)
	if strings.Contains(titleLower, "actions/") {
		return "github-actions"
	}

	return ""
}

func inferDependencyName(title, headRef, ecosystem string) string {
	if match, ok := parseGroupedTitle(title); ok {
		return match.displayName()
	}

	normalizedTitle := stripConventionalCommitPrefix(title)
	if matches := bumpTitlePattern.FindStringSubmatch(normalizedTitle); len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}

	trimmedHeadRef := strings.TrimSpace(headRef)
	if ecosystem == "" {
		return trimmedHeadRef
	}

	rawEcosystem := strings.ReplaceAll(ecosystem, "-", "_")
	prefix := fmt.Sprintf("dependabot/%s/", rawEcosystem)
	remainder := strings.TrimPrefix(trimmedHeadRef, prefix)
	if remainder == trimmedHeadRef {
		return trimmedHeadRef
	}

	return strings.TrimSpace(headVersionPattern.ReplaceAllString(remainder, ""))
}

func inferVersionRange(title string) (string, string) {
	matches := fromToPattern.FindStringSubmatch(strings.TrimSpace(title))
	if len(matches) != 3 {
		return "", ""
	}

	return matches[1], matches[2]
}

func inferGrouped(title, headRef string, labels []string) bool {
	if _, ok := parseGroupedTitle(title); ok {
		return true
	}

	lowerHeadRef := strings.ToLower(headRef)
	if strings.Contains(lowerHeadRef, "/group-") || strings.Contains(lowerHeadRef, "/group_") || strings.Contains(lowerHeadRef, "-group-") {
		return true
	}

	for _, label := range labels {
		lowerLabel := strings.ToLower(label)
		if strings.Contains(lowerLabel, "group") && strings.Contains(lowerLabel, "depend") {
			return true
		}
	}

	return false
}

func parseGroupedTitle(title string) (groupedTitleMatch, bool) {
	trimmedTitle := stripConventionalCommitPrefix(title)
	if matches := groupedDependencyTitlePattern.FindStringSubmatch(trimmedTitle); len(matches) == 3 {
		return groupedTitleMatch{
			groupName:      humanizeGroupName(matches[2]),
			leadDependency: strings.TrimSpace(matches[1]),
		}, true
	}

	if matches := groupedSummaryTitlePattern.FindStringSubmatch(trimmedTitle); len(matches) == 2 {
		return groupedTitleMatch{groupName: humanizeGroupName(matches[1])}, true
	}

	return groupedTitleMatch{}, false
}

func stripConventionalCommitPrefix(title string) string {
	trimmedTitle := strings.TrimSpace(title)
	prefix := conventionalCommitPattern.FindString(trimmedTitle)
	if prefix == "" {
		return trimmedTitle
	}

	remainder := strings.TrimSpace(strings.TrimPrefix(trimmedTitle, prefix))
	if !strings.HasPrefix(strings.ToLower(remainder), "bump ") {
		return trimmedTitle
	}

	return remainder
}

func (match groupedTitleMatch) displayName() string {
	if match.groupName == "" {
		return match.leadDependency
	}
	if match.leadDependency == "" {
		return match.groupName + " group"
	}

	return fmt.Sprintf("%s (%s group)", match.leadDependency, match.groupName)
}

func humanizeGroupName(groupName string) string {
	replacer := strings.NewReplacer("_", " ", "-", " ")
	return strings.Join(strings.Fields(replacer.Replace(strings.TrimSpace(groupName))), " ")
}

func buildSignalText(title, headRef string, labels []string, ecosystem, dependencyName string) string {
	parts := make([]string, 0, 4+len(labels))
	parts = append(parts, title, headRef, ecosystem, dependencyName)
	parts = append(parts, labels...)
	return strings.ToLower(strings.Join(parts, "\n"))
}

func matchKeywords(signalText string, keywords []string) []string {
	matches := make([]string, 0, len(keywords))
	seen := make(map[string]struct{}, len(keywords))
	for _, keyword := range keywords {
		if !strings.Contains(signalText, keyword) {
			continue
		}
		if _, ok := seen[keyword]; ok {
			continue
		}
		seen[keyword] = struct{}{}
		matches = append(matches, keyword)
	}

	sort.Strings(matches)
	return matches
}

func inferChangeKind(previousVersion, nextVersion string) ChangeKind {
	fromVersion, ok := parseSemanticVersion(previousVersion)
	if !ok {
		return ChangeUnknown
	}

	toVersion, ok := parseSemanticVersion(nextVersion)
	if !ok {
		return ChangeUnknown
	}

	switch {
	case fromVersion.major != toVersion.major:
		return ChangeMajor
	case fromVersion.minor != toVersion.minor:
		return ChangeMinor
	case fromVersion.patch != toVersion.patch:
		return ChangePatch
	default:
		return ChangeUnknown
	}
}

func parseSemanticVersion(value string) (semanticVersion, bool) {
	matches := versionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(matches) == 0 {
		return semanticVersion{}, false
	}

	version := semanticVersion{}
	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return semanticVersion{}, false
	}
	version.major = major

	if len(matches) > 2 && matches[2] != "" {
		minor, err := strconv.Atoi(matches[2])
		if err != nil {
			return semanticVersion{}, false
		}
		version.minor = minor
	}

	if len(matches) > 3 && matches[3] != "" {
		patch, err := strconv.Atoi(matches[3])
		if err != nil {
			return semanticVersion{}, false
		}
		version.patch = patch
	}

	return version, true
}

package dependabot

import (
	"regexp"
	"strings"
)

// SecurityStatus records whether a PR updates a package with an open Dependabot security alert.
type SecurityStatus struct {
	// Checked is false when alerts could not be read, so the PR's status is unknown.
	Checked bool
	// Update is true when the PR updates a package with an open alert.
	Update bool
	// Severity is the highest severity among the matched alerts, such as "high".
	Severity string
}

// Alert is an open Dependabot security alert for one package.
type Alert struct {
	// Ecosystem is the advisory ecosystem, such as "npm", "pip", or "rubygems".
	Ecosystem string
	Package   string
	Severity  string
}

// alertEcosystems maps advisory ecosystems to the Dependabot branch ecosystems that update them.
var alertEcosystems = map[string][]string{
	"actions":  {"github-actions"},
	"composer": {"composer"},
	"erlang":   {"hex"},
	"go":       {"go-modules"},
	"maven":    {"maven", "gradle"},
	"npm":      {"npm-and-yarn", "bun"},
	"nuget":    {"nuget"},
	"pip":      {"pip", "uv"},
	"pub":      {"pub"},
	"rubygems": {"bundler"},
	"rust":     {"cargo"},
	"swift":    {"swift"},
}

var severityRanks = map[string]int{"critical": 1, "high": 2, "medium": 3, "low": 4}

var pythonNameSeparators = regexp.MustCompile(`[-_.]+`)

// SeverityRank orders severities from most to least severe; unrecognized values sort last.
func SeverityRank(severity string) int {
	if rank, ok := severityRanks[strings.ToLower(severity)]; ok {
		return rank
	}
	return len(severityRanks) + 1
}

// MarkSecurityUpdates records on each PR whether it updates a package with one of the open
// alerts. A PR matches an alert when one of its dependencies has the alert's package name and,
// when both are known, a compatible ecosystem. Alerts are not matched by manifest path, so in a
// monorepo an update in one directory can match an alert raised for another.
func MarkSecurityUpdates(prs []PR, alerts []Alert) {
	for i := range prs {
		status := SecurityStatus{Checked: true}
		for _, alert := range alerts {
			if !alertMatches(prs[i].Classification, alert) {
				continue
			}
			status.Update = true
			if status.Severity == "" || SeverityRank(alert.Severity) < SeverityRank(status.Severity) {
				status.Severity = strings.ToLower(alert.Severity)
			}
		}
		prs[i].Classification.Security = status
	}
}

func alertMatches(classification Classification, alert Alert) bool {
	ecosystem := strings.ToLower(strings.TrimSpace(alert.Ecosystem))
	if branchEcosystems, ok := alertEcosystems[ecosystem]; ok && classification.Ecosystem != "" {
		compatible := false
		for _, candidate := range branchEcosystems {
			if NormalizeEcosystem(classification.Ecosystem) == candidate {
				compatible = true
				break
			}
		}
		if !compatible {
			return false
		}
	}

	want := packageKey(alert.Package, ecosystem)
	for _, dependency := range classification.Dependencies {
		if want != "" && packageKey(dependency, ecosystem) == want {
			return true
		}
	}
	return false
}

// packageKey normalizes a package name for comparison: case-insensitively, and for Python with
// "-", "_", and "." runs treated alike as PEP 503 specifies.
func packageKey(name, ecosystem string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if ecosystem == "pip" {
		key = pythonNameSeparators.ReplaceAllString(key, "-")
	}
	return key
}

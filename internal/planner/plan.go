// Package planner builds deterministic processing plans for Dependabot PRs.
package planner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JakeTRogers/depflow/internal/dependabot"
)

// Bucket is the deterministic milestone-1 processing class for a PR.
type Bucket string

const (
	// BucketCI prioritizes GitHub Actions updates first.
	BucketCI Bucket = "ci"
	// BucketDevTooling prioritizes developer tooling after CI updates.
	BucketDevTooling Bucket = "developer-tooling"
	// BucketPatch keeps low-risk patch updates early.
	BucketPatch Bucket = "patch"
	// BucketMinor keeps standard minor updates after patch updates.
	BucketMinor Bucket = "minor"
	// BucketGrouped pushes grouped updates later than simple single-package updates.
	BucketGrouped Bucket = "grouped"
	// BucketUnknown keeps insufficiently classified updates in a conservative late bucket.
	BucketUnknown Bucket = "unknown"
	// BucketInfraSensitive keeps infra-sensitive updates late.
	BucketInfraSensitive Bucket = "infra-sensitive"
	// BucketMajor keeps major updates last.
	BucketMajor Bucket = "major"
)

// PlannedPR is a planned queue entry with its bucket and rationale.
type PlannedPR struct {
	PR     dependabot.PR
	Bucket Bucket
	Reason string
}

// Plan is the deterministic ordering returned by milestone 1.
type Plan struct {
	Items []PlannedPR
}

// Build returns the deterministic milestone-1 processing order.
func Build(prs []dependabot.PR) Plan {
	items := make([]PlannedPR, 0, len(prs))
	for _, pr := range prs {
		bucket := selectBucket(pr.Classification)
		reason := buildReason(pr.Classification, bucket)
		if security := pr.Classification.Security; security.Update {
			reason += fmt.Sprintf("; fixes a %s-severity Dependabot alert", displaySeverity(security.Severity))
		}
		items = append(items, PlannedPR{
			PR:     pr,
			Bucket: bucket,
			Reason: reason,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		left := items[i]
		right := items[j]

		leftBucketRank := bucketRank(left.Bucket)
		rightBucketRank := bucketRank(right.Bucket)
		if leftBucketRank != rightBucketRank {
			return leftBucketRank < rightBucketRank
		}

		// Within a bucket, security updates go first, most severe first.
		leftSecurityRank := securityRank(left.PR.Classification.Security)
		rightSecurityRank := securityRank(right.PR.Classification.Security)
		if leftSecurityRank != rightSecurityRank {
			return leftSecurityRank < rightSecurityRank
		}

		leftChangeRank := changeKindRank(left.PR.Classification.ChangeKind)
		rightChangeRank := changeKindRank(right.PR.Classification.ChangeKind)
		if leftChangeRank != rightChangeRank {
			return leftChangeRank < rightChangeRank
		}

		leftEcosystem := strings.ToLower(left.PR.Classification.Ecosystem)
		rightEcosystem := strings.ToLower(right.PR.Classification.Ecosystem)
		if leftEcosystem != rightEcosystem {
			return leftEcosystem < rightEcosystem
		}

		leftDependency := strings.ToLower(left.PR.Classification.DependencyName)
		rightDependency := strings.ToLower(right.PR.Classification.DependencyName)
		if leftDependency == "" {
			leftDependency = strings.ToLower(left.PR.Title)
		}
		if rightDependency == "" {
			rightDependency = strings.ToLower(right.PR.Title)
		}
		if leftDependency != rightDependency {
			return leftDependency < rightDependency
		}

		if left.PR.Title != right.PR.Title {
			return left.PR.Title < right.PR.Title
		}

		return left.PR.Number < right.PR.Number
	})

	return Plan{Items: items}
}

// BuildOrdered classifies prs like Build but orders the plan by the PR numbers in order.
// PRs whose numbers are absent from order keep their Build order after all ordered PRs.
func BuildOrdered(prs []dependabot.PR, order []int) Plan {
	plan := Build(prs)

	position := make(map[int]int, len(order))
	for index, number := range order {
		if _, seen := position[number]; !seen {
			position[number] = index
		}
	}

	rank := func(item PlannedPR) int {
		if index, ok := position[item.PR.Number]; ok {
			return index
		}
		return len(order)
	}

	sort.SliceStable(plan.Items, func(i, j int) bool {
		return rank(plan.Items[i]) < rank(plan.Items[j])
	})

	return plan
}

func selectBucket(classification dependabot.Classification) Bucket {
	switch {
	case classification.HasMajorVersionBump():
		return BucketMajor
	case classification.InfrastructureSensitive:
		return BucketInfraSensitive
	case classification.Grouped:
		return BucketGrouped
	case classification.CI:
		return BucketCI
	case classification.DeveloperTooling:
		return BucketDevTooling
	case classification.ChangeKind == dependabot.ChangePatch:
		return BucketPatch
	case classification.ChangeKind == dependabot.ChangeMinor:
		return BucketMinor
	default:
		return BucketUnknown
	}
}

func bucketRank(bucket Bucket) int {
	switch bucket {
	case BucketCI:
		return 1
	case BucketDevTooling:
		return 2
	case BucketPatch:
		return 3
	case BucketMinor:
		return 4
	case BucketGrouped:
		return 5
	case BucketUnknown:
		return 6
	case BucketInfraSensitive:
		return 7
	case BucketMajor:
		return 8
	default:
		return 9
	}
}

func securityRank(security dependabot.SecurityStatus) int {
	if !security.Update {
		return dependabot.SeverityRank("") + 1
	}
	return dependabot.SeverityRank(security.Severity)
}

func displaySeverity(severity string) string {
	if severity == "" {
		return "unknown"
	}
	return severity
}

func changeKindRank(kind dependabot.ChangeKind) int {
	switch kind {
	case dependabot.ChangePatch:
		return 1
	case dependabot.ChangeMinor:
		return 2
	case dependabot.ChangeUnknown:
		return 3
	case dependabot.ChangeMajor:
		return 4
	default:
		return 5
	}
}

func buildReason(classification dependabot.Classification, bucket Bucket) string {
	switch bucket {
	case BucketCI:
		return "GitHub Actions ecosystem update sorts first"
	case BucketDevTooling:
		return fmt.Sprintf("matched developer tooling keyword(s): %s", strings.Join(classification.DevToolingKeywords, ", "))
	case BucketPatch:
		if classification.PreviousVersion != "" && classification.NextVersion != "" {
			return fmt.Sprintf("patch update from %s to %s", classification.PreviousVersion, classification.NextVersion)
		}
		return "low-risk patch update"
	case BucketMinor:
		if classification.PreviousVersion != "" && classification.NextVersion != "" {
			return fmt.Sprintf("minor update from %s to %s", classification.PreviousVersion, classification.NextVersion)
		}
		return "minor update"
	case BucketGrouped:
		return "grouped update sorts after simple low-risk updates"
	case BucketUnknown:
		if classification.IsCommitUpdate() {
			return fmt.Sprintf("commit update from %s to %s has no version impact; kept in the conservative unknown bucket", shortSHA(classification.PreviousVersion), shortSHA(classification.NextVersion))
		}
		return "insufficient metadata for a low-risk bucket; kept in the conservative unknown bucket"
	case BucketInfraSensitive:
		if len(classification.InfraSensitiveKeywords) > 0 {
			return fmt.Sprintf("matched infra-sensitive keyword(s): %s; sorts after low-risk buckets", strings.Join(classification.InfraSensitiveKeywords, ", "))
		}
		return "infra-sensitive update sorts after low-risk buckets"
	case BucketMajor:
		if classification.PreviousVersion != "" && classification.NextVersion != "" {
			return fmt.Sprintf("major update from %s to %s sorts last", classification.PreviousVersion, classification.NextVersion)
		}
		if classification.ContainsMajorUpdate {
			return "contains at least one major version bump; sorts last"
		}
		return "major update sorts last"
	default:
		return "deterministic tie-breakers applied"
	}
}

// shortSHA abbreviates a full 40-character commit SHA to git's default seven characters and
// leaves anything else, such as the version side of a mixed range, unchanged.
func shortSHA(value string) string {
	if len(value) == 40 {
		return value[:7]
	}
	return value
}

package planner

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/dependabot"
)

func TestBuildOrdersByBucket(t *testing.T) {
	t.Parallel()

	plan := Build([]dependabot.PR{
		newPR(8, "major", dependabot.Classification{ChangeKind: dependabot.ChangeMajor, PreviousVersion: "1.0.0", NextVersion: "2.0.0"}),
		newPR(4, "minor", dependabot.Classification{ChangeKind: dependabot.ChangeMinor}),
		newPR(2, "dev", dependabot.Classification{ChangeKind: dependabot.ChangeMinor, DeveloperTooling: true, DevToolingKeywords: []string{"golangci-lint"}}),
		newPR(6, "unknown", dependabot.Classification{ChangeKind: dependabot.ChangeUnknown}),
		newPR(5, "grouped", dependabot.Classification{ChangeKind: dependabot.ChangePatch, Grouped: true}),
		newPR(7, "infra", dependabot.Classification{ChangeKind: dependabot.ChangePatch, InfrastructureSensitive: true, InfraSensitiveKeywords: []string{"docker"}}),
		newPR(3, "patch", dependabot.Classification{ChangeKind: dependabot.ChangePatch}),
		newPR(1, "ci", dependabot.Classification{ChangeKind: dependabot.ChangePatch, Ecosystem: "github-actions", CI: true}),
	})

	got := planNumbers(plan)
	want := []int{1, 2, 3, 4, 5, 6, 7, 8}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %#v, want %#v", got, want)
	}
	if plan.Items[4].Bucket != BucketGrouped {
		t.Fatalf("grouped bucket = %q, want %q", plan.Items[4].Bucket, BucketGrouped)
	}
	if plan.Items[4].Reason != "grouped update sorts after simple low-risk updates" {
		t.Fatalf("grouped reason = %q", plan.Items[4].Reason)
	}
}

func TestPartitionMajor(t *testing.T) {
	t.Parallel()

	prs := []dependabot.PR{
		newPR(1, "patch", dependabot.Classification{ChangeKind: dependabot.ChangePatch}),
		newPR(2, "direct major", dependabot.Classification{ChangeKind: dependabot.ChangeMajor}),
		newPR(3, "grouped major", dependabot.Classification{Grouped: true, ContainsMajorUpdate: true}),
		newPR(4, "infra major", dependabot.Classification{InfrastructureSensitive: true, ContainsMajorUpdate: true}),
		newPR(5, "minor", dependabot.Classification{ChangeKind: dependabot.ChangeMinor}),
	}

	t.Run("exclude major by default", func(t *testing.T) {
		t.Parallel()

		included, excluded := PartitionMajor(prs, false)
		if got := prNumbers(included); !reflect.DeepEqual(got, []int{1, 5}) {
			t.Fatalf("included = %#v, want %#v", got, []int{1, 5})
		}
		if got := prNumbers(excluded); !reflect.DeepEqual(got, []int{2, 3, 4}) {
			t.Fatalf("excluded = %#v, want %#v", got, []int{2, 3, 4})
		}
	})

	t.Run("include major when requested", func(t *testing.T) {
		t.Parallel()

		included, excluded := PartitionMajor(prs, true)
		if got := prNumbers(included); !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5}) {
			t.Fatalf("included = %#v, want %#v", got, []int{1, 2, 3, 4, 5})
		}
		if len(excluded) != 0 {
			t.Fatalf("len(excluded) = %d, want 0", len(excluded))
		}
	})
}

func TestBuildUsesDeterministicTieBreakers(t *testing.T) {
	t.Parallel()

	left := []dependabot.PR{
		newPR(9, "Bump zebra from 1.0.0 to 1.0.1", dependabot.Classification{Ecosystem: "go-modules", DependencyName: "zebra", ChangeKind: dependabot.ChangePatch}),
		newPR(4, "Bump alpha from 1.0.0 to 1.0.1", dependabot.Classification{Ecosystem: "npm-and-yarn", DependencyName: "alpha", ChangeKind: dependabot.ChangePatch}),
		newPR(3, "Bump alpha from 1.0.0 to 1.0.1", dependabot.Classification{Ecosystem: "go-modules", DependencyName: "alpha", ChangeKind: dependabot.ChangePatch}),
	}
	right := []dependabot.PR{left[2], left[1], left[0]}

	leftPlan := Build(left)
	rightPlan := Build(right)

	leftNumbers := planNumbers(leftPlan)
	rightNumbers := planNumbers(rightPlan)
	if !reflect.DeepEqual(leftNumbers, rightNumbers) {
		t.Fatalf("left order = %#v, right order = %#v", leftNumbers, rightNumbers)
	}

	want := []int{3, 9, 4}
	if !reflect.DeepEqual(leftNumbers, want) {
		t.Fatalf("order = %#v, want %#v", leftNumbers, want)
	}
}

func TestRankHelpers(t *testing.T) {
	t.Parallel()

	if got := bucketRank(BucketUnknown); got != 6 {
		t.Fatalf("bucketRank(BucketUnknown) = %d, want 6", got)
	}
	if got := bucketRank(Bucket("custom")); got != 9 {
		t.Fatalf("bucketRank(custom) = %d, want 9", got)
	}
	if got := changeKindRank(dependabot.ChangeMajor); got != 4 {
		t.Fatalf("changeKindRank(ChangeMajor) = %d, want 4", got)
	}
	if got := changeKindRank(dependabot.ChangeKind("other")); got != 5 {
		t.Fatalf("changeKindRank(other) = %d, want 5", got)
	}
}

func TestBuildReasonBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		classification dependabot.Classification
		bucket         Bucket
		wantContains   string
	}{
		{name: "major", classification: dependabot.Classification{PreviousVersion: "1.0.0", NextVersion: "2.0.0"}, bucket: BucketMajor, wantContains: "major update from 1.0.0 to 2.0.0"},
		{name: "grouped major fallback", classification: dependabot.Classification{Grouped: true, ContainsMajorUpdate: true}, bucket: BucketMajor, wantContains: "contains at least one major version bump"},
		{name: "infra", classification: dependabot.Classification{InfraSensitiveKeywords: []string{"docker"}}, bucket: BucketInfraSensitive, wantContains: "docker"},
		{name: "unknown", classification: dependabot.Classification{}, bucket: BucketUnknown, wantContains: "conservative unknown bucket"},
		{name: "dev", classification: dependabot.Classification{DevToolingKeywords: []string{"golangci-lint"}}, bucket: BucketDevTooling, wantContains: "golangci-lint"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reason := buildReason(test.classification, test.bucket)
			if !strings.Contains(reason, test.wantContains) {
				t.Fatalf("reason = %q, want substring %q", reason, test.wantContains)
			}
		})
	}
}

func TestSelectBucketTreatsAnyMajorSignalAsMajor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		classification dependabot.Classification
		want           Bucket
	}{
		{
			name: "grouped body major",
			classification: dependabot.Classification{
				Grouped:             true,
				ContainsMajorUpdate: true,
			},
			want: BucketMajor,
		},
		{
			name: "infra sensitive body major",
			classification: dependabot.Classification{
				InfrastructureSensitive: true,
				ContainsMajorUpdate:     true,
			},
			want: BucketMajor,
		},
		{
			name: "non-major infra stays infra",
			classification: dependabot.Classification{
				InfrastructureSensitive: true,
			},
			want: BucketInfraSensitive,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := selectBucket(test.classification); got != test.want {
				t.Fatalf("selectBucket() = %q, want %q", got, test.want)
			}
		})
	}
}

func newPR(number int, title string, classification dependabot.Classification) dependabot.PR {
	return dependabot.PR{
		Number:         number,
		Title:          title,
		Classification: classification,
	}
}

func planNumbers(plan Plan) []int {
	numbers := make([]int, 0, len(plan.Items))
	for _, item := range plan.Items {
		numbers = append(numbers, item.PR.Number)
	}

	return numbers
}

func prNumbers(prs []dependabot.PR) []int {
	numbers := make([]int, 0, len(prs))
	for _, pr := range prs {
		numbers = append(numbers, pr.Number)
	}

	return numbers
}

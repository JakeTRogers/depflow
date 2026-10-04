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
		{name: "commit update", classification: dependabot.Classification{PreviousVersion: "08eba0b27e820071cde6df949e0beb9ba4906955", NextVersion: "34e1148"}, bucket: BucketUnknown, wantContains: "commit update from 08eba0b to 34e1148"},
		{name: "mixed commit update keeps version", classification: dependabot.Classification{PreviousVersion: "v1.2.3-beta.1", NextVersion: "34e1148"}, bucket: BucketUnknown, wantContains: "from v1.2.3-beta.1 to 34e1148"},
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

func TestBuildOrderedFollowsGivenOrder(t *testing.T) {
	t.Parallel()

	prs := []dependabot.PR{
		newPR(1, "ci", dependabot.Classification{ChangeKind: dependabot.ChangePatch, Ecosystem: "github-actions", CI: true}),
		newPR(2, "patch", dependabot.Classification{ChangeKind: dependabot.ChangePatch}),
		newPR(3, "major", dependabot.Classification{ChangeKind: dependabot.ChangeMajor}),
		newPR(4, "minor", dependabot.Classification{ChangeKind: dependabot.ChangeMinor}),
	}

	tests := []struct {
		name  string
		order []int
		want  []int
	}{
		{name: "explicit order", order: []int{3, 1, 4, 2}, want: []int{3, 1, 4, 2}},
		{name: "unlisted PRs keep planner order after listed ones", order: []int{4}, want: []int{4, 1, 2, 3}},
		{name: "unknown numbers ignored", order: []int{99, 2, 1}, want: []int{2, 1, 4, 3}},
		{name: "empty order matches Build", order: nil, want: []int{1, 2, 4, 3}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			plan := BuildOrdered(prs, test.order)
			if got := planNumbers(plan); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("order = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestBuildOrderedKeepsBucketAndReason(t *testing.T) {
	t.Parallel()

	plan := BuildOrdered([]dependabot.PR{
		newPR(1, "patch", dependabot.Classification{ChangeKind: dependabot.ChangePatch}),
		newPR(2, "major", dependabot.Classification{ChangeKind: dependabot.ChangeMajor}),
	}, []int{2, 1})

	if plan.Items[0].Bucket != BucketMajor {
		t.Fatalf("first bucket = %q, want %q", plan.Items[0].Bucket, BucketMajor)
	}
	if plan.Items[0].Reason != "major update sorts last" {
		t.Fatalf("first reason = %q", plan.Items[0].Reason)
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

package dependabot

import (
	"reflect"
	"testing"
)

func newFilterPR(number int, title string, draft bool, labels []string, classification Classification) PR {
	return PR{
		Number:         number,
		Title:          title,
		Draft:          draft,
		Labels:         labels,
		Classification: classification,
	}
}

func excludedNumbers(excluded []ExcludedPR) []int {
	numbers := make([]int, 0, len(excluded))
	for _, ex := range excluded {
		numbers = append(numbers, ex.PR.Number)
	}
	return numbers
}

func includedNumbers(included []PR) []int {
	numbers := make([]int, 0, len(included))
	for _, pr := range included {
		numbers = append(numbers, pr.Number)
	}
	return numbers
}

func TestFilterChangeKindAllowList(t *testing.T) {
	t.Parallel()

	prs := []PR{
		newFilterPR(1, "patch", false, nil, Classification{ChangeKind: ChangePatch}),
		newFilterPR(2, "direct major", false, nil, Classification{ChangeKind: ChangeMajor}),
		newFilterPR(3, "grouped major", false, nil, Classification{Grouped: true, ContainsMajorUpdate: true}),
		newFilterPR(4, "minor", false, nil, Classification{ChangeKind: ChangeMinor}),
	}

	included, excluded := Filter(prs, FilterOptions{ChangeKinds: []ChangeKind{ChangePatch, ChangeMinor, ChangeUnknown}})
	if got := includedNumbers(included); !reflect.DeepEqual(got, []int{1, 4}) {
		t.Fatalf("included = %#v, want [1 4]", got)
	}
	if got := excludedNumbers(excluded); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("excluded = %#v, want [2 3]", got)
	}

	includedAll, excludedAll := Filter(prs, FilterOptions{})
	if len(excludedAll) != 0 {
		t.Fatalf("excluded with no ChangeKinds restriction = %#v, want none", excludedAll)
	}
	if len(includedAll) != len(prs) {
		t.Fatalf("included with no ChangeKinds restriction = %d, want %d", len(includedAll), len(prs))
	}
}

func TestFilterDraftsExcludedOnlyWhenApplied(t *testing.T) {
	t.Parallel()

	prs := []PR{
		newFilterPR(1, "ready", false, nil, Classification{}),
		newFilterPR(2, "draft", true, nil, Classification{}),
	}

	included, excluded := Filter(prs, FilterOptions{ApplyDraftFilter: true})
	if got := includedNumbers(included); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("included = %#v, want [1]", got)
	}
	if got := excludedNumbers(excluded); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("excluded = %#v, want [2]", got)
	}

	includedDraftsOn, _ := Filter(prs, FilterOptions{ApplyDraftFilter: true, IncludeDrafts: true})
	if got := includedNumbers(includedDraftsOn); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("included with IncludeDrafts = %#v, want [1 2]", got)
	}

	includedNoFilter, _ := Filter(prs, FilterOptions{})
	if got := includedNumbers(includedNoFilter); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("included with ApplyDraftFilter=false = %#v, want [1 2]", got)
	}
}

func TestFilterEcosystemAllowAndExclude(t *testing.T) {
	t.Parallel()

	prs := []PR{
		newFilterPR(1, "npm", false, nil, Classification{Ecosystem: "npm-and-yarn"}),
		newFilterPR(2, "go", false, nil, Classification{Ecosystem: "go-modules"}),
		newFilterPR(3, "docker", false, nil, Classification{Ecosystem: "docker"}),
	}

	included, excluded := Filter(prs, FilterOptions{Ecosystems: []string{"npm-and-yarn", "go-modules"}})
	if got := includedNumbers(included); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("included = %#v, want [1 2]", got)
	}
	if got := excludedNumbers(excluded); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("excluded = %#v, want [3]", got)
	}

	included, excluded = Filter(prs, FilterOptions{ExcludeEcosystems: []string{"docker"}})
	if got := includedNumbers(included); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("included = %#v, want [1 2]", got)
	}
	if got := excludedNumbers(excluded); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("excluded = %#v, want [3]", got)
	}
}

func TestFilterDependencyAllowAndExcludeIsSubstringMatch(t *testing.T) {
	t.Parallel()

	prs := []PR{
		newFilterPR(1, "lodash", false, nil, Classification{DependencyName: "lodash"}),
		newFilterPR(2, "react", false, nil, Classification{DependencyName: "react-dom"}),
	}

	included, excluded := Filter(prs, FilterOptions{Dependencies: []string{"react"}})
	if got := includedNumbers(included); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("included = %#v, want [2]", got)
	}
	if got := excludedNumbers(excluded); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("excluded = %#v, want [1]", got)
	}

	included, excluded = Filter(prs, FilterOptions{ExcludeDependencies: []string{"lodash"}})
	if got := includedNumbers(included); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("included = %#v, want [2]", got)
	}
	if got := excludedNumbers(excluded); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("excluded = %#v, want [1]", got)
	}
}

func TestFilterRequireLabelRequiresAll(t *testing.T) {
	t.Parallel()

	prs := []PR{
		newFilterPR(1, "both", false, []string{"go", "patch"}, Classification{}),
		newFilterPR(2, "one", false, []string{"go"}, Classification{}),
		newFilterPR(3, "none", false, nil, Classification{}),
	}

	included, excluded := Filter(prs, FilterOptions{RequireLabels: []string{"go", "patch"}})
	if got := includedNumbers(included); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("included = %#v, want [1]", got)
	}
	if got := excludedNumbers(excluded); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("excluded = %#v, want [2 3]", got)
	}
}

func TestFilterExcludeLabelExcludesAny(t *testing.T) {
	t.Parallel()

	prs := []PR{
		newFilterPR(1, "clean", false, []string{"dependencies"}, Classification{}),
		newFilterPR(2, "blocked", false, []string{"dependencies", "do-not-merge"}, Classification{}),
	}

	included, excluded := Filter(prs, FilterOptions{ExcludeLabels: []string{"do-not-merge", "wip"}})
	if got := includedNumbers(included); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("included = %#v, want [1]", got)
	}
	if got := excludedNumbers(excluded); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("excluded = %#v, want [2]", got)
	}
}

func TestFilterSkipGrouped(t *testing.T) {
	t.Parallel()

	prs := []PR{
		newFilterPR(1, "single", false, nil, Classification{Grouped: false}),
		newFilterPR(2, "grouped", false, nil, Classification{Grouped: true}),
	}

	included, excluded := Filter(prs, FilterOptions{SkipGrouped: true})
	if got := includedNumbers(included); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("included = %#v, want [1]", got)
	}
	if got := excludedNumbers(excluded); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("excluded = %#v, want [2]", got)
	}
}

func TestParseChangeKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input  string
		want   ChangeKind
		wantOk bool
	}{
		{input: "patch", want: ChangePatch, wantOk: true},
		{input: " Minor ", want: ChangeMinor, wantOk: true},
		{input: "MAJOR", want: ChangeMajor, wantOk: true},
		{input: "unknown", want: ChangeUnknown, wantOk: true},
		{input: "all", wantOk: false},
		{input: "bogus", wantOk: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()

			got, ok := ParseChangeKind(test.input)
			if ok != test.wantOk {
				t.Fatalf("ParseChangeKind(%q) ok = %t, want %t", test.input, ok, test.wantOk)
			}
			if ok && got != test.want {
				t.Fatalf("ParseChangeKind(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

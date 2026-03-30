package dependabot

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JakeTRogers/depflow/internal/githubcli"
)

func TestIsDependabotAuthor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		login string
		want  bool
	}{
		{name: "bot login", login: "dependabot[bot]", want: true},
		{name: "app login", login: "app/dependabot", want: true},
		{name: "plain login", login: "dependabot", want: true},
		{name: "legacy login", login: "dependabot-preview[bot]", want: true},
		{name: "trimmed case-insensitive canonical login", login: " Dependabot[Bot] ", want: true},
		{name: "prefixed lookalike", login: "dependabot-tools[bot]", want: false},
		{name: "path lookalike", login: "team/dependabot", want: false},
		{name: "embedded lookalike", login: "octo-dependabot-helper", want: false},
		{name: "regular user", login: "octocat", want: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := isDependabotAuthor(test.login); got != test.want {
				t.Fatalf("isDependabotAuthor(%q) = %t, want %t", test.login, got, test.want)
			}
		})
	}
}

func TestNormalizeRejectsNonDependabot(t *testing.T) {
	t.Parallel()

	_, ok := Normalize(githubcli.PullRequest{
		Author: githubcli.PullRequestAuthor{Login: "octocat"},
	})
	if ok {
		t.Fatal("Normalize() ok = true, want false")
	}
}

func TestNormalizeRejectsDependabotLookalikeAuthor(t *testing.T) {
	t.Parallel()

	_, ok := Normalize(githubcli.PullRequest{
		Author: githubcli.PullRequestAuthor{Login: "team/dependabot"},
	})
	if ok {
		t.Fatal("Normalize() ok = true, want false")
	}
}

func TestNormalizeClassifiesDeveloperTooling(t *testing.T) {
	t.Parallel()

	pr, ok := Normalize(githubcli.PullRequest{
		Number:      42,
		Title:       "Bump github.com/golangci/golangci-lint from 1.58.0 to 1.59.0",
		URL:         "https://example.test/pr/42",
		Author:      githubcli.PullRequestAuthor{Login: "dependabot[bot]"},
		Labels:      []githubcli.PullRequestLabel{{Name: "dependencies"}},
		HeadRefName: "dependabot/go_modules/github.com/golangci/golangci-lint-1.59.0",
		BaseRefName: "main",
	})
	if !ok {
		t.Fatal("Normalize() ok = false, want true")
	}

	if pr.Classification.Ecosystem != "go-modules" {
		t.Fatalf("Ecosystem = %q, want go-modules", pr.Classification.Ecosystem)
	}
	if pr.Classification.ChangeKind != ChangeMinor {
		t.Fatalf("ChangeKind = %q, want %q", pr.Classification.ChangeKind, ChangeMinor)
	}
	if !pr.Classification.DeveloperTooling {
		t.Fatal("DeveloperTooling = false, want true")
	}
	wantKeywords := []string{"golangci-lint"}
	if !reflect.DeepEqual(pr.Classification.DevToolingKeywords, wantKeywords) {
		t.Fatalf("DevToolingKeywords = %#v, want %#v", pr.Classification.DevToolingKeywords, wantKeywords)
	}
}

func TestClassifyInfraSensitiveAndGroupedSignals(t *testing.T) {
	t.Parallel()

	infraClassification := classify(
		"Bump docker/login-action from 2.1.0 to 3.0.0",
		"",
		"dependabot/github_actions/docker/login-action-3.0.0",
		[]string{"dependencies"},
	)
	if !infraClassification.CI {
		t.Fatal("CI = false, want true")
	}
	if !infraClassification.InfrastructureSensitive {
		t.Fatal("InfrastructureSensitive = false, want true")
	}
	if infraClassification.ChangeKind != ChangeMajor {
		t.Fatalf("ChangeKind = %q, want %q", infraClassification.ChangeKind, ChangeMajor)
	}

	groupedClassification := classify(
		"Bump the npm_and_yarn group with 3 updates",
		"",
		"dependabot/npm_and_yarn/group-frontend-deps",
		[]string{"dependencies"},
	)
	if !groupedClassification.Grouped {
		t.Fatal("Grouped = false, want true")
	}
	if groupedClassification.DependencyName != "npm and yarn group" {
		t.Fatalf("DependencyName = %q, want npm and yarn group", groupedClassification.DependencyName)
	}
	if groupedClassification.ChangeKind != ChangeUnknown {
		t.Fatalf("ChangeKind = %q, want %q", groupedClassification.ChangeKind, ChangeUnknown)
	}
}

func TestClassifyGroupedTitleVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		title          string
		headRef        string
		wantDependency string
		wantChangeKind ChangeKind
		wantPrevious   string
		wantNext       string
	}{
		{
			name:           "summary title with the",
			title:          "Bump the npm_and_yarn group with 3 updates",
			headRef:        "dependabot/npm_and_yarn/packages-a599cde353",
			wantDependency: "npm and yarn group",
			wantChangeKind: ChangeUnknown,
		},
		{
			name:           "summary title without the",
			title:          "Bump npm_and_yarn group with 3 updates",
			headRef:        "dependabot/npm_and_yarn/packages-a599cde353",
			wantDependency: "npm and yarn group",
			wantChangeKind: ChangeUnknown,
		},
		{
			name:           "lead dependency without versions",
			title:          "Bump lodash in the frontend group",
			headRef:        "dependabot/npm_and_yarn/packages-a599cde353",
			wantDependency: "lodash (frontend group)",
			wantChangeKind: ChangeUnknown,
		},
		{
			name:           "lead dependency with versions",
			title:          "Bump lodash from 4.17.20 to 4.17.21 in the frontend group",
			headRef:        "dependabot/npm_and_yarn/packages-a599cde353",
			wantDependency: "lodash (frontend group)",
			wantChangeKind: ChangePatch,
			wantPrevious:   "4.17.20",
			wantNext:       "4.17.21",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			classification := classify(test.title, "", test.headRef, []string{"dependencies"})
			if !classification.Grouped {
				t.Fatal("Grouped = false, want true")
			}
			if classification.DependencyName != test.wantDependency {
				t.Fatalf("DependencyName = %q, want %q", classification.DependencyName, test.wantDependency)
			}
			if classification.ChangeKind != test.wantChangeKind {
				t.Fatalf("ChangeKind = %q, want %q", classification.ChangeKind, test.wantChangeKind)
			}
			if classification.PreviousVersion != test.wantPrevious {
				t.Fatalf("PreviousVersion = %q, want %q", classification.PreviousVersion, test.wantPrevious)
			}
			if classification.NextVersion != test.wantNext {
				t.Fatalf("NextVersion = %q, want %q", classification.NextVersion, test.wantNext)
			}
		})
	}
}

func TestClassifyConventionalCommitPrefixedGroupedTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		title          string
		headRef        string
		wantDependency string
	}{
		{
			name:           "deps dev grouped summary",
			title:          "build(deps-dev): bump the ci group with 2 updates",
			headRef:        "dependabot/uv/ci-981287ea7a",
			wantDependency: "ci group",
		},
		{
			name:           "deps grouped summary",
			title:          "build(deps): bump the packages group with 5 updates",
			headRef:        "dependabot/uv/packages-a599cde353",
			wantDependency: "packages group",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			classification := classify(test.title, "", test.headRef, []string{"dependencies"})
			if !classification.Grouped {
				t.Fatal("Grouped = false, want true")
			}
			if classification.DependencyName != test.wantDependency {
				t.Fatalf("DependencyName = %q, want %q", classification.DependencyName, test.wantDependency)
			}
			if classification.ChangeKind != ChangeUnknown {
				t.Fatalf("ChangeKind = %q, want %q", classification.ChangeKind, ChangeUnknown)
			}
		})
	}
}

func TestClassifyConventionalCommitPrefixedSingleDependencyTitle(t *testing.T) {
	t.Parallel()

	classification := classify(
		"build(deps): bump actions/cache from latest to stable",
		"",
		"",
		[]string{"github_actions"},
	)
	if classification.Ecosystem != "github-actions" {
		t.Fatalf("Ecosystem = %q, want github-actions", classification.Ecosystem)
	}
	if classification.DependencyName != "actions/cache" {
		t.Fatalf("DependencyName = %q, want actions/cache", classification.DependencyName)
	}
	if classification.Grouped {
		t.Fatal("Grouped = true, want false")
	}
	if classification.ChangeKind != ChangeUnknown {
		t.Fatalf("ChangeKind = %q, want %q", classification.ChangeKind, ChangeUnknown)
	}
}

func TestClassifyFallsBackToHeadRefDependencyInference(t *testing.T) {
	t.Parallel()

	classification := classify(
		"Update dependency metadata",
		"",
		"dependabot/go_modules/github.com/spf13/cobra-1.10.2",
		[]string{"dependencies"},
	)
	if classification.Ecosystem != "go-modules" {
		t.Fatalf("Ecosystem = %q, want go-modules", classification.Ecosystem)
	}
	if classification.DependencyName != "github.com/spf13/cobra" {
		t.Fatalf("DependencyName = %q, want github.com/spf13/cobra", classification.DependencyName)
	}
	if classification.ChangeKind != ChangeUnknown {
		t.Fatalf("ChangeKind = %q, want %q", classification.ChangeKind, ChangeUnknown)
	}
}

func TestClassifyUsesLabelAndTitleEcosystemHints(t *testing.T) {
	t.Parallel()

	classification := classify(
		"Bump actions/checkout from latest to stable",
		"",
		"",
		[]string{"github_actions"},
	)
	if classification.Ecosystem != "github-actions" {
		t.Fatalf("Ecosystem = %q, want github-actions", classification.Ecosystem)
	}
	if classification.DependencyName != "actions/checkout" {
		t.Fatalf("DependencyName = %q, want actions/checkout", classification.DependencyName)
	}
	if classification.ChangeKind != ChangeUnknown {
		t.Fatalf("ChangeKind = %q, want %q", classification.ChangeKind, ChangeUnknown)
	}
}

func TestClassifyGroupedBodyMajorDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                    string
		title                   string
		body                    string
		wantContainsMajorUpdate bool
	}{
		{
			name:  "grouped summary with major bump",
			title: "Bump the npm_and_yarn group with 3 updates",
			body: strings.Join([]string{
				"Updates `next` from 14.2.0 to 15.0.0",
				"Updates `react` from 18.3.0 to 18.3.1",
			}, "\n"),
			wantContainsMajorUpdate: true,
		},
		{
			name:  "grouped summary with only patch and minor bumps",
			title: "Bump the npm_and_yarn group with 2 updates",
			body: strings.Join([]string{
				"Updates `vite` from 5.1.0 to 5.2.0",
				"Updates `lodash` from 4.17.20 to 4.17.21",
			}, "\n"),
			wantContainsMajorUpdate: false,
		},
		{
			name:  "grouped summary with mixed body and one major bump",
			title: "Bump the npm_and_yarn group with 4 updates",
			body: strings.Join([]string{
				"Updates `eslint` from 8.57.0 to 8.57.1",
				"Updates `typescript` from 5.5.4 to 5.6.2",
				"Updates `next` from 14.2.0 to 15.0.0",
			}, "\n"),
			wantContainsMajorUpdate: true,
		},
		{
			name:                    "grouped summary with no parseable versions",
			title:                   "Bump the npm_and_yarn group with 2 updates",
			body:                    "Updates dependencies to the latest versions.",
			wantContainsMajorUpdate: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			classification := classify(test.title, test.body, "dependabot/npm_and_yarn/group-frontend-deps", []string{"dependencies"})
			if !classification.Grouped {
				t.Fatal("Grouped = false, want true")
			}
			if classification.ContainsMajorUpdate != test.wantContainsMajorUpdate {
				t.Fatalf("ContainsMajorUpdate = %t, want %t", classification.ContainsMajorUpdate, test.wantContainsMajorUpdate)
			}
		})
	}
}

func TestHasMajorVersionBump(t *testing.T) {
	t.Parallel()

	directMajor := classify(
		"Bump github.com/foo/bar from 1.9.0 to 2.0.0",
		"",
		"dependabot/go_modules/github.com/foo/bar-2.0.0",
		[]string{"dependencies"},
	)

	tests := []struct {
		name           string
		classification Classification
		want           bool
	}{
		{
			name:           "direct major via change kind",
			classification: directMajor,
			want:           true,
		},
		{
			name: "grouped major via body signal",
			classification: Classification{
				ContainsMajorUpdate: true,
			},
			want: true,
		},
		{
			name: "non-major classification",
			classification: Classification{
				ChangeKind: ChangeMinor,
			},
			want: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := test.classification.HasMajorVersionBump(); got != test.want {
				t.Fatalf("HasMajorVersionBump() = %t, want %t", got, test.want)
			}
		})
	}
}

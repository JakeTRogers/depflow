package dependabot

import (
	"reflect"
	"testing"
)

func TestMarkSecurityUpdates(t *testing.T) {
	t.Parallel()

	groupBody := "Bumps the frontend group with 2 updates:\n\n| Package | From | To |\n| --- | --- | --- |\n| [lodash](https://example.test) | `4.17.20` | `4.17.21` |\n| rust | `1.94-alpine` | `1.98-alpine` |\n"

	tests := []struct {
		name         string
		title        string
		body         string
		headRef      string
		alerts       []Alert
		wantUpdate   bool
		wantSeverity string
	}{
		{name: "title dependency matches", title: "Bump lodash from 4.17.20 to 4.17.21", headRef: "dependabot/npm_and_yarn/lodash-4.17.21", alerts: []Alert{{Ecosystem: "npm", Package: "lodash", Severity: "high"}}, wantUpdate: true, wantSeverity: "high"},
		{name: "case-insensitive package", title: "Bump Newtonsoft.Json from 12.0.1 to 13.0.1", headRef: "dependabot/nuget/Newtonsoft.Json-13.0.1", alerts: []Alert{{Ecosystem: "nuget", Package: "newtonsoft.json", Severity: "medium"}}, wantUpdate: true, wantSeverity: "medium"},
		{name: "python names normalized", title: "Bump python_dateutil from 2.8.0 to 2.9.0", headRef: "dependabot/pip/python-dateutil-2.9.0", alerts: []Alert{{Ecosystem: "pip", Package: "python-dateutil", Severity: "low"}}, wantUpdate: true, wantSeverity: "low"},
		{name: "other package", title: "Bump lodash from 4.17.20 to 4.17.21", headRef: "dependabot/npm_and_yarn/lodash-4.17.21", alerts: []Alert{{Ecosystem: "npm", Package: "axios", Severity: "high"}}},
		{name: "same name in another ecosystem", title: "Bump requests from 2.31.0 to 2.32.0", headRef: "dependabot/pip/requests-2.32.0", alerts: []Alert{{Ecosystem: "npm", Package: "requests", Severity: "high"}}},
		{name: "highest severity wins", title: "Bump lodash from 4.17.20 to 4.17.21", headRef: "dependabot/npm_and_yarn/lodash-4.17.21", alerts: []Alert{{Ecosystem: "npm", Package: "lodash", Severity: "medium"}, {Ecosystem: "npm", Package: "lodash", Severity: "CRITICAL"}, {Ecosystem: "npm", Package: "lodash", Severity: "low"}}, wantUpdate: true, wantSeverity: "critical"},
		{name: "grouped body dependency matches", title: "Bump the frontend group with 2 updates", body: groupBody, headRef: "dependabot/npm_and_yarn/frontend-abc123", alerts: []Alert{{Ecosystem: "npm", Package: "lodash", Severity: "high"}}, wantUpdate: true, wantSeverity: "high"},
		{name: "no alerts", title: "Bump lodash from 4.17.20 to 4.17.21", headRef: "dependabot/npm_and_yarn/lodash-4.17.21"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			prs := []PR{{Number: 1, Classification: classify(test.title, test.body, test.headRef, nil)}}
			MarkSecurityUpdates(prs, test.alerts)
			got := prs[0].Classification.Security
			want := SecurityStatus{Checked: true, Update: test.wantUpdate, Severity: test.wantSeverity}
			if got != want {
				t.Fatalf("Security = %+v, want %+v", got, want)
			}
		})
	}
}

func TestClassifyGroupedDependencies(t *testing.T) {
	t.Parallel()

	body := "Bumps the deps group with 3 updates:\n\n| Package | From | To |\n| --- | --- | --- |\n| [@hookform/resolvers](https://example.test) | `5.7.1` | `5.9.1` |\n| rust | `1.94-alpine` | `1.98-alpine` |\n\nUpdates `@hookform/resolvers` from 5.7.1 to 5.9.1\n\nUpdates `zod` from 4.4.3 to 4.6.5\n"
	classification := classify("Bump lodash from 4.17.20 to 4.17.21 in the deps group", body, "dependabot/npm_and_yarn/deps-abc123", nil)
	want := []string{"lodash", "@hookform/resolvers", "zod", "rust"}
	if !reflect.DeepEqual(classification.Dependencies, want) {
		t.Fatalf("Dependencies = %v, want %v", classification.Dependencies, want)
	}

	single := classify("Bump lodash from 4.17.20 to 4.17.21", "", "dependabot/npm_and_yarn/lodash-4.17.21", nil)
	if !reflect.DeepEqual(single.Dependencies, []string{"lodash"}) {
		t.Fatalf("single Dependencies = %v, want [lodash]", single.Dependencies)
	}
}

func TestFilterSecurityOnly(t *testing.T) {
	t.Parallel()

	prs := []PR{
		newFilterPR(1, "security", false, nil, Classification{Security: SecurityStatus{Checked: true, Update: true, Severity: "high"}}),
		newFilterPR(2, "not security", false, nil, Classification{Security: SecurityStatus{Checked: true}}),
	}
	included, excluded := Filter(prs, FilterOptions{SecurityOnly: true})
	if got := includedNumbers(included); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("included = %v, want [1]", got)
	}
	if len(excluded) != 1 || excluded[0].Reason != "not a security update (--security-only)" {
		t.Fatalf("excluded = %+v", excluded)
	}
}

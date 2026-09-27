package planfile

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/planner"
)

func plannedPR(number int, bucket planner.Bucket, title string) planner.PlannedPR {
	return planner.PlannedPR{
		PR:     dependabot.PR{Number: number, Title: title},
		Bucket: bucket,
	}
}

func TestWriteRendersPicksThenSkips(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	generated := time.Date(2026, 9, 27, 14, 3, 0, 0, time.UTC)
	picks := []planner.PlannedPR{
		plannedPR(31, planner.BucketCI, "Bump actions/checkout from 7.0.0 to 7.0.1"),
		plannedPR(22, planner.BucketMinor, "Bump github.com/spf13/cobra from 1.9.0 to 1.10.2"),
	}
	skips := []Skipped{{
		Item:   plannedPR(40, planner.BucketMajor, "Bump foo from 1.4.0 to 2.0.0"),
		Reason: `change-kind "major" not in --change-kind allow-list`,
	}}

	if err := Write(&out, "owner/repo", generated, picks, skips); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"repo owner/repo\n",
		"# depflow plan, generated 2026-09-27T14:03:00Z\n",
		"\npick #31 [ci] Bump actions/checkout from 7.0.0 to 7.0.1\npick #22 [minor] Bump github.com/spf13/cobra from 1.9.0 to 1.10.2\n",
		"# Excluded by default filters. Change \"skip\" to \"pick\" to include:\n",
		"skip #40 [major] Bump foo from 1.4.0 to 2.0.0  # change-kind \"major\" not in --change-kind allow-list\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Write() output missing %q:\n%s", want, got)
		}
	}
	if !strings.HasPrefix(got, "repo owner/repo\n") {
		t.Fatalf("Write() output should start with the repo line:\n%s", got)
	}
}

func TestWriteOmitsSkipSectionWhenNoSkips(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := Write(&out, "owner/repo", time.Now(), []planner.PlannedPR{plannedPR(1, planner.BucketPatch, "patch")}, nil); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if strings.Contains(out.String(), "Excluded by default filters") {
		t.Fatalf("Write() output should not include skip section:\n%s", out.String())
	}
}

func TestWriteKeepsHostileTitlesOnOneLine(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	title := "Bump evil\npick #99 [patch] injected\x1b[31m\ttabbed"
	if err := Write(&out, "owner/repo", time.Now(), []planner.PlannedPR{plannedPR(7, planner.BucketPatch, title)}, nil); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("Write() output contains escape byte: %q", out.String())
	}
	if !strings.Contains(out.String(), "pick #7 [patch] Bump evil pick #99 [patch] injected[31m tabbed\n") {
		t.Fatalf("Write() did not collapse title to one line: %q", out.String())
	}

	file, err := Parse(&out)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(file.Entries) != 1 || file.Entries[0].Number != 7 {
		t.Fatalf("Parse() entries = %#v, want only #7", file.Entries)
	}
}

func TestWriteParseRoundTrip(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	picks := []planner.PlannedPR{
		plannedPR(3, planner.BucketPatch, "patch"),
		plannedPR(1, planner.BucketCI, "ci"),
	}
	skips := []Skipped{{Item: plannedPR(9, planner.BucketMajor, "major"), Reason: "excluded"}}
	if err := Write(&out, "github.com/owner/repo", time.Now(), picks, skips); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	file, err := Parse(&out)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if file.Repo != "github.com/owner/repo" {
		t.Fatalf("Repo = %q", file.Repo)
	}
	want := []Entry{
		{Action: ActionPick, Number: 3, Bucket: "patch"},
		{Action: ActionPick, Number: 1, Bucket: "ci"},
		{Action: ActionSkip, Number: 9, Bucket: "major"},
	}
	if got := stripLines(file.Entries); !reflect.DeepEqual(got, want) {
		t.Fatalf("Entries = %#v, want %#v", got, want)
	}
}

func TestParseAcceptsVerbAliasesAndBareNumbers(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"# comment",
		"",
		"repo owner/repo",
		"PICK #4 [minor] title",
		"p 5",
		"   s #6 whatever",
		"drop 7",
		"d #8 [patch]",
		"pick 9 []",
	}, "\n")

	file, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	want := []Entry{
		{Action: ActionPick, Number: 4, Bucket: "minor", Line: 4},
		{Action: ActionPick, Number: 5, Line: 5},
		{Action: ActionSkip, Number: 6, Line: 6},
		{Action: ActionSkip, Number: 7, Line: 7},
		{Action: ActionSkip, Number: 8, Bucket: "patch", Line: 8},
		{Action: ActionPick, Number: 9, Line: 9},
	}
	if !reflect.DeepEqual(file.Entries, want) {
		t.Fatalf("Entries = %#v, want %#v", file.Entries, want)
	}

	picks := file.Picks()
	if len(picks) != 3 || picks[0].Number != 4 || picks[1].Number != 5 || picks[2].Number != 9 {
		t.Fatalf("Picks() = %#v, want #4, #5, #9", picks)
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing repo", input: "pick #1\n", want: `missing "repo OWNER/REPO" line`},
		{name: "duplicate repo", input: "repo a/b\nrepo c/d\n", want: "line 2: repo already set"},
		{name: "repo without value", input: "repo\n", want: `line 1: want "repo OWNER/REPO"`},
		{name: "repo with extra fields", input: "repo a/b c\n", want: `line 1: want "repo OWNER/REPO"`},
		{name: "unknown command", input: "repo a/b\nsquash #1\n", want: `line 2: unknown command "squash" (want pick or skip)`},
		{name: "missing number", input: "repo a/b\npick\n", want: "line 2: missing PR number"},
		{name: "non-numeric number", input: "repo a/b\npick #abc\n", want: `line 2: invalid PR number "#abc"`},
		{name: "zero number", input: "repo a/b\npick 0\n", want: `line 2: invalid PR number "0"`},
		{name: "negative number", input: "repo a/b\npick -3\n", want: `line 2: invalid PR number "-3"`},
		{name: "duplicate number", input: "repo a/b\npick #1\nskip 1\n", want: "line 3: PR #1 already listed on line 2"},
		{name: "line too long", input: "repo a/b\npick #1 " + strings.Repeat("x", maxLineBytes) + "\n", want: "reading plan file"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(strings.NewReader(test.input))
			if err == nil {
				t.Fatalf("Parse() error = nil, want %q", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse() error = %q, want it to contain %q", err, test.want)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("disk full")
}

func TestWritePropagatesWriterError(t *testing.T) {
	t.Parallel()

	err := Write(failingWriter{}, "owner/repo", time.Now(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Write() error = %v, want disk full", err)
	}
}

func stripLines(entries []Entry) []Entry {
	stripped := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		entry.Line = 0
		stripped = append(stripped, entry)
	}
	return stripped
}

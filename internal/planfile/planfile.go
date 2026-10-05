// Package planfile renders and parses editable depflow plan files.
//
// A plan file lists one PR per line in execution order, in the style of a git rebase todo list:
//
//	repo owner/repo
//	pick #31 [ci] Bump actions/checkout from 7.0.0 to 7.0.1
//	skip #40 [major] Bump foo from 1.4.0 to 2.0.0
//
// Only the command, PR number, and optional [bucket] token are read back; the rest of each line
// is informational.
package planfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/JakeTRogers/depflow/internal/terminal"
)

// Action is the command applied to a PR line.
type Action string

const (
	// ActionPick processes the PR.
	ActionPick Action = "pick"
	// ActionSkip leaves the PR alone.
	ActionSkip Action = "skip"
)

const repoDirective = "repo"

// maxLineBytes bounds a single plan file line; generated lines are far shorter.
const maxLineBytes = 1024 * 1024

// Entry is one PR line from a plan file.
type Entry struct {
	Action Action
	Number int
	// Bucket is the planner bucket recorded when the plan was written, or empty if absent.
	Bucket string
	Line   int
}

// File is a parsed plan file.
type File struct {
	Repo    string
	Entries []Entry
}

// Picks returns the picked entries in file order.
func (f File) Picks() []Entry {
	picks := make([]Entry, 0, len(f.Entries))
	for _, entry := range f.Entries {
		if entry.Action == ActionPick {
			picks = append(picks, entry)
		}
	}
	return picks
}

// Skipped is a PR rendered as a skip line together with the reason it was left out.
type Skipped struct {
	Item   planner.PlannedPR
	Reason string
}

// Write renders an editable plan for repo with picks in execution order followed by skips.
func Write(w io.Writer, repo string, generated time.Time, picks []planner.PlannedPR, skips []Skipped) error {
	var builder strings.Builder

	fmt.Fprintf(&builder, "%s %s\n", repoDirective, oneLine(repo))
	builder.WriteString("#\n")
	fmt.Fprintf(&builder, "# depflow plan, generated %s\n", generated.UTC().Format(time.RFC3339))
	builder.WriteString("# Lines run top to bottom; reorder them to change the order.\n")
	builder.WriteString("#   pick, p          = process this PR\n")
	builder.WriteString("#   skip, s, drop, d = leave this PR alone (deleting the line also skips it)\n")
	builder.WriteString("# Save with no pick lines to abort.\n")

	if len(picks) > 0 {
		builder.WriteString("\n")
	}
	for _, item := range picks {
		writeLine(&builder, ActionPick, item, "")
	}

	if len(skips) > 0 {
		builder.WriteString("\n# Not included by default. Change \"skip\" to \"pick\" to include:\n")
	}
	for _, skipped := range skips {
		writeLine(&builder, ActionSkip, skipped.Item, skipped.Reason)
	}

	if _, err := io.WriteString(w, builder.String()); err != nil {
		return fmt.Errorf("writing plan file: %w", err)
	}
	return nil
}

func writeLine(builder *strings.Builder, action Action, item planner.PlannedPR, note string) {
	fmt.Fprintf(builder, "%s #%d [%s] %s", action, item.PR.Number, item.Bucket, oneLine(item.PR.Title))
	if security := item.PR.Classification.Security; security.Update {
		severity := security.Severity
		if severity == "" {
			severity = "unknown severity"
		}
		note = strings.TrimPrefix(note+"; security: "+severity, "; ")
	}
	if note = oneLine(note); note != "" {
		fmt.Fprintf(builder, "  # %s", note)
	}
	builder.WriteString("\n")
}

// oneLine strips terminal control bytes and collapses all whitespace so GitHub-derived text
// cannot break the one-PR-per-line format.
func oneLine(value string) string {
	return strings.Join(strings.Fields(terminal.Sanitize(value)), " ")
}

// Parse reads a plan file. It rejects unknown commands, malformed or duplicate PR numbers,
// and files without a repo line.
func Parse(r io.Reader) (File, error) {
	var file File
	seen := make(map[int]int)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), maxLineBytes)

	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}

		command := strings.ToLower(fields[0])
		if command == repoDirective {
			if file.Repo != "" {
				return File{}, fmt.Errorf("line %d: repo already set", lineNumber)
			}
			if len(fields) != 2 {
				return File{}, fmt.Errorf("line %d: want \"repo OWNER/REPO\"", lineNumber)
			}
			file.Repo = fields[1]
			continue
		}

		action, ok := parseAction(command)
		if !ok {
			return File{}, fmt.Errorf("line %d: unknown command %q (want pick or skip)", lineNumber, fields[0])
		}
		if len(fields) < 2 {
			return File{}, fmt.Errorf("line %d: missing PR number", lineNumber)
		}

		number, err := strconv.Atoi(strings.TrimPrefix(fields[1], "#"))
		if err != nil || number < 1 {
			return File{}, fmt.Errorf("line %d: invalid PR number %q", lineNumber, fields[1])
		}
		if previous, ok := seen[number]; ok {
			return File{}, fmt.Errorf("line %d: PR #%d already listed on line %d", lineNumber, number, previous)
		}
		seen[number] = lineNumber

		file.Entries = append(file.Entries, Entry{
			Action: action,
			Number: number,
			Bucket: parseBucket(fields[2:]),
			Line:   lineNumber,
		})
	}
	if err := scanner.Err(); err != nil {
		return File{}, fmt.Errorf("reading plan file: %w", err)
	}

	if file.Repo == "" {
		return File{}, errors.New("missing \"repo OWNER/REPO\" line")
	}

	return file, nil
}

func parseAction(command string) (Action, bool) {
	switch command {
	case "pick", "p":
		return ActionPick, true
	case "skip", "s", "drop", "d":
		return ActionSkip, true
	default:
		return "", false
	}
}

func parseBucket(rest []string) string {
	if len(rest) == 0 {
		return ""
	}
	token := rest[0]
	if len(token) < 3 || !strings.HasPrefix(token, "[") || !strings.HasSuffix(token, "]") {
		return ""
	}
	return token[1 : len(token)-1]
}

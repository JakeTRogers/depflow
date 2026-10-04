package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/planfile"
	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/spf13/cobra"
)

const (
	noPickLinesMessage     = "Nothing to do: plan has no pick lines."
	noOpenPickedPRsMessage = "Nothing to do: none of the picked PRs can be processed."
)

// planFileConflictingFlags shape which PRs a generated plan contains, so they have no meaning
// when executing a saved plan.
var planFileConflictingFlags = []string{
	"limit",
	"ecosystem",
	"exclude-ecosystem",
	"dependency",
	"exclude-dependency",
	"require-label",
	"exclude-label",
	"skip-grouped",
	"change-kind",
	"include-drafts",
}

func rejectFilterFlagsWithPlanFile(cmd *cobra.Command) error {
	for _, name := range planFileConflictingFlags {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("flag --%s cannot be used with --plan; pass filters to `depflow plan -o FILE` when generating the plan", name)
		}
	}
	return nil
}

type planFileContents struct {
	picks []planner.PlannedPR
	skips []planfile.Skipped
}

func (c planFileContents) empty() bool {
	return len(c.picks) == 0 && len(c.skips) == 0
}

// buildPlanFileContents plans prs with the user's filters. PRs held back only by the default
// change-kind or draft filters become skip lines so an explicit edit can include them; PRs
// excluded by filters the user typed are left out of the file entirely.
func buildPlanFileContents(cmd *cobra.Command, prs []dependabot.PR, opts *commandOptions, changeKinds []dependabot.ChangeKind, includeDrafts bool) planFileContents {
	filterOpts := buildFilterOptions(opts, changeKinds, includeDrafts, true)
	included, excluded := dependabot.Filter(prs, filterOpts)
	included = applyLimit(included, opts)

	candidateOpts := filterOpts
	if !cmd.Flags().Changed("change-kind") {
		candidateOpts.ChangeKinds = nil
	}
	if !cmd.Flags().Changed("include-drafts") {
		candidateOpts.IncludeDrafts = true
	}
	candidates, _ := dependabot.Filter(prs, candidateOpts)

	reasons := make(map[int]string, len(excluded))
	for _, ex := range excluded {
		reasons[ex.PR.Number] = ex.Reason
	}

	var skipPRs []dependabot.PR
	for _, pr := range candidates {
		if _, ok := reasons[pr.Number]; ok {
			skipPRs = append(skipPRs, pr)
		}
	}

	skipPlan := planner.Build(skipPRs)
	skips := make([]planfile.Skipped, 0, len(skipPlan.Items))
	for _, item := range skipPlan.Items {
		skips = append(skips, planfile.Skipped{Item: item, Reason: reasons[item.PR.Number]})
	}

	return planFileContents{picks: planner.Build(included).Items, skips: skips}
}

type bucketDrift struct {
	number int
	was    string
	now    planner.Bucket
}

type planFileResolution struct {
	plan    planner.Plan
	picked  int
	dropped []int
	drifted []bucketDrift
	// blocked lists picks that became major updates after the plan was written. They are not
	// processed unless the file records them as [major], so a new major bump is never merged on
	// the strength of an approval given to a smaller one.
	blocked  []bucketDrift
	unlisted int
}

// resolvePlanFile turns the pick lines of file into an ordered plan. Only open Dependabot PRs
// from prs can be picked, so a stale or hand-edited file can never cause an unexpected merge.
func resolvePlanFile(file planfile.File, repo string, prs []dependabot.PR) (planFileResolution, error) {
	if !sameRepo(file.Repo, repo) {
		return planFileResolution{}, fmt.Errorf("plan file is for %s but the target repository is %s; rerun with --repo %s or regenerate the plan", file.Repo, repo, file.Repo)
	}

	byNumber := make(map[int]dependabot.PR, len(prs))
	for _, pr := range prs {
		byNumber[pr.Number] = pr
	}

	picks := file.Picks()
	res := planFileResolution{picked: len(picks)}
	selected := make([]dependabot.PR, 0, len(picks))
	order := make([]int, 0, len(picks))
	recordedBuckets := make(map[int]string, len(picks))
	for _, entry := range picks {
		pr, ok := byNumber[entry.Number]
		if !ok {
			res.dropped = append(res.dropped, entry.Number)
			continue
		}
		selected = append(selected, pr)
		order = append(order, entry.Number)
		recordedBuckets[entry.Number] = entry.Bucket
	}

	ordered := planner.BuildOrdered(selected, order)
	for _, item := range ordered.Items {
		was := recordedBuckets[item.PR.Number]
		if was == "" || strings.EqualFold(was, string(item.Bucket)) {
			res.plan.Items = append(res.plan.Items, item)
			continue
		}
		drift := bucketDrift{number: item.PR.Number, was: was, now: item.Bucket}
		if item.Bucket == planner.BucketMajor {
			res.blocked = append(res.blocked, drift)
			continue
		}
		res.drifted = append(res.drifted, drift)
		res.plan.Items = append(res.plan.Items, item)
	}

	listed := make(map[int]bool, len(file.Entries))
	for _, entry := range file.Entries {
		listed[entry.Number] = true
	}
	for _, pr := range prs {
		if !listed[pr.Number] {
			res.unlisted++
		}
	}

	return res, nil
}

// sameRepo compares [HOST/]OWNER/REPO values case-insensitively. Hosts are compared only when
// both values include one.
func sameRepo(left, right string) bool {
	leftParts := repoParts(left)
	rightParts := repoParts(right)
	if len(leftParts) < 2 || len(rightParts) < 2 {
		return false
	}
	if len(leftParts) > 2 && len(rightParts) > 2 {
		return slices.Equal(leftParts, rightParts)
	}
	return slices.Equal(leftParts[len(leftParts)-2:], rightParts[len(rightParts)-2:])
}

func repoParts(repo string) []string {
	repo = strings.Trim(strings.ToLower(strings.TrimSpace(repo)), "/")
	if repo == "" {
		return nil
	}
	return strings.Split(repo, "/")
}

func writePlanFileNotices(w io.Writer, res planFileResolution, reportUnlisted bool) error {
	var builder strings.Builder
	if len(res.dropped) > 0 {
		fmt.Fprintf(&builder, "Not processed (not an open Dependabot PR): %s\n", formatPRNumbers(res.dropped))
	}
	for _, drift := range res.blocked {
		fmt.Fprintf(&builder, "Not processed: #%d is now [major], was [%s] when planned; change its bucket to [major] in the plan file to include it\n", drift.number, sanitize(drift.was))
	}
	for _, drift := range res.drifted {
		fmt.Fprintf(&builder, "Warning: #%d is now [%s], was [%s] when planned\n", drift.number, drift.now, sanitize(drift.was))
	}
	if reportUnlisted && res.unlisted > 0 {
		fmt.Fprintf(&builder, "Left alone: %d open Dependabot PR(s) not listed in the plan file\n", res.unlisted)
	}
	if builder.Len() == 0 {
		return nil
	}
	builder.WriteString("\n")

	if _, err := io.WriteString(w, builder.String()); err != nil {
		return fmt.Errorf("writing plan file notices: %w", err)
	}
	return nil
}

func formatPRNumbers(numbers []int) string {
	formatted := make([]string, 0, len(numbers))
	for _, number := range numbers {
		formatted = append(formatted, fmt.Sprintf("#%d", number))
	}
	return strings.Join(formatted, ", ")
}

// planFromFile reads a saved plan from path ("-" for stdin) and resolves it against the
// currently open Dependabot PRs.
func planFromFile(cmd *cobra.Command, deps commandDeps, opts *commandOptions, path string) (planFileResolution, string, error) {
	file, err := readPlanFile(cmd, path)
	if err != nil {
		return planFileResolution{}, "", err
	}

	repo, err := resolveRepo(cmd.Context(), deps, opts.repo)
	if err != nil {
		return planFileResolution{}, "", err
	}

	prs, err := discoverDependabotPRs(cmd.Context(), deps, opts)
	if err != nil {
		return planFileResolution{}, "", err
	}

	res, err := resolvePlanFile(file, repo, prs)
	return res, repo, err
}

func readPlanFile(cmd *cobra.Command, path string) (planfile.File, error) {
	if path == "-" {
		file, err := planfile.Parse(cmd.InOrStdin())
		if err != nil {
			return planfile.File{}, fmt.Errorf("reading plan from stdin: %w", err)
		}
		return file, nil
	}

	file, err := parsePlanFilePath(path)
	if err != nil {
		return planfile.File{}, fmt.Errorf("reading plan file %s: %w", path, err)
	}
	return file, nil
}

func parsePlanFilePath(path string) (file planfile.File, err error) {
	reader, err := os.Open(path)
	if err != nil {
		return planfile.File{}, err
	}
	defer func() {
		err = errors.Join(err, reader.Close())
	}()

	return planfile.Parse(reader)
}

// planFromEditor renders the current plan to a temporary file, lets the user edit it, and
// resolves the result. ok is false when there was nothing to edit and a message was printed.
func planFromEditor(cmd *cobra.Command, deps commandDeps, opts *commandOptions, changeKinds []dependabot.ChangeKind, includeDrafts bool) (res planFileResolution, repo string, ok bool, err error) {
	prs, err := discoverDependabotPRs(cmd.Context(), deps, opts)
	if err != nil {
		return planFileResolution{}, "", false, err
	}
	if len(prs) == 0 {
		return planFileResolution{}, "", false, printLine(cmd.OutOrStdout(), noOpenDependabotPRsMessage)
	}

	contents := buildPlanFileContents(cmd, prs, opts, changeKinds, includeDrafts)
	if contents.empty() {
		return planFileResolution{}, "", false, printLine(cmd.OutOrStdout(), noEligiblePRsMessage)
	}

	if deps.editor == nil {
		return planFileResolution{}, "", false, errors.New("editing plan: no editor configured")
	}

	repo, err = resolveRepo(cmd.Context(), deps, opts.repo)
	if err != nil {
		return planFileResolution{}, "", false, err
	}

	path, err := writeTempPlanFile(repo, contents)
	if err != nil {
		return planFileResolution{}, "", false, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()

	if err := deps.editor.Edit(cmd.Context(), path); err != nil {
		return planFileResolution{}, "", false, fmt.Errorf("editing plan: %w", err)
	}

	file, err := parsePlanFilePath(path)
	if err == nil {
		res, err = resolvePlanFile(file, repo, prs)
	}
	if err != nil {
		keep = true
		return planFileResolution{}, "", false, fmt.Errorf("edited plan: %w (kept at %s; fix it and rerun with --plan %s)", err, path, path)
	}

	return res, repo, true, nil
}

func writeTempPlanFile(repo string, contents planFileContents) (string, error) {
	file, err := os.CreateTemp("", "depflow-plan-*.txt")
	if err != nil {
		return "", fmt.Errorf("creating plan file: %w", err)
	}

	writeErr := planfile.Write(file, repo, time.Now(), contents.picks, contents.skips)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("saving plan file: %w", err)
	}

	return file.Name(), nil
}

func printLine(w io.Writer, line string) error {
	if _, err := fmt.Fprintln(w, line); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

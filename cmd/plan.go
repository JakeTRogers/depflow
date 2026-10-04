package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/planfile"
	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/spf13/cobra"
)

type planOptions struct {
	changeKind    []string
	includeDrafts bool
	details       bool
	output        string
	force         bool
}

func newPlanCommand(deps commandDeps, opts *commandOptions) *cobra.Command {
	planOpts := &planOptions{}

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show the deterministic Dependabot processing order",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if planOpts.force && (planOpts.output == "" || planOpts.output == "-") {
				return errors.New("--force only applies when writing a plan file with --output FILE")
			}
			changeKinds, err := parseChangeKinds(planOpts.changeKind)
			if err != nil {
				return err
			}

			prs, err := discoverDependabotPRs(cmd.Context(), deps, opts)
			if err != nil {
				return err
			}
			if err := warnUnmatchedEcosystems(cmd.ErrOrStderr(), prs, opts); err != nil {
				return err
			}

			if planOpts.output != "" {
				return writePlanFile(cmd, deps, opts, prs, changeKinds, planOpts)
			}

			filterOpts := buildFilterOptions(opts, changeKinds, planOpts.includeDrafts, true)
			included, excluded := dependabot.Filter(prs, filterOpts)
			plan, overLimit := limitPlan(planner.Build(included), opts)
			if len(plan.Items) == 0 && len(excluded) == 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), noOpenDependabotPRsMessage); err != nil {
					return fmt.Errorf("writing plan output: %w", err)
				}
				return nil
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Planned order for %d Dependabot pull request(s)\n\n", len(plan.Items)); err != nil {
				return fmt.Errorf("writing plan header: %w", err)
			}

			if err := writePlanItems(cmd.OutOrStdout(), plan, planOpts.details); err != nil {
				return err
			}

			if len(excluded) > 0 {
				if len(plan.Items) > 0 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
						return fmt.Errorf("writing plan separator: %w", err)
					}
				}
				if err := writeExcludedPRs(cmd.OutOrStdout(), excludedPRsHeading, excluded); err != nil {
					return err
				}
			}

			if len(overLimit) > 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
					return fmt.Errorf("writing plan output spacing: %w", err)
				}
			}
			return writeLimitNotice(cmd.OutOrStdout(), len(overLimit), opts)
		},
	}

	cmd.Flags().StringSliceVar(&planOpts.changeKind, "change-kind", defaultChangeKindValues, "include only these change kinds: patch, minor, major, unknown, or all")
	if err := cmd.RegisterFlagCompletionFunc("change-kind", changeKindCompletions); err != nil {
		panic(err)
	}
	cmd.Flags().BoolVar(&planOpts.includeDrafts, "include-drafts", false, "include draft Dependabot PRs in planning")
	cmd.Flags().BoolVar(&planOpts.details, "details", false, "show full titles, classification signals, reasons, and URLs instead of the compact table")
	cmd.Flags().StringVarP(&planOpts.output, "output", "o", "", "write an editable plan `FILE` for depflow execute --plan (- for stdout)")
	cmd.Flags().BoolVar(&planOpts.force, "force", false, "overwrite an existing --output file")
	cmd.MarkFlagsMutuallyExclusive("details", "output")

	return cmd
}

func writePlanItems(writer io.Writer, plan planner.Plan, details bool) error {
	if !details {
		return writeCompactPlan(writer, plan)
	}
	for index, item := range plan.Items {
		if err := writePlannedPR(writer, index+1, item); err != nil {
			return err
		}
		if index+1 < len(plan.Items) {
			if _, err := fmt.Fprintln(writer); err != nil {
				return fmt.Errorf("writing plan separator: %w", err)
			}
		}
	}
	return nil
}

func writeCompactPlan(writer io.Writer, plan planner.Plan) error {
	if len(plan.Items) == 0 {
		return nil
	}
	var builder strings.Builder
	builder.WriteString("ORDER\tPR\tBUCKET\tECOSYSTEM\tDEPENDENCY\tCHANGE\n")
	for index, item := range plan.Items {
		classification := item.PR.Classification
		dependency := classification.DependencyName
		if strings.TrimSpace(dependency) == "" {
			dependency = item.PR.Title
		}
		fmt.Fprintf(&builder, "%d\t#%d\t%s\t%s\t%s\t%s\n", index+1, item.PR.Number,
			planCell(string(item.Bucket)), planCell(classification.Ecosystem),
			planCell(dependency), planCell(string(classification.EffectiveChangeKind())))
	}
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	if _, err := io.WriteString(table, builder.String()); err != nil {
		return fmt.Errorf("writing compact plan: %w", err)
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("writing compact plan: %w", err)
	}
	return nil
}

func planCell(value string) string {
	value = strings.Join(strings.Fields(sanitize(value)), " ")
	if value == "" {
		return "unknown"
	}
	return value
}

func writePlannedPR(writer io.Writer, index int, item planner.PlannedPR) error {
	classification := item.PR.Classification
	title := sanitize(item.PR.Title)
	dependencyName := sanitize(classification.DependencyName)
	reason := sanitize(item.Reason)
	url := sanitize(item.PR.URL)

	var builder strings.Builder
	fmt.Fprintf(&builder, "%d. #%d [%s] %s\n", index, item.PR.Number, item.Bucket, title)
	fmt.Fprintf(&builder, "   signals: ecosystem=%s change=%s grouped=%s dev-tooling=%s infra-sensitive=%s\n",
		displayOrUnknown(classification.Ecosystem),
		classification.EffectiveChangeKind(),
		yesNo(classification.Grouped),
		yesNo(classification.DeveloperTooling),
		yesNo(classification.InfrastructureSensitive))
	if dependencyName != "" {
		fmt.Fprintf(&builder, "   dependency: %s\n", dependencyName)
	}
	fmt.Fprintf(&builder, "   reason: %s\n", reason)
	fmt.Fprintf(&builder, "   url: %s\n", url)

	if _, err := io.WriteString(writer, builder.String()); err != nil {
		return fmt.Errorf("writing plan output: %w", err)
	}

	return nil
}

// writePlanFile saves an editable plan to planOpts.output ("-" for stdout). Status messages go
// to stderr when the plan itself is written to stdout.
func writePlanFile(cmd *cobra.Command, deps commandDeps, opts *commandOptions, prs []dependabot.PR, changeKinds []dependabot.ChangeKind, planOpts *planOptions) error {
	toStdout := planOpts.output == "-"
	status := cmd.OutOrStdout()
	if toStdout {
		status = cmd.ErrOrStderr()
	}

	contents := buildPlanFileContents(cmd, prs, opts, changeKinds, planOpts.includeDrafts)
	if contents.empty() {
		if len(prs) == 0 {
			return printLine(status, noOpenDependabotPRsMessage)
		}
		return printLine(status, noEligiblePRsMessage)
	}

	repo, err := resolveRepo(cmd.Context(), deps, opts.repo)
	if err != nil {
		return err
	}

	if toStdout {
		return planfile.Write(cmd.OutOrStdout(), repo, time.Now(), contents.picks, contents.skips)
	}

	// O_EXCL refuses an existing file in the same step that creates it, so a hand-edited plan is
	// never replaced unless --force asks for that.
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if planOpts.force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(planOpts.output, flags, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("plan file %s already exists; pass --force to overwrite it", planOpts.output)
	}
	if err != nil {
		return fmt.Errorf("creating plan file: %w", err)
	}
	writeErr := planfile.Write(file, repo, time.Now(), contents.picks, contents.skips)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("saving plan file %s: %w", planOpts.output, err)
	}

	summary := fmt.Sprintf("Wrote plan for %d PR(s) to %s", len(contents.picks), planOpts.output)
	if len(contents.skips) > 0 {
		summary += fmt.Sprintf(" (%d more listed as skip)", len(contents.skips))
	}
	repoFlag := ""
	if opts.repo != "" {
		repoFlag = " --repo " + opts.repo
	}
	if _, err := fmt.Fprintf(status, "%s.\nEdit it, then run: depflow%s execute --plan %s\n", summary, repoFlag, planOpts.output); err != nil {
		return fmt.Errorf("writing plan output: %w", err)
	}
	return nil
}

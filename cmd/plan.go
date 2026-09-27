package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/planfile"
	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/spf13/cobra"
)

type planOptions struct {
	changeKind    []string
	includeDrafts bool
	output        string
}

func newPlanCommand(deps commandDeps, opts *commandOptions) *cobra.Command {
	planOpts := &planOptions{}

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show the deterministic Dependabot processing order",
		RunE: func(cmd *cobra.Command, _ []string) error {
			changeKinds, err := parseChangeKinds(planOpts.changeKind)
			if err != nil {
				return err
			}

			prs, err := discoverDependabotPRs(cmd.Context(), deps, opts)
			if err != nil {
				return err
			}

			if planOpts.output != "" {
				return writePlanFile(cmd, deps, opts, prs, changeKinds, planOpts)
			}

			filterOpts := buildFilterOptions(opts, changeKinds, planOpts.includeDrafts, true)
			included, excluded := dependabot.Filter(prs, filterOpts)
			included = applyLimit(included, opts)
			plan := planner.Build(included)
			if len(plan.Items) == 0 && len(excluded) == 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), noOpenDependabotPRsMessage); err != nil {
					return fmt.Errorf("writing plan output: %w", err)
				}
				return nil
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Planned order for %d Dependabot pull request(s)\n\n", len(plan.Items)); err != nil {
				return fmt.Errorf("writing plan header: %w", err)
			}

			for index, item := range plan.Items {
				if err := writePlannedPR(cmd.OutOrStdout(), index+1, item); err != nil {
					return err
				}
				if index+1 < len(plan.Items) {
					if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
						return fmt.Errorf("writing plan separator: %w", err)
					}
				}
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

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&planOpts.changeKind, "change-kind", defaultChangeKindValues, "include only these change kinds: patch, minor, major, unknown, or all")
	if err := cmd.RegisterFlagCompletionFunc("change-kind", changeKindCompletions); err != nil {
		panic(err)
	}
	cmd.Flags().BoolVar(&planOpts.includeDrafts, "include-drafts", false, "include draft Dependabot PRs in planning")
	cmd.Flags().StringVarP(&planOpts.output, "output", "o", "", "write an editable plan file for `depflow execute --plan` (- for stdout)")

	return cmd
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

	file, err := os.Create(planOpts.output)
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

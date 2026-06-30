package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/spf13/cobra"
)

type planOptions struct {
	changeKind    []string
	includeDrafts bool
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
	cmd.Flags().BoolVar(&planOpts.includeDrafts, "include-drafts", false, "include draft Dependabot PRs in planning")

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
		classification.ChangeKind,
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

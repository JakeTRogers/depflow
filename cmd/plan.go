package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/spf13/cobra"
)

func newPlanCommand(deps commandDeps, opts *commandOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Show the deterministic Dependabot processing order",
		RunE: func(cmd *cobra.Command, _ []string) error {
			prs, err := discoverDependabotPRs(cmd.Context(), deps, opts)
			if err != nil {
				return err
			}

			plan := planner.Build(prs)
			if len(plan.Items) == 0 {
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

			return nil
		},
	}
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

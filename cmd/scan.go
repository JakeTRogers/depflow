package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/spf13/cobra"
)

func newScanCommand(deps commandDeps, opts *commandOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "scan",
		Short: "List open Dependabot pull requests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			prs, err := discoverDependabotPRs(cmd.Context(), deps, opts)
			if err != nil {
				return err
			}

			filterOpts := buildFilterOptions(opts, nil, true, false)
			prs, _ = dependabot.Filter(prs, filterOpts)
			prs = applyLimit(prs, opts)

			if len(prs) == 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), noOpenDependabotPRsMessage); err != nil {
					return fmt.Errorf("writing scan output: %w", err)
				}
				return nil
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Found %d open Dependabot pull request(s)\n\n", len(prs)); err != nil {
				return fmt.Errorf("writing scan header: %w", err)
			}

			for index, pr := range prs {
				if index > 0 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
						return fmt.Errorf("writing scan separator: %w", err)
					}
				}

				if err := writeScannedPR(cmd.OutOrStdout(), pr); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func writeScannedPR(writer io.Writer, pr dependabot.PR) error {
	classification := pr.Classification
	title := sanitize(pr.Title)
	author := sanitize(pr.Author)
	headRef := sanitize(pr.HeadRef)
	baseRef := sanitize(pr.BaseRef)
	dependencyName := sanitize(classification.DependencyName)
	url := sanitize(pr.URL)

	var builder strings.Builder
	fmt.Fprintf(&builder, "#%d %s\n", pr.Number, title)
	fmt.Fprintf(&builder, "  author: %s  draft: %s\n", author, yesNo(pr.Draft))
	fmt.Fprintf(&builder, "  refs: %s -> %s\n", headRef, baseRef)
	if dependencyName != "" {
		fmt.Fprintf(&builder, "  dependency: %s\n", dependencyName)
	}
	fmt.Fprintf(&builder, "  classification: ecosystem=%s change=%s grouped=%s dev-tooling=%s infra-sensitive=%s\n",
		displayOrUnknown(classification.Ecosystem),
		classification.ChangeKind,
		yesNo(classification.Grouped),
		yesNo(classification.DeveloperTooling),
		yesNo(classification.InfrastructureSensitive))
	fmt.Fprintf(&builder, "  labels: %s\n", formatLabels(pr.Labels))
	fmt.Fprintf(&builder, "  url: %s\n", url)

	if _, err := io.WriteString(writer, builder.String()); err != nil {
		return fmt.Errorf("writing scan output: %w", err)
	}

	return nil
}

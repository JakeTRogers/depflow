package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/executor"
	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/JakeTRogers/depflow/internal/progress"
	"github.com/spf13/cobra"
)

type executeOptions struct {
	dryRun           bool
	changeKind       []string
	includeDrafts    bool
	admin            bool
	pollInterval     time.Duration
	checkTimeout     time.Duration
	postMergeDelay   time.Duration
	postMergeTimeout time.Duration
	showChecks       bool
	showTiming       bool
}

const minPollInterval = 5 * time.Second

func newExecuteCommand(deps commandDeps, opts *commandOptions) *cobra.Command {
	execOpts := &executeOptions{}

	cmd := &cobra.Command{
		Use:   "execute",
		Short: "Process Dependabot PRs in planned order",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateExecuteOptions(execOpts); err != nil {
				return err
			}

			changeKinds, err := parseChangeKinds(execOpts.changeKind)
			if err != nil {
				return err
			}

			prs, err := discoverDependabotPRs(cmd.Context(), deps, opts)
			if err != nil {
				return err
			}

			filterOpts := buildFilterOptions(opts, changeKinds, execOpts.includeDrafts, true)
			included, excluded := dependabot.Filter(prs, filterOpts)
			included = applyLimit(included, opts)
			if len(excluded) > 0 {
				if err := writeExcludedPRs(cmd.OutOrStdout(), excludedPRsHeading, excluded); err != nil {
					return err
				}
			}

			plan := planner.Build(included)
			if len(plan.Items) == 0 {
				if len(excluded) > 0 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
						return fmt.Errorf("writing execute output spacing: %w", err)
					}
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), noEligiblePRsMessage); err != nil {
						return fmt.Errorf("writing execute output: %w", err)
					}
					return nil
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), noOpenDependabotPRsMessage); err != nil {
					return fmt.Errorf("writing execute output: %w", err)
				}
				return nil
			}

			if execOpts.dryRun {
				if len(excluded) > 0 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
						return fmt.Errorf("writing execute output spacing: %w", err)
					}
				}
				return printDryRun(cmd.OutOrStdout(), plan)
			}

			if len(excluded) > 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
					return fmt.Errorf("writing execute output spacing: %w", err)
				}
			}

			cfg := executor.Config{
				Admin:            execOpts.admin,
				PollInterval:     execOpts.pollInterval,
				CheckTimeout:     execOpts.checkTimeout,
				PostMergeDelay:   execOpts.postMergeDelay,
				PostMergeTimeout: execOpts.postMergeTimeout,
				ShowChecks:       execOpts.showChecks,
				ShowTiming:       execOpts.showTiming,
			}

			repo, err := resolveExecuteRepo(cmd.Context(), deps, opts.repo)
			if err != nil {
				return err
			}

			ui := progress.NewTracker(cmd.ErrOrStderr(), len(plan.Items))
			verbosity := progress.FromCount(opts.verbosity)
			log := progress.NewLogger(ui.LogWriter(), verbosity)

			result, err := executor.Run(cmd.Context(), deps.operator, plan, repo, cfg, log, ui)

			ui.Stop()
			if printErr := printResult(cmd.OutOrStdout(), result, execOpts.showTiming); printErr != nil {
				if err != nil {
					return errors.Join(err, printErr)
				}
				return printErr
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&execOpts.dryRun, "dry-run", false, "show planned order without executing")
	cmd.Flags().StringSliceVar(&execOpts.changeKind, "change-kind", defaultChangeKindValues, "include only these change kinds: patch, minor, major, unknown, or all")
	cmd.Flags().BoolVar(&execOpts.includeDrafts, "include-drafts", false, "include draft Dependabot PRs in execution")
	cmd.Flags().BoolVar(&execOpts.admin, "admin", false, "bypass branch protection rules using GitHub admin privileges")
	cmd.Flags().DurationVar(&execOpts.pollInterval, "poll-interval", 30*time.Second, "CI status polling interval")
	cmd.Flags().DurationVar(&execOpts.checkTimeout, "check-timeout", 30*time.Minute, "maximum wait for CI checks per PR")
	cmd.Flags().DurationVar(&execOpts.postMergeDelay, "post-merge-delay", 10*time.Second, "delay before checking post-merge CI")
	cmd.Flags().DurationVar(&execOpts.postMergeTimeout, "post-merge-timeout", 30*time.Minute, "maximum wait for post-merge CI")
	cmd.Flags().BoolVar(&execOpts.showChecks, "show-checks", false, "show per-check pass/pending/fail detail while waiting")
	cmd.Flags().BoolVar(&execOpts.showTiming, "show-timing", false, "show elapsed wait time and per-PR duration")

	return cmd
}

func validateExecuteOptions(opts *executeOptions) error {
	checks := []struct {
		flag  string
		value time.Duration
	}{
		{flag: "poll-interval", value: opts.pollInterval},
		{flag: "check-timeout", value: opts.checkTimeout},
		{flag: "post-merge-delay", value: opts.postMergeDelay},
		{flag: "post-merge-timeout", value: opts.postMergeTimeout},
	}

	for _, check := range checks {
		if check.value <= 0 {
			return fmt.Errorf("flag --%s must be greater than zero", check.flag)
		}
	}

	if opts.pollInterval < minPollInterval {
		return fmt.Errorf("flag --poll-interval must be at least %s", minPollInterval)
	}
	if opts.checkTimeout <= opts.pollInterval {
		return fmt.Errorf("flag --check-timeout must be greater than --poll-interval")
	}
	if opts.postMergeTimeout <= opts.pollInterval {
		return fmt.Errorf("flag --post-merge-timeout must be greater than --poll-interval")
	}

	return nil
}

func resolveExecuteRepo(ctx context.Context, deps commandDeps, repo string) (string, error) {
	if strings.TrimSpace(repo) != "" {
		return repo, nil
	}

	if deps.resolver == nil {
		return "", errors.New("resolving current repository: no repository resolver configured; " + rerunWithRepoHint(""))
	}

	resolvedRepo, err := deps.resolver.ResolveRepo(ctx)
	if err != nil {
		return "", fmt.Errorf("resolving current repository: %w; %s", err, rerunWithRepoHint(""))
	}

	resolvedRepo = strings.TrimSpace(resolvedRepo)
	if resolvedRepo == "" {
		return "", errors.New("resolving current repository: gh did not return a repository; " + rerunWithRepoHint(""))
	}

	return resolvedRepo, nil
}

func printDryRun(w io.Writer, plan planner.Plan) error {
	if _, err := fmt.Fprintf(w, "Dry run: %d PR(s) would be processed in this order:\n\n", len(plan.Items)); err != nil {
		return fmt.Errorf("writing dry run header: %w", err)
	}
	for i, item := range plan.Items {
		if _, err := fmt.Fprintf(w, "%d. #%d [%s] %s\n", i+1, item.PR.Number, item.Bucket, sanitize(item.PR.Title)); err != nil {
			return fmt.Errorf("writing dry run item: %w", err)
		}
		if _, err := fmt.Fprintf(w, "   reason: %s\n", sanitize(item.Reason)); err != nil {
			return fmt.Errorf("writing dry run reason: %w", err)
		}
	}
	return nil
}

func printResult(w io.Writer, result *executor.Result, showTiming bool) error {
	if result == nil {
		return nil
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return fmt.Errorf("writing execution summary spacing: %w", err)
	}
	if _, err := fmt.Fprintln(w, "Execution Summary"); err != nil {
		return fmt.Errorf("writing execution summary heading: %w", err)
	}
	if _, err := fmt.Fprintln(w, "================="); err != nil {
		return fmt.Errorf("writing execution summary divider: %w", err)
	}

	for _, pr := range result.Processed {
		status := string(pr.Status)
		line := fmt.Sprintf("#%d %s — %s", pr.Item.PR.Number, sanitize(pr.Item.PR.Title), status)
		if showTiming {
			line += fmt.Sprintf(" (%s)", pr.Duration.Round(time.Second))
		}
		if pr.Error != nil {
			line += fmt.Sprintf(" (%s)", sanitizeError(pr.Error))
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return fmt.Errorf("writing execution summary item: %w", err)
		}
	}

	merged := result.Merged()
	failed := result.Failed()
	if _, err := fmt.Fprintf(w, "\nMerged: %d", len(merged)); err != nil {
		return fmt.Errorf("writing execution summary totals: %w", err)
	}
	if failed != nil {
		if _, err := fmt.Fprintf(w, "  Failed: #%d", failed.Item.PR.Number); err != nil {
			return fmt.Errorf("writing execution summary failed item: %w", err)
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return fmt.Errorf("writing execution summary trailing newline: %w", err)
	}

	return nil
}

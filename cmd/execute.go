package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/JakeTRogers/depflow/internal/config"
	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/executor"
	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/JakeTRogers/depflow/internal/progress"
	"github.com/spf13/cobra"
)

type executeOptions struct {
	mergeMethod      string
	preferences      config.Document
	dryRun           bool
	changeKind       []string
	includeDrafts    bool
	admin            bool
	pollInterval     time.Duration
	checkTimeout     time.Duration
	postMergeDelay   time.Duration
	postMergeTimeout time.Duration
	requirePostMerge bool
	showChecks       bool
	showTiming       bool
	edit             bool
	planPath         string
}

const minPollInterval = 5 * time.Second

// checkRegistrationGrace is how long execute gives GitHub to register checks for a PR that
// reports none, or whose branch was just updated, before trusting the reported check state.
const checkRegistrationGrace = 30 * time.Second

// postMergeRunGrace is how long execute waits for a merge commit to start any workflow run before
// continuing with a warning; --require-post-merge-ci turns that into a failure instead.
const postMergeRunGrace = 2 * time.Minute

func newExecuteCommand(deps commandDeps, opts *commandOptions) *cobra.Command {
	execOpts := &executeOptions{}

	cmd := &cobra.Command{
		Use:   "execute",
		Short: "Process Dependabot PRs in planned order",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateExecuteOptions(execOpts); err != nil {
				return err
			}
			document, _, err := loadPreferences(opts)
			if err != nil {
				return err
			}
			execOpts.preferences = document
			if _, err := document.Resolve("", execOpts.mergeMethod, cmd.Flags().Changed("merge-method")); err != nil {
				return err
			}

			if execOpts.planPath != "" {
				if err := rejectFilterFlagsWithPlanFile(cmd); err != nil {
					return err
				}
				res, repo, err := planFromFile(cmd, deps, opts, execOpts.planPath)
				if err != nil {
					return err
				}
				return runResolvedPlanFile(cmd, deps, opts, execOpts, res, repo, true)
			}

			changeKinds, err := parseChangeKinds(execOpts.changeKind)
			if err != nil {
				return err
			}

			if execOpts.edit {
				res, repo, ok, err := planFromEditor(cmd, deps, opts, changeKinds, execOpts.includeDrafts)
				if err != nil || !ok {
					return err
				}
				return runResolvedPlanFile(cmd, deps, opts, execOpts, res, repo, false)
			}

			prs, err := discoverDependabotPRs(cmd.Context(), deps, opts)
			if err != nil {
				return err
			}
			if err := warnUnmatchedEcosystems(cmd.ErrOrStderr(), prs, opts); err != nil {
				return err
			}

			filterOpts := buildFilterOptions(opts, changeKinds, execOpts.includeDrafts, true)
			included, excluded := dependabot.Filter(prs, filterOpts)
			plan, overLimit := limitPlan(planner.Build(included), opts)
			if len(excluded) > 0 {
				if err := writeExcludedPRs(cmd.OutOrStdout(), excludedPRsHeading, excluded); err != nil {
					return err
				}
			}
			if err := writeLimitNotice(cmd.OutOrStdout(), len(overLimit), opts); err != nil {
				return err
			}

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

			if len(excluded) > 0 || len(overLimit) > 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
					return fmt.Errorf("writing execute output spacing: %w", err)
				}
			}

			return runPlan(cmd, deps, opts, execOpts, plan, "")
		},
	}

	cmd.Flags().BoolVar(&execOpts.dryRun, "dry-run", false, "show planned order without executing")
	cmd.Flags().StringVar(&execOpts.mergeMethod, "merge-method", "", "PR merge method: merge, squash, rebase (flag > environment > repo > global > merge)")
	if err := cmd.RegisterFlagCompletionFunc("merge-method", mergeMethodCompletions(deps, opts)); err != nil {
		panic(err)
	}
	cmd.Flags().BoolVar(&execOpts.edit, "edit", false, "edit the plan in $VISUAL/$EDITOR before executing (reorder lines, pick or skip PRs)")
	cmd.Flags().StringVar(&execOpts.planPath, "plan", "", "execute a plan `FILE` written by depflow plan -o (- reads stdin)")
	cmd.MarkFlagsMutuallyExclusive("edit", "plan")
	cmd.Flags().StringSliceVar(&execOpts.changeKind, "change-kind", defaultChangeKindValues, "include only these change kinds: patch, minor, major, unknown, or all")
	if err := cmd.RegisterFlagCompletionFunc("change-kind", changeKindCompletions); err != nil {
		panic(err)
	}
	cmd.Flags().BoolVar(&execOpts.includeDrafts, "include-drafts", false, "include draft Dependabot PRs in execution")
	cmd.Flags().BoolVar(&execOpts.admin, "admin", false, "bypass branch protection rules using GitHub admin privileges")
	cmd.Flags().DurationVar(&execOpts.pollInterval, "poll-interval", 30*time.Second, "CI status polling interval")
	cmd.Flags().DurationVar(&execOpts.checkTimeout, "check-timeout", 30*time.Minute, "maximum wait for CI checks per PR")
	cmd.Flags().DurationVar(&execOpts.postMergeDelay, "post-merge-delay", 10*time.Second, "delay before checking post-merge CI")
	cmd.Flags().DurationVar(&execOpts.postMergeTimeout, "post-merge-timeout", 30*time.Minute, "maximum wait for post-merge CI")
	cmd.Flags().BoolVar(&execOpts.requirePostMerge, "require-post-merge-ci", false, "fail if a merge commit starts no workflow runs instead of continuing with a warning")
	cmd.Flags().BoolVar(&execOpts.showChecks, "show-checks", false, "show per-check pass/pending/fail detail while waiting")
	cmd.Flags().BoolVar(&execOpts.showTiming, "show-timing", false, "show elapsed wait time and per-PR duration")

	return cmd
}

// runResolvedPlanFile reports what changed since a plan file was written, then dry-runs or
// executes it in file order.
func runResolvedPlanFile(cmd *cobra.Command, deps commandDeps, opts *commandOptions, execOpts *executeOptions, res planFileResolution, repo string, reportUnlisted bool) error {
	out := cmd.OutOrStdout()
	if err := writePlanFileNotices(out, res, reportUnlisted); err != nil {
		return err
	}

	if res.picked == 0 {
		return printLine(out, noPickLinesMessage)
	}
	if len(res.plan.Items) == 0 {
		return printLine(out, noOpenPickedPRsMessage)
	}

	// Bucket reasons describe the planner's ordering, which the file overrides, so list only
	// the file order.
	if execOpts.dryRun {
		if err := printPlanOrder(out, fmt.Sprintf(dryRunHeaderFormat, len(res.plan.Items)), res.plan); err != nil {
			return err
		}
		_, _, err := prepareMerge(cmd, deps, opts, execOpts, res.plan, repo)
		return err
	}
	if err := printPlanOrder(out, fmt.Sprintf("Processing %d PR(s) in this order:\n", len(res.plan.Items)), res.plan); err != nil {
		return err
	}

	return runPlan(cmd, deps, opts, execOpts, res.plan, repo)
}

// runPlan dry-runs or executes plan. An empty repo is resolved only when executing.
func runPlan(cmd *cobra.Command, deps commandDeps, opts *commandOptions, execOpts *executeOptions, plan planner.Plan, repo string) error {
	if execOpts.dryRun {
		if err := printDryRun(cmd.OutOrStdout(), plan); err != nil {
			return err
		}
	}
	method, repo, err := prepareMerge(cmd, deps, opts, execOpts, plan, repo)
	if err != nil || execOpts.dryRun {
		return err
	}

	cfg := executor.Config{
		MergeMethod:        method.Value,
		Admin:              execOpts.admin,
		PollInterval:       execOpts.pollInterval,
		CheckTimeout:       execOpts.checkTimeout,
		CheckGrace:         checkRegistrationGrace,
		PostMergeDelay:     execOpts.postMergeDelay,
		PostMergeTimeout:   execOpts.postMergeTimeout,
		PostMergeGrace:     postMergeRunGrace,
		RequirePostMergeCI: execOpts.requirePostMerge,
		ShowChecks:         execOpts.showChecks,
		ShowTiming:         execOpts.showTiming,
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

func resolveRepo(ctx context.Context, deps commandDeps, repo string) (string, error) {
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

const dryRunHeaderFormat = "Dry run: %d PR(s) would be processed in this order:\n\n"

func printDryRun(w io.Writer, plan planner.Plan) error {
	if _, err := fmt.Fprintf(w, dryRunHeaderFormat, len(plan.Items)); err != nil {
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

func printPlanOrder(w io.Writer, header string, plan planner.Plan) error {
	if _, err := io.WriteString(w, header); err != nil {
		return fmt.Errorf("writing plan order header: %w", err)
	}
	for i, item := range plan.Items {
		if _, err := fmt.Fprintf(w, "%d. #%d [%s] %s\n", i+1, item.PR.Number, item.Bucket, sanitize(item.PR.Title)); err != nil {
			return fmt.Errorf("writing plan order item: %w", err)
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

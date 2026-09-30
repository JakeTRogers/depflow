// Merge preference resolution is shared by normal, edited, and saved plans.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JakeTRogers/depflow/internal/config"
	"github.com/JakeTRogers/depflow/internal/githubcli"
	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/spf13/cobra"
)

func mergeMethodCompletions(deps commandDeps, opts *commandOptions) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		methods := []string{"merge", "squash", "rebase"}
		if deps.operator != nil {
			ctx, cancel := context.WithTimeout(cmd.Context(), 500*time.Millisecond)
			defer cancel()
			if caps, err := deps.operator.ReadMergeCapabilities(ctx, opts.repo); err == nil {
				methods = caps.Methods
			}
		}
		return mergeMethodCandidates(methods), cobra.ShellCompDirectiveNoFileComp
	}
}

func prepareMerge(cmd *cobra.Command, deps commandDeps, opts *commandOptions, execOpts *executeOptions, plan planner.Plan, repo string) (config.Value, string, error) {
	var capabilityErr error
	if repo == "" {
		repo, capabilityErr = resolveRepo(cmd.Context(), deps, opts.repo)
	}
	var caps githubcli.MergeCapabilities
	if capabilityErr == nil {
		if deps.operator == nil {
			capabilityErr = errors.New("no repository capability reader configured")
		} else {
			caps, capabilityErr = deps.operator.ReadMergeCapabilities(cmd.Context(), repo)
			if capabilityErr == nil {
				repo = caps.Repo
			}
		}
	}
	canonical := ""
	if repo != "" {
		var err error
		canonical, err = config.CanonicalRepo(repo)
		if err != nil {
			return config.Value{}, repo, err
		}
	}
	method, err := execOpts.preferences.Resolve(canonical, execOpts.mergeMethod, cmd.Flags().Changed("merge-method"))
	if err != nil {
		return config.Value{}, repo, err
	}
	if err := printLine(cmd.ErrOrStderr(), fmt.Sprintf("Merge method: %s (%s)", method.Value, sanitize(method.Source))); err != nil {
		return method, repo, err
	}
	if capabilityErr != nil {
		if !execOpts.dryRun || errors.Is(capabilityErr, context.Canceled) {
			return method, repo, capabilityErr
		}
		return method, repo, printLine(cmd.ErrOrStderr(), "Warning: merge policy unverified; repository preferences may be unresolved: "+sanitize(capabilityErr.Error()))
	}
	if err := caps.Require(method.Value); err != nil {
		return method, repo, mergePreflightError(method, err)
	}
	if err := printLine(cmd.ErrOrStderr(), "Repository-enabled methods: "+strings.Join(caps.Methods, ", ")+" (branch rules still apply)"); err != nil {
		return method, repo, err
	}
	for _, item := range plan.Items {
		if err := deps.operator.CheckMergeAllowed(cmd.Context(), repo, item.PR.Number, method.Value); err != nil {
			var conflict *githubcli.MethodNotAllowedError
			if execOpts.dryRun && !errors.As(err, &conflict) && !errors.Is(err, githubcli.ErrMergeQueueUnsupported) && !errors.Is(err, context.Canceled) {
				if err := printLine(cmd.ErrOrStderr(), "Warning: merge policy unverified: "+sanitize(err.Error())); err != nil {
					return method, repo, err
				}
				continue
			}
			return method, repo, mergePreflightError(method, err)
		}
	}
	return method, repo, nil
}

func mergePreflightError(method config.Value, err error) error {
	var conflict *githubcli.MethodNotAllowedError
	if errors.As(err, &conflict) && len(conflict.Capabilities.Methods) > 0 {
		return fmt.Errorf("%s: %w; no pull requests were modified; rerun the same command with --merge-method %s, preserving its filters and plan arguments", method.Source, err, conflict.Capabilities.Methods[0])
	}
	return fmt.Errorf("%s: %w; no pull requests were modified", method.Source, err)
}

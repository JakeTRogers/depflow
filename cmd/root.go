// Package cmd wires the depflow Cobra command tree and shared CLI helpers.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/JakeTRogers/depflow/internal/executor"
	"github.com/JakeTRogers/depflow/internal/githubcli"
	"github.com/spf13/cobra"
)

type prLister interface {
	ListOpenPullRequests(ctx context.Context, repo string, limit int) ([]githubcli.PullRequest, error)
}

type prOperator interface {
	executor.Operator
}

type repoResolver interface {
	ResolveRepo(ctx context.Context) (string, error)
}

type commandDeps struct {
	lister   prLister
	operator prOperator
	resolver repoResolver
}

type commandOptions struct {
	repo      string
	limit     int
	verbosity int
}

const defaultPullRequestLimit = 100

func defaultDeps() (commandDeps, error) {
	client, err := githubcli.NewClient()
	if err != nil {
		return commandDeps{}, fmt.Errorf("creating GitHub CLI client: %w", err)
	}

	return commandDeps{
		lister:   client,
		operator: client,
		resolver: client,
	}, nil
}

func newRootCommand(deps commandDeps) *cobra.Command {
	opts := &commandOptions{limit: defaultPullRequestLimit}

	cmd := &cobra.Command{
		Use:           "depflow",
		Short:         "Discover, plan, and execute Dependabot PR processing",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.PersistentFlags().StringVar(&opts.repo, "repo", "", "GitHub repository in [HOST/]OWNER/REPO format")
	cmd.PersistentFlags().IntVar(&opts.limit, "limit", defaultPullRequestLimit, "maximum number of Dependabot pull requests to return after filtering")
	cmd.PersistentFlags().CountVarP(&opts.verbosity, "verbose", "v", "increase log verbosity (-v info, -vv debug, -vvv trace)")

	cmd.AddCommand(newScanCommand(deps, opts))
	cmd.AddCommand(newPlanCommand(deps, opts))
	cmd.AddCommand(newExecuteCommand(deps, opts))
	cmd.AddCommand(newVersionCommand())

	return cmd
}

// Execute runs the root command and exits non-zero on failure.
func Execute() {
	if err := execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, sanitize(err.Error()))
		os.Exit(1)
	}
}

func execute() error {
	deps, err := defaultDeps()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := newRootCommand(deps)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		return err
	}

	return nil
}

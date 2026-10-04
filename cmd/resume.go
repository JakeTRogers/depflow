package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/JakeTRogers/depflow/internal/executor"
	"github.com/JakeTRogers/depflow/internal/planfile"
	"github.com/JakeTRogers/depflow/internal/planner"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resumeFlags are the execute settings a resumed run must repeat to behave the same way. Filter
// flags are left out because a plan file already fixes which PRs run.
var resumeFlags = []string{
	"repo",
	"config",
	"verbose",
	"merge-method",
	"admin",
	"poll-interval",
	"check-timeout",
	"post-merge-delay",
	"post-merge-timeout",
	"require-post-merge-ci",
	"show-checks",
	"show-timing",
}

// resumeItems returns the PRs a rerun should process, in plan order: the PR that stopped
// execution, unless it had already merged, followed by every PR that was never attempted.
func resumeItems(plan planner.Plan, result *executor.Result) (resume []planner.PlannedPR, notAttempted []int) {
	processed := make(map[int]executor.PRResult)
	if result != nil {
		for _, pr := range result.Processed {
			processed[pr.Item.PR.Number] = pr
		}
	}

	for _, item := range plan.Items {
		pr, ok := processed[item.PR.Number]
		switch {
		case !ok:
			resume = append(resume, item)
			notAttempted = append(notAttempted, item.PR.Number)
		case pr.Error != nil && !pr.Merged:
			resume = append(resume, item)
		}
	}
	return resume, notAttempted
}

// writeResumePlan saves the PRs left after a stopped run as a plan file and prints how to
// continue from it.
func writeResumePlan(cmd *cobra.Command, deps commandDeps, resume []planner.PlannedPR, result *executor.Result, repo string) error {
	file, err := os.CreateTemp(deps.resumeDir, "depflow-resume-*.txt")
	if err != nil {
		return fmt.Errorf("creating resume plan: %w", err)
	}
	writeErr := planfile.Write(file, repo, time.Now(), resume, nil)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("saving resume plan %s: %w", file.Name(), err)
	}

	note := ""
	if failed := result.Failed(); failed != nil && !failed.Merged {
		note = fmt.Sprintf(" (#%d failed and is listed first; change it to skip to leave it out)", failed.Item.PR.Number)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "\nResume plan for %d PR(s) written to %s%s.\n%s\n",
		len(resume), file.Name(), note, resumeHint(cmd, file.Name(), runtime.GOOS))
	if err != nil {
		return fmt.Errorf("writing resume hint: %w", err)
	}
	return nil
}

func resumeHint(cmd *cobra.Command, path, goos string) string {
	shell := ""
	if goos == "windows" {
		shell = " in PowerShell"
	}
	return fmt.Sprintf("To continue, run%s: depflow%s execute --plan %s", shell, resumeFlagArgs(cmd, goos), shellQuote(path, goos))
}

// resumeFlagArgs renders the explicitly set resumeFlags of cmd as command-line arguments.
func resumeFlagArgs(cmd *cobra.Command, goos string) string {
	var builder strings.Builder
	for _, name := range resumeFlags {
		flag := cmd.Flags().Lookup(name)
		if flag == nil || !flag.Changed {
			continue
		}
		builder.WriteString(" " + flagArg(flag, goos))
	}
	return builder.String()
}

func flagArg(flag *pflag.Flag, goos string) string {
	if flag.Value.Type() == "bool" && flag.Value.String() == "true" {
		return "--" + flag.Name
	}
	return "--" + flag.Name + "=" + shellQuote(flag.Value.String(), goos)
}

// shellQuote quotes value for PowerShell on Windows and POSIX shells elsewhere.
func shellQuote(value, goos string) string {
	if goos == "windows" {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	if value != "" && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@%+,") == "" {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func writeNotAttempted(w io.Writer, notAttempted []int) error {
	if len(notAttempted) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "Not attempted: %s\n", formatPRNumbers(notAttempted)); err != nil {
		return fmt.Errorf("writing execution summary: %w", err)
	}
	return nil
}

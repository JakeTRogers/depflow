package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/JakeTRogers/depflow/internal/dependabot"
	"github.com/JakeTRogers/depflow/internal/terminal"
)

const (
	noOpenDependabotPRsMessage   = "No open Dependabot pull requests found."
	noEligiblePRsMessage         = "Nothing to do after applying filters."
	excludedPRsHeading           = "Excluded by filters"
	rerunWithRepoFlagHint        = "rerun with --repo OWNER/REPO"
	maxDiscoveryPullRequestLimit = 1000
)

func writeExcludedPRs(writer io.Writer, heading string, excluded []dependabot.ExcludedPR) error {
	if _, err := fmt.Fprintf(writer, "%s (%d):\n", heading, len(excluded)); err != nil {
		return fmt.Errorf("writing excluded PRs heading: %w", err)
	}

	for _, ex := range excluded {
		if _, err := fmt.Fprintf(writer, "#%d %s — %s\n", ex.PR.Number, sanitize(ex.PR.Title), sanitize(ex.Reason)); err != nil {
			return fmt.Errorf("writing excluded PRs item: %w", err)
		}
	}

	return nil
}

func rerunWithRepoHint(suffix string) string {
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return rerunWithRepoFlagHint
	}

	return rerunWithRepoFlagHint + " " + suffix
}

func sanitize(value string) string {
	return terminal.Sanitize(value)
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}

	return sanitize(err.Error())
}

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
	noEligiblePRsMessage         = "Nothing to do after excluding major updates."
	rerunWithRepoFlagHint        = "rerun with --repo OWNER/REPO"
	maxDiscoveryPullRequestLimit = 1000
)

func writeExcludedMajorUpdates(writer io.Writer, heading string, excluded []dependabot.PR) error {
	if _, err := fmt.Fprintf(writer, "%s (%d):\n", heading, len(excluded)); err != nil {
		return fmt.Errorf("writing excluded major updates heading: %w", err)
	}

	for _, pr := range excluded {
		if _, err := fmt.Fprintf(writer, "#%d %s\n", pr.Number, sanitize(pr.Title)); err != nil {
			return fmt.Errorf("writing excluded major updates item: %w", err)
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

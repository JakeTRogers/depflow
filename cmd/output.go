package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/JakeTRogers/depflow/internal/dependabot"
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
	if value == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(value))

	changed := false
	for i := 0; i < len(value); i++ {
		if shouldStripControlByte(value[i]) {
			changed = true
			continue
		}
		builder.WriteByte(value[i])
	}

	if !changed {
		return value
	}

	return builder.String()
}

func shouldStripControlByte(value byte) bool {
	switch {
	case value == '\t', value == '\n', value == '\r':
		return false
	case value <= 0x08:
		return true
	case value >= 0x0B && value <= 0x0C:
		return true
	case value >= 0x0E && value <= 0x1F:
		return true
	case value == 0x1B:
		return true
	case value == 0x7F:
		return true
	default:
		return false
	}
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}

	return sanitize(err.Error())
}

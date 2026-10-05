package githubcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// DependabotAlert is an open Dependabot security alert.
type DependabotAlert struct {
	Ecosystem string `json:"ecosystem"`
	Package   string `json:"package"`
	Severity  string `json:"severity"`
}

const alertsJQ = ".[] | {ecosystem: .dependency.package.ecosystem, package: .dependency.package.name, severity: .security_advisory.severity}"

// ListOpenDependabotAlerts returns the repository's open Dependabot alerts. An empty repo uses
// the repository gh infers. Reading alerts needs access to them, so callers should expect
// permission errors for repositories the user does not administer.
func (c *client) ListOpenDependabotAlerts(ctx context.Context, repo string) ([]DependabotAlert, error) {
	args, err := dependabotAlertsArgs(repo)
	if err != nil {
		return nil, err
	}

	output, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("listing Dependabot alerts: running gh %s: %w", commandSummary(args), err)
	}

	// --paginate with --jq prints one JSON object per line across all pages.
	var alerts []DependabotAlert
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var alert DependabotAlert
		if err := decoder.Decode(&alert); errors.Is(err, io.EOF) {
			return alerts, nil
		} else if err != nil {
			return nil, fmt.Errorf("decoding Dependabot alerts: %w", err)
		}
		alerts = append(alerts, alert)
	}
}

func dependabotAlertsArgs(repo string) ([]string, error) {
	const query = "dependabot/alerts?state=open&per_page=100"

	switch strings.Count(repo, "/") {
	case 0:
		if repo != "" {
			return nil, fmt.Errorf("repo must be in OWNER/REPO or HOST/OWNER/REPO format")
		}
		return []string{"api", "repos/{owner}/{repo}/" + query, "--paginate", "--jq", alertsJQ}, nil
	case 1:
		return []string{"api", "repos/" + repo + "/" + query, "--paginate", "--jq", alertsJQ}, nil
	case 2:
		parts := strings.SplitN(repo, "/", 3)
		return []string{"api", "repos/" + parts[1] + "/" + parts[2] + "/" + query, "--hostname", parts[0], "--paginate", "--jq", alertsJQ}, nil
	default:
		return nil, fmt.Errorf("repo must be in OWNER/REPO or HOST/OWNER/REPO format")
	}
}

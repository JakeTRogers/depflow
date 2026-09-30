// Repository capability checks run before mutating GitHub state.
package githubcli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/JakeTRogers/depflow/internal/config"
)

// MergeCapabilities lists repository-wide enabled methods, not branch permissions.
type MergeCapabilities struct {
	Repo    string
	Methods []string
}

// ErrMergeQueueUnsupported marks a known queue policy rather than a lookup failure.
var ErrMergeQueueUnsupported = errors.New("queue-managed merging is not supported by depflow (including --admin)")

// MethodNotAllowedError reports a repository preference conflict.
type MethodNotAllowedError struct {
	Method       string
	Capabilities MergeCapabilities
}

func (err *MethodNotAllowedError) Error() string {
	allowed := strings.Join(err.Capabilities.Methods, ", ")
	if allowed == "" {
		allowed = "none"
	}
	return fmt.Sprintf("merge method %q is disabled for %s; repository-enabled methods: %s", err.Method, err.Capabilities.Repo, allowed)
}

// Require rejects methods that the repository explicitly disables.
func (caps MergeCapabilities) Require(method string) error {
	if err := config.ValidateMethod(method); err != nil {
		return err
	}
	if !slices.Contains(caps.Methods, method) {
		return &MethodNotAllowedError{Method: method, Capabilities: caps}
	}
	return nil
}

type capabilityResponse struct {
	URL                string `json:"url"`
	MergeCommitAllowed *bool  `json:"mergeCommitAllowed"`
	SquashMergeAllowed *bool  `json:"squashMergeAllowed"`
	RebaseMergeAllowed *bool  `json:"rebaseMergeAllowed"`
}

func (response capabilityResponse) capabilities() (MergeCapabilities, error) {
	repo, err := repositoryFromURL(response.URL)
	if err != nil {
		return MergeCapabilities{}, err
	}
	if response.MergeCommitAllowed == nil || response.SquashMergeAllowed == nil || response.RebaseMergeAllowed == nil {
		return MergeCapabilities{}, errors.New("GitHub returned incomplete merge capabilities")
	}
	caps := MergeCapabilities{Repo: repo}
	for _, entry := range []struct {
		name    string
		allowed bool
	}{{"merge", *response.MergeCommitAllowed}, {"squash", *response.SquashMergeAllowed}, {"rebase", *response.RebaseMergeAllowed}} {
		if entry.allowed {
			caps.Methods = append(caps.Methods, entry.name)
		}
	}
	return caps, nil
}

func repositoryFromURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("GitHub returned an invalid repository URL %q", raw)
	}
	return config.CanonicalRepo(parsed.Host + parsed.Path)
}

// ReadMergeCapabilities resolves the host and repository in the same gh request.
func (c *client) ReadMergeCapabilities(ctx context.Context, repo string) (MergeCapabilities, error) {
	args := []string{"repo", "view"}
	if repo != "" {
		args = append(args, repo)
	}
	args = append(args, "--json", "url,mergeCommitAllowed,squashMergeAllowed,rebaseMergeAllowed")
	var response capabilityResponse
	if err := c.runJSON(ctx, &response, args...); err != nil {
		return MergeCapabilities{}, fmt.Errorf("reading repository merge capabilities: %w", err)
	}
	return response.capabilities()
}

// CheckMergeAllowed refreshes repository permissions and rejects queue-managed PRs.
func (c *client) CheckMergeAllowed(ctx context.Context, repo string, number int, method string) error {
	canonical, err := config.CanonicalRepo(repo)
	if err != nil {
		return err
	}
	parts := strings.Split(canonical, "/")
	const query = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){url mergeCommitAllowed squashMergeAllowed rebaseMergeAllowed pullRequest(number:$number){isMergeQueueEnabled}}}`
	var response struct {
		Data struct {
			Repository struct {
				capabilityResponse
				PullRequest struct {
					IsMergeQueueEnabled *bool `json:"isMergeQueueEnabled"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.runJSON(ctx, &response, "api", "graphql", "--hostname", parts[0], "-f", "query="+query,
		"-f", "owner="+parts[1], "-f", "name="+parts[2], "-F", "number="+strconv.Itoa(number)); err != nil {
		return fmt.Errorf("checking merge policy for PR #%d: %w", number, err)
	}
	if len(response.Errors) > 0 {
		return fmt.Errorf("checking merge policy: %s", response.Errors[0].Message)
	}
	caps, err := response.Data.Repository.capabilities()
	if err != nil {
		return err
	}
	queue := response.Data.Repository.PullRequest.IsMergeQueueEnabled
	if queue == nil {
		return fmt.Errorf("GitHub did not return merge queue status for PR #%d", number)
	}
	if *queue {
		return fmt.Errorf("PR #%d targets a merge-queue-enabled branch: %w", number, ErrMergeQueueUnsupported)
	}
	return caps.Require(method)
}

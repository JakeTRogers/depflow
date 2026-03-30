package dependabot

import (
	"strings"

	"github.com/JakeTRogers/depflow/internal/githubcli"
)

// PR is the normalized Dependabot pull request shape used by planning.
type PR struct {
	Number         int
	Title          string
	URL            string
	Author         string
	BaseRef        string
	HeadRef        string
	Labels         []string
	Draft          bool
	Classification Classification
}

// Normalize converts a raw GitHub pull request into the normalized Dependabot shape.
func Normalize(raw githubcli.PullRequest) (PR, bool) {
	author := strings.TrimSpace(raw.Author.Login)
	if !isDependabotAuthor(author) {
		return PR{}, false
	}

	labels := make([]string, 0, len(raw.Labels))
	for _, label := range raw.Labels {
		name := strings.TrimSpace(label.Name)
		if name == "" {
			continue
		}
		labels = append(labels, name)
	}

	return PR{
		Number:  raw.Number,
		Title:   raw.Title,
		URL:     raw.URL,
		Author:  author,
		BaseRef: raw.BaseRefName,
		HeadRef: raw.HeadRefName,
		Labels:  labels,
		Draft:   raw.IsDraft,
		Classification: classify(
			raw.Title,
			raw.HeadRefName,
			labels,
		),
	}, true
}

// Package executor runs the sequential Dependabot pull request execution flow.
package executor

import "errors"

var (
	// ErrExecutionFailed indicates executor.Run stopped after a PR processing failure.
	ErrExecutionFailed = errors.New("execution failed")
	// ErrCheckFailed indicates a PR CI check reached a terminal failure state.
	ErrCheckFailed = errors.New("CI check failed")
	// ErrCheckTimeout indicates waiting for PR CI checks exceeded the configured timeout.
	ErrCheckTimeout = errors.New("CI check timeout exceeded")
	// ErrBranchUpdateTimeout indicates waiting for a rebased branch update exceeded the configured timeout.
	ErrBranchUpdateTimeout = errors.New("branch update timeout exceeded")
	// ErrHeadChanged indicates the PR head commit moved after its CI checks were verified.
	ErrHeadChanged = errors.New("head commit changed after CI checks passed")
	// ErrBranchBehind indicates the PR branch was still behind its base when it was about to merge.
	ErrBranchBehind = errors.New("branch behind base")
	// ErrMergeConflict indicates the PR cannot be merged because of conflicts.
	ErrMergeConflict = errors.New("merge conflict detected")
	// ErrPostMergeTimeout indicates waiting for post-merge CI exceeded the configured timeout.
	ErrPostMergeTimeout = errors.New("post-merge CI timeout exceeded")
)

# Copilot Instructions for depflow

## Project Overview

depflow is a Go 1.25 CLI tool for discovering open Dependabot pull requests, planning a deterministic processing order, and executing that plan with live progress tracking. It uses the GitHub CLI (`gh`) as its sole GitHub transport.

## Architecture

### Package Layout

- `cmd/` — Cobra command wiring
	- `root.go` — Root command, global flags (`--repo`, `--limit`, `-v/--verbose`), dependency injection via `commandDeps` (holds `prLister`, `prOperator`, and `repoResolver` interfaces), and signal-aware root execution via `signal.NotifyContext` and `ExecuteContext`
	- `scan.go` — `scan` subcommand, lists open Dependabot PRs with metadata
	- `plan.go` — `plan` subcommand, shows deterministic processing order
	- `execute.go` — `execute` subcommand, validates positive duration flags plus a minimum `--poll-interval` of 5s and timeout `>` poll-interval relationships, resolves the repo when `--repo` is omitted, and wires the executor with progress tracker and verbosity-controlled logger. Execute-specific flags: `--dry-run`, `--poll-interval`, `--check-timeout`, `--post-merge-delay`, `--post-merge-timeout`
	- `version.go` — `version` subcommand, prints version and platform
	- `discovery.go` — Shared PR discovery logic used by scan, plan, and execute

- `internal/dependabot/` — Dependabot PR normalization and classification
	- `pr.go` — `PR` type, `Normalize()` filters by Dependabot author using the package-private `isDependabotAuthor()` helper
	- `classify.go` — `Classification` type, package-private `classify()` infers ecosystem, dependency name, version range, change kind (patch/minor/major), grouping, CI, dev-tooling, and infra-sensitive signals. Handles conventional-commit-prefixed titles.

- `internal/planner/` — Deterministic bucket-based ordering
	- `plan.go` — `Plan` and `PlannedPR` types, `Build()` sorts PRs into buckets (ci → developer-tooling → patch → minor → grouped → unknown → infra-sensitive → major) with tie-breaking by change kind rank, ecosystem, dependency name, title, PR number

- `internal/executor/` — Sequential PR processing loop
	- `executor.go` — `Operator` interface (ViewPullRequest, MergePullRequest, CommentOnPR, ListWorkflowRuns, CompareBranches), `Progress` interface (SetStatus, Increment), `Config`, `PRResult`, `Result` types, `Run()` orchestration function
	- `wait.go` — `waitForChecks()`, `waitForPostMergeCI()` (correlates runs by merge commit SHA and exits early on terminal failures), `waitForBranchUpdate()` polling helpers
	- `errors.go` — Sentinel errors: `ErrExecutionFailed`, `ErrCheckFailed`, `ErrCheckTimeout`, `ErrBranchUpdateTimeout`, `ErrMergeConflict`, `ErrPostMergeTimeout`

- `internal/githubcli/` — Thin `gh` CLI wrapper
	- `client.go` — package-private `client` type returned by `NewClient()`, `ghExecutor` wraps `exec.CommandContext` with `GH_PAGER=""` env var, resolves the `gh` binary to an absolute path once, provides `runJSON()` and `ResolveRepo()`, and truncates stderr in command errors
	- `pulls.go` — `ListOpenPullRequests()`, `PullRequest` type
	- `pr_detail.go` — `ViewPullRequest()`, `PRDetail`, `MergeCommit`, and `StatusCheck` types; `PRDetail.MergeCommit.OID` carries the merge commit SHA used after merge
	- `merge.go` — `MergePullRequest()` (merge commit + delete branch)
	- `comment.go` — `CommentOnPR()` (used for `@dependabot rebase`)
	- `compare.go` — `CompareBranches()` via GitHub API with URL-escaped refs, `BranchComparison` type
	- `runs.go` — `ListWorkflowRuns()`, `WorkflowRun` type with `HeadSHA` for merge-SHA correlation

- `internal/progress/` — Terminal progress display
	- `progress.go` — package-private `tracker` type returned by `NewTracker()`, exported `Verbosity` type, `NewLogger()` creates verbosity-filtered slog logger writing above the progress bar

- `main.go` — Entry point, calls `cmd.Execute()`

### Key Types

- `dependabot.PR` — Normalized Dependabot PR with `Classification`
- `dependabot.Classification` — Ecosystem, change kind, grouping, dev-tooling, infra-sensitive signals
- `planner.Plan` / `planner.PlannedPR` — Ordered processing plan with bucket and reason
- `executor.Operator` — Interface for all GitHub operations (satisfied by the package-private GitHub CLI client returned by `githubcli.NewClient()`)
- `executor.Progress` — Interface for progress updates (satisfied by the package-private tracker returned by `progress.NewTracker()`)
- `executor.Config` — Polling intervals and timeouts
- `executor.Result` / `executor.PRResult` — Execution outcomes
- `progress.Verbosity` — Quiet (0) / Info (1) / Debug (2) / Trace (3)
- `githubcli.PRDetail` — Detailed PR state including `MergeCommit.OID` for merged PR confirmation
- `githubcli.WorkflowRun` — Workflow run metadata including `HeadSHA` for post-merge CI correlation
- `executor` sentinel errors — `ErrExecutionFailed`, `ErrCheckFailed`, `ErrCheckTimeout`, `ErrBranchUpdateTimeout`, `ErrMergeConflict`, `ErrPostMergeTimeout`

### Command Surface

- `depflow scan` — read-only, lists open Dependabot PRs with classification
- `depflow plan` — read-only, shows deterministic processing order
- `depflow execute` — mutating, processes PRs sequentially with signal-aware shutdown (rebase → CI wait → merge → post-merge CI wait correlated by merge commit SHA)
- `depflow version` — prints version and platform

## Code Patterns

**Error handling:** Wrap errors with context using `fmt.Errorf("description: %w", err)`

**Logging:** Use `log/slog` (stdlib). The progress package creates verbosity-filtered loggers via `progress.NewLogger()`. Logs write above the mpb progress bar through the tracker's `LogWriter()`.

**Adding CLI flags:** Define in `init()` or command constructor, use `cmd.Flags()` for command-specific or `cmd.PersistentFlags()` for global. Count flags use `CountVarP()` (e.g., verbosity).

**Signal handling:** `cmd.Execute()` creates a root context with `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` and runs Cobra with `ExecuteContext(...)` so active waits can stop promptly on shutdown.

**Interfaces:** Defined at the consumer, not the provider. `executor.Operator` is defined in the executor package and satisfied by the package-private GitHub client returned by `githubcli.NewClient()`. `executor.Progress` is defined in executor and satisfied by the package-private tracker returned by `progress.NewTracker()`.

**GitHub transport:** All GitHub operations go through the `gh` CLI via `exec.CommandContext`. `githubcli.NewClient()` resolves `gh` once up front, JSON responses are decoded with `client.runJSON()`, and non-JSON commands use the client's executor directly.

**Execute validation:** Keep execute flag validation in `cmd/execute.go`. All duration flags must be `> 0`, `--poll-interval` must be at least 5 seconds, and `--check-timeout` / `--post-merge-timeout` must be greater than `--poll-interval`.

**Rebase strategy:** For PRs behind base, depflow posts a `@dependabot rebase` comment and polls `CompareBranches` until the branch is up to date (replaced the unreliable GitHub `update-branch` API).

**Execute flow:** Sequential processing with stop-on-first-failure semantics. After a successful merge, depflow re-reads the PR to obtain `MergeCommit.OID`, waits for post-merge CI by matching workflow `HeadSHA`, and converts non-context post-merge failures into recorded failed `PRResult` entries returned through `ErrExecutionFailed`. No retry or skip mode.

## Development Commands

```bash
go test ./... -v           # Run tests
go test ./... -cover       # Coverage
go build -o depflow .      # Build binary
go vet ./...               # Static analysis
```

## Testing Patterns

Use `httptest.Server`-style `stubExecutor` for GitHub CLI tests — records `gh` args and returns canned JSON. Test the `cmd` package using Cobra's `Execute()` with captured stdout. Use table-driven tests with `t.Parallel()`. The executor uses a `fakeOperator` pattern for testing with configurable return values per call. `sleepFunc` is a package-level variable replaced in tests to avoid real delays.

## Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/vbauerster/mpb/v8` — Terminal progress bar (mpb)
- Go 1.25 stdlib (`log/slog`, `context`, `encoding/json`, `os/exec`, etc.)

## Commit Convention

Uses [Conventional Commits](https://www.conventionalcommits.org/) (enforced by pre-commit hooks).

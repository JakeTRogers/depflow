# depflow

depflow is a Go CLI tool for discovering open Dependabot pull requests, planning a deterministic processing order, and executing that plan with live progress tracking.

## Commands

By default, `plan` and `execute` exclude major version updates and draft PRs; use `--change-kind` and `--include-drafts` to change that. `scan` is a pure visibility tool and always shows every open Dependabot PR regardless of change-kind or draft state, though it still honors the shared classification filters described below.

### Filtering

`scan`, `plan`, and `execute` share a set of classification-based filters built from the same signals shown by `scan` (ecosystem, dependency name, labels, grouping):

- `--ecosystem` / `--exclude-ecosystem` — allow-list / deny-list by ecosystem (repeatable or comma-separated)
- `--dependency` / `--exclude-dependency` — allow-list / deny-list by substring match against the dependency name (repeatable or comma-separated, case-insensitive)
- `--require-label` — only include PRs that have **all** of the given labels (repeatable or comma-separated)
- `--exclude-label` — exclude PRs that have **any** of the given labels (repeatable or comma-separated)
- `--skip-grouped` — exclude grouped Dependabot updates

These default to no restriction (everything passes) and apply identically across all three commands, so you can preview a filtered subset with `scan`/`plan` before running the same filters through `execute`.

`plan` and `execute` additionally accept:

- `--change-kind` — include only these change kinds: `patch`, `minor`, `major`, `unknown`, or `all` (repeatable or comma-separated; default: `patch,minor,unknown`, i.e. major updates excluded). Grouped PRs whose body contains a major version bump are treated as `major` for this filter.
- `--include-drafts` — include draft Dependabot PRs (default: excluded)

PRs excluded by any filter are listed with their specific reason under an `Excluded by filters` section (for `plan`/`execute`); `scan` filters silently since it's a visibility tool, not a queue.

### scan

Lists open Dependabot pull requests with metadata including classification signals: ecosystem, change kind, grouping, developer tooling, and infrastructure sensitivity.

### plan

Shows deterministic classification and the preferred processing order. By default, `plan` excludes major version updates and drafts from the planned queue and lists them separately under `Excluded by filters` along with the reason each was excluded. Grouped summary PRs are also treated as major when their PR body contains a major version bump. Included PRs are sorted into buckets — ci, developer-tooling, patch, minor, grouped, unknown, infra-sensitive, major — so that lower-risk updates are processed first.

### execute

Processes Dependabot PRs in planned order with a live progress display. By default, `execute` excludes major version updates and drafts, reports them before execution, and only processes the remaining queue. If every discovered PR is excluded, the command exits without mutating anything and prints `Nothing to do after applying filters.`. For each included PR the command:

- Inspects PR state and branch comparison
- Posts a `@dependabot rebase` comment and polls until the branch is updated if it is behind base
- Waits for CI checks to pass by polling the status check rollup
- Re-checks mergeability and branch state before merge
- Submits an approval review immediately before merge
- Merges the PR using a merge commit strategy and deletes the head branch
- Waits for post-merge CI for the merged commit on the base branch before proceeding to the next PR
- Stops on first failure (no retry or skip mode) and exits non-zero if any PR fails to process

Without `--admin`, any failed pre-merge check stops execution. With `--admin`, depflow waits for all pre-merge checks to reach a terminal state, logs a summary warning plus one warning per failed check, then continues with approval and an admin merge. The flag is forwarded as `gh pr merge --admin`, which bypasses branch protection rules. Because GitHub's status-check metadata does not distinguish policy gates from ordinary test failures, `--admin` bypasses all failed pre-merge checks. These admin-bypass warnings are emitted at warn level, so they remain visible even without `-v`.

Execute renders a live two-line progress tracker on stderr (powered by mpb) and writes any enabled logs above the progress display. The final execution summary is printed to stdout.
If the process receives `SIGINT` or `SIGTERM`, depflow cancels the active execution context so waits and polling loops can stop promptly.

#### Execute Flags

- `--dry-run` — show planned order without executing
- `--change-kind` — include only these change kinds in execution (default: `patch,minor,unknown`)
- `--include-drafts` — include draft Dependabot PRs in execution
- `--admin` — bypass branch protection rules using GitHub admin privileges
- `--poll-interval` — CI status polling interval (default: 30s, minimum: 5s)
- `--check-timeout` — maximum wait for CI checks per PR (default: 30m, must be greater than `--poll-interval`)
- `--post-merge-delay` — delay before checking post-merge CI (default: 10s)
- `--post-merge-timeout` — maximum wait for post-merge CI (default: 30m, must be greater than `--poll-interval`)
- `--show-checks` — show per-check pass/pending/fail detail on the progress line while waiting for CI, post-merge CI, and branch updates
- `--show-timing` — show elapsed wait time on the progress line and per-PR duration in the execution summary

All execute duration flags must be greater than zero. `--poll-interval` must be at least 5 seconds. `--check-timeout` and `--post-merge-timeout` must be greater than `--poll-interval`.

### version

Prints version and platform information.

```text
depflow 0.1.0 (linux/amd64)
```

## Global Flags

- `--repo [HOST/]OWNER/REPO` — target an explicit GitHub repository; if omitted, `gh` attempts to infer the current repository and `execute` resolves that repo before mutating operations
- `--limit N` — maximum number of eligible Dependabot pull requests to return after classification filtering (default: 100). Discovery expands the underlying open-PR query as needed, capped at 1000 pull requests, so PRs filtered out do not count against the limit.
- `-v, --verbose` — increase execute log verbosity (`-v` for info, `-vv` for debug, `-vvv` for trace)
- `--ecosystem`, `--exclude-ecosystem`, `--dependency`, `--exclude-dependency`, `--require-label`, `--exclude-label`, `--skip-grouped` — see [Filtering](#filtering); shared by `scan`, `plan`, and `execute`

## Output Conventions

- GitHub-derived strings printed by `scan`, `plan`, execute dry-run output, execution summaries, the live progress line (including `--show-checks` check and workflow run names), and top-level error output are sanitized to strip terminal control bytes before being written to the terminal.
- `scan`, `plan`, and `execute` print `No open Dependabot pull requests found.` when no Dependabot PRs are discovered.
- With default filtering enabled, `plan` and `execute` list excluded PRs (and why) under `Excluded by filters`, and `execute` prints `Nothing to do after applying filters.` when no eligible PRs remain.
- When repository inference fails and `--repo` was omitted, depflow includes a rerun hint using `--repo OWNER/REPO`.

## Prerequisites

- Go 1.25+
- [GitHub CLI](https://cli.github.com/) (`gh`) installed and authenticated with permission to review and merge pull requests; fine-grained tokens need pull-request write access in addition to workflow visibility

`scan`, `plan`, and `execute` all use `gh` as the GitHub transport.

## Installation

1. Download the binary for your preferred platform from the [releases](https://github.com/JakeTRogers/depflow/releases) page
2. Extract the archive (contains this README, the Apache 2.0 license, and the depflow binary)
3. Install the [GitHub CLI](https://cli.github.com/)
4. Copy the binary to a directory in your `$PATH`

## Examples

```bash
depflow scan
depflow --repo owner/repo --limit 25 scan
depflow --repo owner/repo plan
depflow --repo owner/repo plan --change-kind=all
depflow --repo owner/repo plan --ecosystem npm-and-yarn --skip-grouped
depflow --repo owner/repo execute --dry-run
depflow --repo owner/repo execute --dry-run --change-kind=all
depflow --repo owner/repo execute --exclude-label do-not-merge --require-label dependencies
depflow -v --repo owner/repo execute
depflow -vv --repo owner/repo execute --poll-interval 15s --check-timeout 10m
depflow --repo owner/repo execute --show-checks --show-timing
depflow version
```

## Shell Completion

depflow uses Cobra's built-in `completion` command. Bash, Zsh, and Fish are supported.

```bash
# Bash
source <(depflow completion bash)

# Zsh
source <(depflow completion zsh)

# Fish
depflow completion fish | source
```

## Architecture

- `cmd/` — Cobra command wiring (root, scan, plan, execute, version, discovery helpers)
- `internal/dependabot/` — PR normalization (classify ecosystem, change kind, grouping, dev-tooling, infra-sensitive signals)
- `internal/planner/` — Deterministic bucket-based ordering with tie-breaking by change kind, ecosystem, dependency name, title, and PR number
- `internal/executor/` — Sequential PR processing loop with Operator interface (dependency injection), approval before merge, and polling helpers for CI checks, branch updates, and post-merge CI
- `internal/githubcli/` — Thin `gh` CLI wrapper: list PRs, view PR details, approve, merge, comment, compare branches, list workflow runs
- `internal/progress/` — mpb-based live progress tracker with verbosity-controlled slog logger
- `internal/terminal/` — Strips terminal control bytes from untrusted GitHub-sourced strings before they reach the terminal
- `main.go` — Entry point

## Development

```bash
go test ./... -v           # Run tests
go test ./... -cover       # Coverage (must maintain ≥80%)
go build -o depflow .      # Build binary
go vet ./...               # Static analysis
```

### Pre-commit Hooks

This project uses [pre-commit](https://pre-commit.com/) hooks to ensure code quality:

- **go test** — runs all tests with race detection
- **go test coverage** — ensures cmd package maintains ≥85% coverage
- **golangci-lint** — comprehensive Go linting
- **commitizen** — enforces conventional commit messages

## License

[Apache 2.0](LICENSE)

# depflow

depflow is a Go CLI tool for discovering open Dependabot pull requests, planning a deterministic processing order, and executing that plan with live progress tracking.

## Commands

### scan

Lists open Dependabot pull requests with metadata including classification signals: ecosystem, change kind, grouping, developer tooling, and infrastructure sensitivity.

### plan

Shows deterministic classification and the preferred processing order. PRs are sorted into buckets — ci, developer-tooling, patch, minor, grouped, unknown, infra-sensitive, major — so that lower-risk updates are processed first.

### execute

Processes Dependabot PRs in planned order with a live progress display. For each PR the command:

- Inspects PR state and branch comparison
- Posts a `@dependabot rebase` comment and polls until the branch is updated if it is behind base
- Waits for CI checks to pass by polling the status check rollup
- Re-checks mergeability and branch state before merge
- Merges the PR using a merge commit strategy and deletes the head branch
- Waits for post-merge CI for the merged commit on the base branch before proceeding to the next PR
- Stops on first failure (no retry or skip mode) and exits non-zero if any PR fails to process

Execute renders a live two-line progress tracker on stderr (powered by mpb) and writes any enabled logs above the progress display. The final execution summary is printed to stdout.
If the process receives `SIGINT` or `SIGTERM`, depflow cancels the active execution context so waits and polling loops can stop promptly.

#### Execute Flags

- `--dry-run` — show planned order without executing
- `--poll-interval` — CI status polling interval (default: 30s, minimum: 5s)
- `--check-timeout` — maximum wait for CI checks per PR (default: 30m, must be greater than `--poll-interval`)
- `--post-merge-delay` — delay before checking post-merge CI (default: 10s)
- `--post-merge-timeout` — maximum wait for post-merge CI (default: 30m, must be greater than `--poll-interval`)

All execute duration flags must be greater than zero. `--poll-interval` must be at least 5 seconds. `--check-timeout` and `--post-merge-timeout` must be greater than `--poll-interval`.

### version

Prints version and platform information.

```text
depflow 0.1.0 (linux/amd64)
```

## Global Flags

- `--repo [HOST/]OWNER/REPO` — target an explicit GitHub repository; if omitted, `gh` attempts to infer the current repository and `execute` resolves that repo before mutating operations
- `--limit N` — maximum number of Dependabot pull requests to return after filtering (default: 100). Discovery expands the underlying open-PR query as needed, capped at 1000 pull requests before filtering.
- `-v, --verbose` — increase execute log verbosity (`-v` for info, `-vv` for debug, `-vvv` for trace)

## Output Conventions

- GitHub-derived strings printed by `scan`, `plan`, execute dry-run output, execution summaries, and top-level error output are sanitized to strip terminal control bytes before being written to the terminal.
- `scan`, `plan`, and `execute` print `No open Dependabot pull requests found.` when no Dependabot PRs remain after filtering.
- When repository inference fails and `--repo` was omitted, depflow includes a rerun hint using `--repo OWNER/REPO`.

## Prerequisites

- Go 1.25+
- [GitHub CLI](https://cli.github.com/) (`gh`) installed and authenticated with `repo` and `workflow` scopes

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
depflow --repo owner/repo execute --dry-run
depflow -v --repo owner/repo execute
depflow -vv --repo owner/repo execute --poll-interval 15s --check-timeout 10m
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
- `internal/executor/` — Sequential PR processing loop with Operator interface (dependency injection), polling helpers for CI checks, branch updates, and post-merge CI
- `internal/githubcli/` — Thin `gh` CLI wrapper: list PRs, view PR details, merge, comment, compare branches, list workflow runs
- `internal/progress/` — mpb-based live progress tracker with verbosity-controlled slog logger
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

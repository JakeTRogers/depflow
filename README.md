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

Developer-tooling and infrastructure-sensitive hints use keywords from the dependency name parsed from the PR title, or the lead dependency of a grouped update. Keywords match whole name segments split on punctuation, so `@aws-sdk/client-s3` matches `aws` but `drawsvg` does not. Project paths, labels, group names, and other title text do not set these hints. When the name can only be inferred from a branch, it is still displayed but does not contribute risk hints. These are keyword heuristics, not a complete assessment of dependency risk, and apply to `scan`, `plan`, and `execute` alike.

### plan

Shows deterministic classification and the preferred processing order. By default, `plan` excludes major version updates and drafts from the planned queue and lists them separately under `Excluded by filters` along with the reason each was excluded. Grouped summary PRs are also treated as major when the update list in their PR body (Dependabot's `Updates ... from A to B` lines and `Package | From | To` table) contains a major version bump; versions mentioned in bundled release notes and changelogs are ignored. Included PRs are sorted into buckets — ci, developer-tooling, patch, minor, grouped, unknown, infra-sensitive, major — so that lower-risk updates are processed first.

The default listing is a compact table with execution order, PR number, bucket, ecosystem, dependency, and change kind. Every PR has its own row in processing order. Missing values appear as `unknown`; when no dependency name is available, the title is shown instead. Long identifiers are preserved rather than truncated.

- `--details`: show full titles, classification signals, reasons, and URLs instead of the compact table
- `-o, --output FILE` — write an [editable plan file](#editing-the-plan) instead of the listing (`-` writes it to stdout); cannot be combined with `--details`
- `--force` — overwrite an existing `--output` file; without it, `plan -o` refuses to replace a file so an edited plan is not lost

```bash
depflow plan
depflow plan --details
```

### execute

Processes Dependabot PRs in planned order with a live progress display. By default, `execute` excludes major version updates and drafts, reports them before execution, and only processes the remaining queue. If every discovered PR is excluded, the command exits without mutating anything and prints `Nothing to do after applying filters.`. For each included PR the command:

- Inspects PR state and branch comparison
- Posts a `@dependabot rebase` comment and polls until the branch is updated if it is behind base
- Waits for CI checks to pass by polling the status check rollup. GitHub registers checks for a new commit asynchronously, so a PR that reports no checks is given a 30-second grace period from when its head commit is first observed before depflow treats it as having no CI. The grace period restarts whenever the observed head changes; after a rebase or an observed head change, passing checks are not trusted until it has elapsed
- Re-checks mergeability and branch state before merge, and stops if the head commit changed after its checks passed
- Submits an approval review immediately before merge
- Merges the PR using the selected method, pinned to the verified head commit (`gh pr merge --match-head-commit`), and deletes the head branch
- Waits for post-merge CI for the merged commit on the base branch before proceeding to the next PR. GitHub-managed `dynamic` runs, such as Dependabot's own update jobs, are not treated as CI. If the merge commit starts no runs within 2 minutes (for example, because path filters skipped every workflow), depflow logs a warning and continues unless `--require-post-merge-ci` is set. This grace period applies even when recent run history contains no push-triggered runs, since that does not prove push workflows are absent
- Stops on first failure (no retry or skip mode) and exits non-zero if any PR fails to process

Without `--admin`, any failed pre-merge check stops execution. With `--admin`, depflow waits for all pre-merge checks to reach a terminal state, logs a summary warning plus one warning per failed check, then continues with approval and an admin merge. The flag is forwarded as `gh pr merge --admin`, which bypasses branch protection rules. Because GitHub's status-check metadata does not distinguish policy gates from ordinary test failures, `--admin` bypasses all failed pre-merge checks. These admin-bypass warnings are emitted at warn level, so they remain visible even without `-v`.

Execute renders a live two-line progress tracker on stderr (powered by mpb) and writes any enabled logs above the progress display. The final execution summary is printed to stdout.
If the process receives `SIGINT` or `SIGTERM`, depflow cancels the active execution context so waits and polling loops can stop promptly.

#### Execute Flags

- `--dry-run` — show planned order without executing
- `--merge-method merge|squash|rebase` — override the preferred merge method for this run; see [Merge preferences](#merge-preferences)
- `--edit` — open the plan in your editor before executing; see [Editing the plan](#editing-the-plan)
- `--plan FILE` — execute a plan file written by `depflow plan -o` (`-` reads stdin); cannot be combined with `--edit`, `--limit`, `--change-kind`, `--include-drafts`, or the classification filters
- `--change-kind` — include only these change kinds in execution (default: `patch,minor,unknown`)
- `--include-drafts` — include draft Dependabot PRs in execution
- `--admin` — bypass branch protection rules using GitHub admin privileges
- `--poll-interval` — CI status polling interval (default: 30s, minimum: 5s)
- `--check-timeout` — maximum wait for CI checks per PR (default: 30m, must be greater than `--poll-interval`)
- `--post-merge-delay` — delay before checking post-merge CI (default: 10s)
- `--post-merge-timeout` — maximum wait for post-merge CI (default: 30m, must be greater than `--poll-interval`)
- `--require-post-merge-ci` — fail when a merge commit starts no workflow runs, instead of continuing with a warning
- `--show-checks` — show per-check pass/pending/fail detail on the progress line while waiting for CI, post-merge CI, and branch updates
- `--show-timing` — show elapsed wait time on the progress line and per-PR duration in the execution summary

All execute duration flags must be greater than zero. `--poll-interval` must be at least 5 seconds. `--check-timeout` and `--post-merge-timeout` must be greater than `--poll-interval`.

### Merge preferences

Choose one of GitHub CLI's three merge methods:

| Value | GitHub action | GitHub CLI flag |
| --- | --- | --- |
| `merge` | Create a merge commit | `--merge` |
| `squash` | Squash and merge | `--squash` |
| `rebase` | Rebase and merge | `--rebase` |

This choice is independent of the `@dependabot rebase` request used to bring an outdated PR branch up to date. The default remains `merge` when no preference is configured. Preferences apply to normal execution, `--edit`, and `--plan`; saved plans do not store a merge method.

```bash
depflow config set merge-method squash
depflow config set merge-method rebase --repo github.com/acme/service
depflow config set merge-method squash --local
depflow execute --repo acme/service --merge-method merge
```

Precedence, highest first:

1. Explicit `execute --merge-method`
2. `DEPFLOW_MERGE_METHOD` environment variable
3. Saved repository preference
4. Saved global preference
5. Built-in `merge` default

An explicitly empty or invalid value is an error, not a request to inherit. Unset the environment variable to restore inheritance. Configuration and environment values are validated before discovery; explicit valid run flags override environment values. Invalid saved configuration must be repaired rather than silently ignored.

For a nonempty plan, execution reports the effective method and its source on stderr and queries GitHub for repository-enabled methods. It validates every selected PR's repository method and merge queue policy before making any changes, and refreshes policy before updating each PR and immediately before approval. If the effective method is disabled, depflow stops with the allowed alternatives and a rerun hint. It never silently changes methods, even when only one is allowed. Keep the original filters or `--plan` argument when applying the suggested `--merge-method` override.

Capability lookup failures stop execution before mutations. Dry runs still print the plan and method: known policy conflicts fail, while unavailable policy data produces an explicit `unverified` warning. If repository resolution is unavailable, repository-specific preferences may also be unresolved. Dry runs never modify PRs.

The repository-enabled list is not a guarantee of mergeability: branch rulesets, linear-history requirements, permissions, conflicts, and checks can still block a merge. GitHub's decision at merge time is authoritative; policy changes can stop a partially completed run. Successfully processed PRs are not rolled back.

Merge-queue-enabled target branches are explicitly unsupported, including with `--admin`. Queues control their own merge method, and `gh` does not support this tool's `--delete-branch` flow on those branches. Depflow never adds an admin bypass to solve a preference conflict. The GitHub host must support the `isMergeQueueEnabled` GraphQL field; unsupported or unavailable policy metadata fails closed during execution.

### config

Configuration uses Viper and YAML, with a command interface similar to getRelease:

```bash
depflow config show                           # effective global preferences, YAML
depflow config show --repo acme/service       # effective preferences for a repository
depflow config show --local --show-origin     # values, sources, scope, and file path
depflow config show --format json             # machine-readable effective values
depflow config get merge-method
depflow config set merge-method squash
depflow config reset merge-method --local     # remove repository override; inherit again
depflow config reset --yes                    # reset global overrides only
depflow config reset --repo acme/service --yes # reset this repository's overrides only
depflow config edit                           # edit the complete file in $VISUAL/$EDITOR
depflow config path
```

No scope flag means global settings; `--global` makes that explicit. `--repo [HOST/]OWNER/REPO` selects a repository without a network lookup. An unqualified owner/repo uses `GH_HOST` if set, otherwise `github.com`. `--local` asks `gh` for the current repository. These scope selectors are mutually exclusive. Global configuration commands and explicit `--repo` configuration work without `gh` installed or authenticated. They validate preference values but do not check whether a repository currently allows them.

All scopes live in one user-owned file, never a file in the checkout. Repository keys are lowercase, host-qualified identities so GitHub.com and Enterprise repositories cannot share overrides accidentally. Execution uses the host from the repository URL returned by `gh`.

```yaml
version: 1
merge-method: squash
repositories:
  github.com/acme/service:
    merge-method: rebase
  git.example.com/acme/service:
    merge-method: merge
```

The default location is `depflow/config.yaml` under Go's platform-specific user configuration directory: `$XDG_CONFIG_HOME` or `$HOME/.config` on Linux, `$HOME/Library/Application Support` on macOS, and `%AppData%` on Windows. `--config PATH` selects a different YAML file instead of layering it over the default. A missing default file uses built-in defaults; an explicitly selected missing file is an error for reads and execution. `config set`, `reset`, and `edit` can create it.

Only saved overrides are written. Environment values, run flags, and inherited defaults are never persisted by `config set`. Resetting removes overrides rather than writing today's default, and does not remove other scopes. Resetting an entire scope requires `--yes`; there are no surprise confirmation prompts in scripts. `config edit` always edits the whole file, so omit scope selectors. It validates a temporary copy before replacing the original and refuses to overwrite changes made while the editor was open. Invalid edits leave the original untouched, and malformed existing files can be repaired with `config edit`.

Writes use a temporary file and a sibling `.lock` file to prevent cooperating writers from losing updates. If a process crashes during a write, remove its stale lock only after confirming no configuration writer is running. New configuration files use owner-only permissions where supported. Unknown keys, unsupported schema versions, and unreadable files are errors. Only `merge-method` is currently configurable; other execution flags remain per-run options.

### Editing the plan

To exclude specific PRs, pull in one the default filters left out, or change the processing order, edit the plan as a text file, much like a `git rebase -i` todo list:

```bash
depflow execute --edit                 # open the plan in your editor, then execute what you save
depflow plan -o plan.txt               # or save it, edit it, and run it later
depflow execute --plan plan.txt
```

```text
repo owner/repo
#
# depflow plan, generated 2026-09-27T14:03:00Z
# Lines run top to bottom; reorder them to change the order.
#   pick, p          = process this PR
#   skip, s, drop, d = leave this PR alone (deleting the line also skips it)
# Save with no pick lines to abort.

pick #31 [ci] Bump actions/checkout from 7.0.0 to 7.0.1
pick #28 [patch] Bump golang.org/x/sys from 0.46.0 to 0.47.0
pick #22 [minor] Bump github.com/spf13/cobra from 1.9.0 to 1.10.2

# Not included by default. Change "skip" to "pick" to include:
skip #40 [major] Bump foo from 1.4.0 to 2.0.0  # change-kind "major" not in --change-kind allow-list
```

- PRs run in the order of their `pick` lines. The command and PR number control execution. The optional bucket records what the PR was when planned and is checked for drift (see below), while the title is informational; changing the bucket does not change execution order.
- PRs held back only by the default `--change-kind` and draft filters, or cut by `--limit`, are listed as `skip` lines so you can include them. PRs removed by filters you pass explicitly (for example `--exclude-ecosystem npm-and-yarn` or `--change-kind patch`) are left out of the file.
- Before anything is changed, every picked PR is checked against the currently open Dependabot PRs. Picks that aren't open Dependabot PRs (merged since the plan was written, or never from Dependabot) are reported under `Not processed` and skipped, so a stale or hand-edited plan can never merge anything else.
- The `repo` line must match the target repository (`--repo`, or the one `gh` infers).
- When running a plan, depflow warns if a PR's bucket has changed since the plan was written. A pick that has become `[major]` (for example, Dependabot moved it to a new major version) is not processed and is reported under `Not processed`; change its bucket to `[major]` in the plan file to include it anyway. A `pick` line with no bucket is not checked.
- When running a saved plan, depflow also reports how many open Dependabot PRs the file doesn't list. Those PRs are left alone.
- The editor is `$VISUAL`, then `$EDITOR`, then `vi` (`notepad` on Windows). Exiting the editor with an error aborts. If the edited plan can't be parsed, depflow keeps the file and prints its path so you can fix it and rerun with `--plan`.
- `--edit` needs an interactive terminal. In scripts, use `plan -o` and `execute --plan`.

### version

Prints version and platform information.

```text
depflow 0.1.0 (linux/amd64)
```

## Global Flags

- `--config PATH` — use an explicit YAML preferences file instead of the user default
- `--repo [HOST/]OWNER/REPO` — target an explicit GitHub repository; if omitted, `gh` attempts to infer the current repository and `execute` resolves that repo before mutating operations
- `--limit N` — maximum number of eligible Dependabot pull requests to return after classification filtering (default: 100). Discovery expands the underlying open-PR query as needed, capped at 1000 pull requests, so PRs filtered out do not count against the limit. `plan` and `execute` keep the first N PRs in processing order and report how many more were cut; plan files list the cut PRs as `skip` lines. `scan` keeps the first N by PR number.
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
depflow --repo owner/repo execute --edit --dry-run
depflow --repo owner/repo plan -o plan.txt
depflow --repo owner/repo execute --plan plan.txt
depflow version
```

## Shell Completion

depflow uses Cobra's built-in `completion` command. Bash, Zsh, and Fish are supported.

`execute --merge-method <TAB>` suggests methods enabled for the selected `--repo` or the repository inferred by `gh`, with descriptions where the shell supports them. The lookup has a 500 ms deadline. Missing authentication, an unresolved repository, offline operation, or a timeout silently falls back to all three methods; a verified empty allowlist offers none. Only arguments before the cursor are available to completion, so place `--repo` before `--merge-method` when completing for another repository. Execution always validates independently. Configuration keys and `config set merge-method <TAB>` complete without network access; global preferences are not restricted by the current repository.

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
- `internal/planfile/` — Renders and parses editable plan files (`pick`/`skip` lines) used by `plan -o`, `execute --edit`, and `execute --plan`
- `internal/executor/` — Sequential PR processing loop with Operator interface (dependency injection), approval before merge, and polling helpers for CI checks, branch updates, and post-merge CI
- `internal/githubcli/` — Thin `gh` CLI wrapper: list PRs, view PR details, approve, merge, comment, compare branches, list workflow runs
- `internal/config/` — Viper-backed preferences with global and host-qualified repository scopes, validation, inheritance, and guarded YAML persistence
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

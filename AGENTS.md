# depflow

Go CLI that discovers open Dependabot pull requests, plans a deterministic processing order, and executes the plan (rebase → CI wait → approve → merge → post-merge CI wait) with live progress. All GitHub access goes through the `gh` CLI as a subprocess. Full command/flag reference: `README.md`.

## Commands

- Build: `go build -o depflow .`
- Test: `go test ./... -v` (single test: `go test ./cmd -run TestName -v`; race: `go test ./... -race`)
- Coverage: `go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out` — total must stay ≥ 80% (pre-commit gate)
- Static analysis: `go vet ./...`
- Full validation: `pre-commit run --all-files` (golangci-lint, `go test -race`, coverage gate, file hygiene)

## Architecture

- `main.go` → `cmd/` — Cobra wiring: `root.go` (global flags, dependency injection via `commandDeps`, signal-aware `ExecuteContext`), `scan.go`, `plan.go`, `execute.go`, `version.go`, plus `discovery.go` (PR discovery shared by all three commands) and `output.go` (rendering).
- `internal/dependabot/` — normalizes PRs and classifies them (ecosystem, change kind, grouping, dev-tooling, infra-sensitive); shared `--change-kind`/ecosystem/dependency/label filters live in `filter.go`. Grouped PRs count as `major` when the PR body contains a major bump.
- `internal/planner/` — deterministic bucket ordering: ci → developer-tooling → patch → minor → grouped → unknown → infra-sensitive → major, with defined tie-breaking.
- `internal/executor/` — sequential stop-on-first-failure processing loop; polling helpers in `wait.go`; sentinel errors in `errors.go`.
- `internal/githubcli/` — thin `gh` subprocess wrapper (list/view/approve/merge/comment/compare/runs); sets `GH_PAGER=""` and resolves the `gh` binary once.
- `internal/progress/` — mpb live progress tracker plus verbosity-filtered slog logger that writes above the bar.
- `internal/terminal/` — strips terminal control bytes from GitHub-sourced strings.
- Interfaces are defined at the consumer: `executor.Operator` and `executor.Progress` live in `internal/executor` and are satisfied by the package-private types returned by `githubcli.NewClient()` and `progress.NewTracker()`.

## Conventions

- Go style: `.github/instructions/go.instructions.md` (applies to `*.go`, `go.mod`, `go.sum`).
- Errors wrapped with `fmt.Errorf("context: %w", err)`; logging via stdlib `log/slog` through `progress.NewLogger()`.
- Tests: table-driven with `t.Parallel()`; `stubExecutor` records `gh` args and returns canned JSON (`internal/githubcli`); `fakeOperator` drives executor tests; package-level `sleepFunc` is swapped in tests to avoid real delays; `cmd` tests run Cobra's `Execute()` with captured stdout.
- Commits: Conventional Commits enforced by commitizen (`.cz.yaml`) via the pre-commit `commit-msg` hook. Version and CHANGELOG are managed by `cz bump` (GPG-signed annotated tags, `v$version` format).

## Gotchas

- Every PR must bump the `version` string in `cmd/version.go` — the `verBumpChkr` CI workflow fails otherwise (exempt only when the PR title starts with `ci(actions):`).
- Pre-commit rejects commits when total test coverage drops below 80%.
- Releases: pushing a `v*` tag runs GoReleaser; `README.md` ships inside the release archives, so keep it end-user-facing.
- Before running live `gh` commands in a shell, run `gh config set pager cat` to avoid interactive pager hangs (depflow's own wrapper already sets `GH_PAGER=""`).
- Anything printed from GitHub-derived data must pass through `internal/terminal` sanitization first.
- `.github/copilot-instructions.md` predates v2.0.0 and still documents the removed `--include-major`/`-M` flag and `planner.PartitionMajor()`; trust `README.md` and the code for the current `--change-kind` filter surface.

# depflow

depflow takes a repository's open Dependabot pull requests, orders them from lowest to highest risk, and merges them one at a time, waiting for CI before and after each merge.

## Language

### Pull requests

**Dependabot PR**:
An open pull request authored by Dependabot. It is the only kind of pull request depflow will ever process.
_Avoid_: update PR, bot PR

**Ecosystem**:
The Dependabot package ecosystem a PR updates, such as `go-modules`, `npm-and-yarn`, or `github-actions`.
_Avoid_: package manager, language

**Grouped update**:
A Dependabot PR that updates several dependencies at once on behalf of a Dependabot group.
_Avoid_: grouped summary PR, group PR, multi-dependency PR

**Update list**:
The dependencies and version ranges that a grouped update's description says it changes. Versions mentioned in its bundled release notes or changelogs are not part of it.
_Avoid_: changelog, release notes

**Lead dependency**:
The single dependency named in a PR's title. A grouped update titled only by its group name has none.
_Avoid_: primary package, main dependency

**Change kind**:
The semantic-version impact of a PR: patch, minor, major, or unknown. A grouped update whose update list contains a major bump has change kind major.
_Avoid_: bump type, update type, severity

**Commit update**:
A PR that moves a dependency from one commit SHA to another rather than between versions. Its change kind is unknown.
_Avoid_: SHA bump

**Security update**:
A Dependabot PR that updates a package with an open Dependabot alert in the same ecosystem.
_Avoid_: security fix, vulnerability PR

**Keyword hint**:
A keyword match on the lead dependency's name that marks a PR with a developer-tooling hint or an infra-sensitive hint. It is a heuristic, not an assessment.
_Avoid_: risk hint, risk score

**Developer-tooling hint**:
A keyword hint that the PR updates a build, lint, or test tool, so it should be merged before the PRs that tool validates.
_Avoid_: dev dependency, low-risk hint

**Infra-sensitive hint**:
A keyword hint that the PR updates a dependency tied to cloud, container, or deployment infrastructure, so it carries elevated risk.
_Avoid_: infra risk, ops dependency

**Classification**:
Everything depflow infers about a PR: its ecosystem, dependencies, change kind, grouping, keyword hints, and security status.
_Avoid_: metadata, signals

### Planning

**Bucket**:
The processing stage that sets a PR's place in the plan. The ci and developer-tooling buckets run first so every later PR is validated against the updated pipeline; the rest run from lowest to highest risk: patch, minor, grouped, unknown, infra-sensitive, major. Risk outranks sequencing: a major, infra-sensitive, or grouped PR belongs to the riskiest of those buckets even when it is also a ci or developer-tooling update.
_Avoid_: tier, category, priority, risk level

**Plan**:
The ordered list of Dependabot PRs that one run will process.
_Avoid_: queue, schedule

**Plan file**:
The editable text form of a plan, with one pick or skip line per PR.
_Avoid_: todo list, script

**Resume plan file**:
A plan file that depflow writes when a run stops early or sets PRs aside. It lists the PRs that are still unmerged.
_Avoid_: retry file, checkpoint

**Bucket drift**:
A picked PR whose current bucket differs from the bucket recorded for it in the plan file.

### Execution

**Processing**:
The full per-PR cycle: bring the branch up to date, wait for pre-merge checks, approve, merge, then wait for post-merge CI.
_Avoid_: handling, landing

**Dependabot rebase**:
Asking Dependabot to bring its own PR branch up to date with the base branch. It is unrelated to the rebase merge method.
_Avoid_: rebase (on its own)

**Pre-merge checks**:
The status checks that must pass on a PR's head commit before it is merged.
_Avoid_: PR CI, tests

**Post-merge CI**:
The workflow runs triggered on the base branch by a PR's merge commit.
_Avoid_: main CI, push CI

**Merge method**:
How GitHub combines a PR into its base branch: merge, squash, or rebase.
_Avoid_: merge strategy, merge mode

**Merge preference**:
A saved merge method at global or repository scope.
_Avoid_: merge setting, default method

**Admin merge**:
A merge that uses admin privileges to bypass branch protection, including failed pre-merge checks.
_Avoid_: force merge, override

### Outcomes

**Excluded**:
Removed by a filter before entering the plan.
_Avoid_: skipped, filtered out

**Skipped**:
Marked with a skip line in a plan file, so the run leaves it alone.
_Avoid_: dropped, excluded

**Not processed**:
Picked in a plan file but rejected when the run starts, because it is no longer an open Dependabot PR or its bucket drifted to major.
_Avoid_: skipped, rejected

**Set aside**:
Failed during processing in a run that continues past failures.
_Avoid_: skipped, ignored

**Not attempted**:
Never reached because the run stopped early.
_Avoid_: skipped, pending

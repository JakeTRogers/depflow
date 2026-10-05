## v2.3.0 (2026-10-04)

### Feat

- **execute**: add --skip-failed to continue past PR-specific failures
- mark PRs that fix open Dependabot security alerts
- **execute**: write a resume plan when execution stops early
- **filter**: accept dependabot.yml ecosystem names and warn on typos

### Fix

- **cmd**: tighten argument validation and gh error messages
- **cmd**: show FILE placeholders in plan and execute help
- **plan**: refuse to overwrite an existing plan file
- **dependabot**: read only the update list in grouped PR bodies
- **dependabot**: match risk keywords on whole name segments
- **executor**: handle merge commits that start no post-merge CI
- **cmd**: apply --limit in processing order
- **planfile**: hold back picks that became major since planning
- **dependabot**: stop treating commit SHAs as semantic versions
- **dependabot**: parse titles with repeated commit scopes
- **dependabot**: classify requirement-range version updates
- **executor**: pin merges to the CI-verified head commit

## v2.2.1 (2026-09-29)

### Fix

- prevent path-based dependency misclassification

## v2.2.0 (2026-09-29)

### Feat

- add configurable merge methods and repository preferences

## v2.1.0 (2026-09-26)

### Feat

- **execute**: add editable plan files (--edit, --plan, plan -o)

## v2.0.2 (2026-09-07)

### Fix

- **deps**: bump github.com/vbauerster/mpb/v8 from 8.12.1 to 8.16.0

## v2.0.1 (2026-07-03)

### Fix

- **cli**: add shell completion for --change-kind values

## v2.0.0 (2026-07-03)

### BREAKING CHANGE

- `-M`/`--include-major` is removed.

### Feat

- replace -M with --change-kind and add classification filters
- **execute**: surface per-check and timing detail during waits

### Fix

- report change=major for grouped PRs with a body-only major bump

## v1.2.1 (2026-06-21)

### Fix

- **deps**: bump github.com/vbauerster/mpb/v8 from 8.12.0 to 8.12.1

## v1.2.0 (2026-04-11)

### Feat

- **execute**: add --admin flag to bypass branch protection rules

## v1.1.0 (2026-03-30)

### Feat

- add --include-major flag that is disabled by default to plan and execute sub commands
- **execute**: add PR approval step and ApprovePullRequest to Operator interface

## v1.0.0 (2026-03-29)

### Feat

- initial release of depflow CLI to automate dependabot pull request workflows

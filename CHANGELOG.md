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

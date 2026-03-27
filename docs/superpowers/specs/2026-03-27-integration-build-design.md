# Integration Build Design
**Date:** 2026-03-27
**Jira:** DBMON-1816
**Purpose:** Build a deployable otelcol Docker image merging all open MySQL receiver PRs, for UI/QE lab testing.

---

## Context

Several MySQL receiver PRs are in review upstream. The UI team and QE need a deployable image with all of these changes together before any individual PR merges. This build is purely for internal lab use and must never be pushed to the upstream `open-telemetry/opentelemetry-collector-contrib` repo.

---

## PRs Included

Merged in this order (dependency order, narrowest-to-broadest):

1. `mysql-remove-processlist-eol` — PR #47121 (removal of deprecated `information_schema.processlist`)
2. `mysql-fix-log-resource-attributes` — PR #47208 (fix `mysql.instance.endpoint` on log events)
3. `mysql-add-plan-hash-to-topx` — PR #46272 (add `query_plan_hash` + join fix)
4. `tune-query-plan-obfuscation` — PR #46760 (obfuscate + EXPLAIN fixes)
5. `add-support-for-tracing` — PR #46327 (trace propagation for MySQL)
6. `mysql-add-service-resource-attributes-clean` — PR #47174 / DBMON-1593 (unique DB instance identifier) — **provisional**: may be swapped for a different branch/PR before or after the initial build

**Swapping a provisional branch:** edit `scripts/integration-branches.txt`, replace the branch name, and re-run the script. No other changes needed.

---

## Repository & Branch Strategy

- **Base:** upstream `main` (`open-telemetry/opentelemetry-collector-contrib`), fetched fresh on every run
- **Integration branch:** `integration/demo` on `cjksplunk/opentelemetry-collector-contrib`
  - Always force-pushed — treated as a scratch branch
  - Never opened as a PR against upstream
- **Branch list:** maintained in `scripts/integration-branches.txt` (one branch per line, in merge order)

---

## Script Design

**Location:** `scripts/integration-build.sh`

**Invocation:**
```bash
./scripts/integration-build.sh                              # full build (in-place)
./scripts/integration-build.sh --worktree <path>            # full build via worktree (recommended)
./scripts/integration-build.sh --continue                   # resume after unresolvable conflict
./scripts/integration-build.sh --no-push                    # build image locally, skip docker push
./scripts/integration-build.sh --no-docker                  # merge + push branch only, skip build
# Example:
./scripts/integration-build.sh --worktree /tmp/otelcol-integration --no-docker
```

**Worktree mode (recommended):** `--worktree <path>` keeps the main clone on its current branch throughout. The `integration/demo` branch is checked out in an isolated worktree at `<path>`, created automatically on first run. Use `/tmp/otelcol-integration` as the canonical path.

**Full run sequence:**
1. Fetch upstream `main` and all feature branches from `origin`
2. Abort any in-progress merge in the work directory, then reset `integration/demo` to upstream `main` HEAD
3. Enable `git rerere` (`rerere.enabled=true`, `rerere.autoupdate=true`) so recorded conflict resolutions replay automatically
4. For each branch in `scripts/integration-branches.txt`:
   - `git merge --no-ff --no-verify origin/<branch>`
   - After merge, run `go test ./...` from `receiver/mysqlreceiver/` — failure stops the build with resume instructions
   - On conflict: if rerere resolved all conflicts (no leftover markers), auto-commit and continue; otherwise print instructions, write resume index to `.integration-state`, exit non-zero
5. Force-push `integration/demo` to `origin`
6. Build binary: `GOOS=linux GOARCH=amd64 make otelcontribcol`
7. Build image: `make docker-otelcontribcol`
8. Read current tag number from `.integration-tag`, increment it
9. Tag image: `docker tag otelcontribcol:latest ckalbren559/otelcol-demo:<N>`
10. Push image: `docker push ckalbren559/otelcol-demo:<N>`
11. Print new tag number so Helm values can be updated

**Conflict resolution (automatic via rerere):**
Known conflicts between the listed branches are pre-recorded in `.git/rr-cache`. The script detects when rerere has staged all resolutions and auto-commits — no user interaction required for the current branch set.

**Conflict resolution (manual fallback):**
If a new conflict arises that rerere cannot resolve:
```
CONFLICT: merging <branch> failed.
cd /tmp/otelcol-integration
git add <conflicted files>
GIT_EDITOR=: git commit --no-verify    # NOT git merge --continue (flags rejected by this git version)
./scripts/integration-build.sh --worktree /tmp/otelcol-integration --continue
```

**Per-merge test runner:**
After each successful merge, `go test ./...` runs from `receiver/mysqlreceiver/` (must run from the module directory — not the repo root). Test failure saves state and exits with resume instructions identical to the conflict flow.

---

## Docker Image

- **Registry:** Docker Hub
- **Image:** `ckalbren559/otelcol-demo`
- **Tag:** incrementing integer (`1`, `2`, `3`, ...)
- **Tag state:** stored in `.integration-tag` (gitignored, lives in repo root)
- **Auth:** script checks `~/.docker/config.json` for `auths` entry before attempting push; fails fast with a clear message if not authenticated. (`docker info --format '{{.Username}}'` does not work in current Docker versions.)

---

## File Layout

```
scripts/
  integration-build.sh        # main script
  integration-branches.txt    # ordered branch list (one per line)
.integration-state             # resume index after conflict (gitignored)
.integration-tag               # current build number (gitignored)
.git/rr-cache/                 # rerere conflict resolution cache (shared by worktree)
docs/superpowers/specs/
  2026-03-27-integration-build-design.md  # this file
```

**Worktree path:** `/tmp/otelcol-integration` — persistent between runs. A fresh run resets it to `upstream/main` and re-merges all branches. The worktree shares the main clone's `.git/rr-cache`, so recorded resolutions apply automatically.

---

## Updating the Build

When a PR branch is updated (e.g., in response to reviewer feedback):
1. Push the updated branch to `origin` as normal
2. Re-run `./scripts/integration-build.sh` — it always starts fresh from upstream `main` and pulls latest `origin/<branch>` for each PR

When upstream `main` moves:
1. Rebase affected PR branches against new `main` (same work you'd do for the actual PRs)
2. Re-run the script — it fetches the latest upstream `main` as the base automatically

---

## Constraints

- This build is for internal lab use only
- `integration/demo` must never be proposed as a PR against `open-telemetry/opentelemetry-collector-contrib`
- The script must not touch any upstream remote or modify any tracked PR branch

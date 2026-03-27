# Integration Build Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a shell script that merges all in-flight MySQL receiver PRs onto a fresh upstream base, builds a Docker image, and pushes it to Docker Hub as `ckalbren559/otelcol-demo:<N>`.

**Architecture:** A single bash script reads an ordered branch list from a config file, merges each branch onto a reset `integration/demo` branch, then drives the existing Makefile build targets and Docker push. Conflict resolution is manual — the script pauses, prints instructions, and resumes via `--continue`. Tag state is tracked in a gitignored counter file.

**Tech Stack:** bash, git, GNU make, Docker, existing `otelcontribcol` Makefile targets.

---

## File Map

| Action | Path | Purpose |
|--------|------|---------|
| Create | `scripts/integration-build.sh` | Main integration + build script |
| Create | `scripts/integration-branches.txt` | Ordered branch list (one per line) |
| Modify | `.gitignore` | Add `.integration-state` and `.integration-tag` |

State files (not committed):
- `.integration-state` — written by script during conflict; holds the index of the branch that failed
- `.integration-tag` — holds current integer tag; created on first run if absent (starts at 0)

---

## Task 1: Add gitignore entries

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Append entries to .gitignore**

Add to the bottom of `.gitignore`:

```
# Integration build state
.integration-state
.integration-tag
```

- [ ] **Step 2: Verify**

```bash
grep -n "integration-state\|integration-tag" .gitignore
```

Expected output (line numbers will vary):
```
46:.integration-state
47:.integration-tag
```

- [ ] **Step 3: Commit**

```bash
git add .gitignore
git commit -m "chore: gitignore integration build state files"
```

---

## Task 2: Create the branch list

**Files:**
- Create: `scripts/integration-branches.txt`

- [ ] **Step 1: Create scripts/ directory and branch list**

```bash
mkdir -p scripts
```

Create `scripts/integration-branches.txt` with this exact content (one branch per line, in merge order):

```
mysql-remove-processlist-eol
mysql-fix-log-resource-attributes
mysql-add-plan-hash-to-topx
tune-query-plan-obfuscation
add-support-for-tracing
mysql-add-service-resource-attributes-clean
```

- [ ] **Step 2: Verify line count**

```bash
wc -l scripts/integration-branches.txt
```

Expected: `6 scripts/integration-branches.txt`

- [ ] **Step 3: Commit**

```bash
git add scripts/integration-branches.txt
git commit -m "chore: add integration build branch list"
```

---

## Task 3: Write the integration build script

**Files:**
- Create: `scripts/integration-build.sh`

- [ ] **Step 1: Create the script**

Create `scripts/integration-build.sh` with this content:

```bash
#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------------------
# integration-build.sh
#
# Merges all branches in scripts/integration-branches.txt onto a fresh
# upstream main base, builds the otelcontribcol binary + Docker image,
# and pushes to Docker Hub as ckalbren559/otelcol-demo:<N>.
#
# Usage:
#   ./scripts/integration-build.sh              # full build
#   ./scripts/integration-build.sh --continue   # resume after conflict
#   ./scripts/integration-build.sh --no-push    # skip docker push
#   ./scripts/integration-build.sh --no-docker  # skip binary + docker build
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BRANCHES_FILE="$SCRIPT_DIR/integration-branches.txt"
STATE_FILE="$REPO_ROOT/.integration-state"
TAG_FILE="$REPO_ROOT/.integration-tag"
INTEGRATION_BRANCH="integration/demo"
UPSTREAM_REMOTE="upstream"
ORIGIN_REMOTE="origin"
IMAGE="ckalbren559/otelcol-demo"

# --- parse flags ---
CONTINUE=false
NO_PUSH=false
NO_DOCKER=false
for arg in "$@"; do
  case "$arg" in
    --continue)   CONTINUE=true ;;
    --no-push)    NO_PUSH=true ;;
    --no-docker)  NO_DOCKER=true ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

# --- helpers ---
info()    { echo "[INFO]  $*"; }
success() { echo "[OK]    $*"; }
fail()    { echo "[ERROR] $*" >&2; exit 1; }

# --- read branch list ---
[[ -f "$BRANCHES_FILE" ]] || fail "Branch list not found: $BRANCHES_FILE"
mapfile -t BRANCHES < "$BRANCHES_FILE"
[[ ${#BRANCHES[@]} -gt 0 ]] || fail "Branch list is empty: $BRANCHES_FILE"

# --- determine start index ---
START_INDEX=0
if $CONTINUE; then
  [[ -f "$STATE_FILE" ]] || fail "--continue specified but no .integration-state file found. Run without --continue for a fresh build."
  START_INDEX=$(cat "$STATE_FILE")
  info "Resuming from branch index $START_INDEX (${BRANCHES[$START_INDEX]})"
else
  # Fresh build: reset integration/demo to upstream main
  info "Fetching upstream main..."
  git -C "$REPO_ROOT" fetch "$UPSTREAM_REMOTE" main

  info "Fetching all feature branches from origin..."
  for branch in "${BRANCHES[@]}"; do
    git -C "$REPO_ROOT" fetch "$ORIGIN_REMOTE" "$branch" || fail "Could not fetch $ORIGIN_REMOTE/$branch — does the branch exist?"
  done

  info "Resetting $INTEGRATION_BRANCH to upstream/main..."
  git -C "$REPO_ROOT" checkout -B "$INTEGRATION_BRANCH" "$UPSTREAM_REMOTE/main"
fi

# --- merge loop ---
for (( i=START_INDEX; i<${#BRANCHES[@]}; i++ )); do
  branch="${BRANCHES[$i]}"
  info "Merging $branch ($((i+1))/${#BRANCHES[@]})..."
  if ! git -C "$REPO_ROOT" merge --no-ff "$ORIGIN_REMOTE/$branch" -m "chore(integration): merge $branch"; then
    echo "$i" > "$STATE_FILE"
    echo ""
    echo "================================================================"
    echo "CONFLICT: merging '$branch' failed."
    echo ""
    echo "Resolve the conflicts, then run:"
    echo "  git add <conflicted files>"
    echo "  git merge --continue"
    echo ""
    echo "Then resume the build with:"
    echo "  ./scripts/integration-build.sh --continue"
    echo "================================================================"
    exit 1
  fi
  success "Merged $branch"
done

# Clean up state file on successful merge
rm -f "$STATE_FILE"

# --- push integration branch ---
info "Force-pushing $INTEGRATION_BRANCH to $ORIGIN_REMOTE..."
git -C "$REPO_ROOT" push --force "$ORIGIN_REMOTE" "$INTEGRATION_BRANCH"
success "Pushed $INTEGRATION_BRANCH"

# --- build ---
if ! $NO_DOCKER; then
  info "Building otelcontribcol binary (linux/amd64)..."
  GOOS=linux GOARCH=amd64 make -C "$REPO_ROOT" otelcontribcol

  info "Building Docker image..."
  make -C "$REPO_ROOT" docker-otelcontribcol

  # --- tag + push ---
  CURRENT_TAG=0
  [[ -f "$TAG_FILE" ]] && CURRENT_TAG=$(cat "$TAG_FILE")
  NEXT_TAG=$(( CURRENT_TAG + 1 ))
  echo "$NEXT_TAG" > "$TAG_FILE"

  info "Tagging image as $IMAGE:$NEXT_TAG..."
  docker tag otelcontribcol:latest "$IMAGE:$NEXT_TAG"

  if ! $NO_PUSH; then
    # Check docker auth before attempting push
    if ! docker info --format '{{.Username}}' 2>/dev/null | grep -q .; then
      fail "Not logged in to Docker Hub. Run 'docker login' first, then retry."
    fi
    info "Pushing $IMAGE:$NEXT_TAG..."
    docker push "$IMAGE:$NEXT_TAG"
    success "Pushed $IMAGE:$NEXT_TAG"
  else
    info "--no-push: skipped docker push"
  fi

  echo ""
  echo "================================================================"
  echo "Build complete."
  echo "Image: $IMAGE:$NEXT_TAG"
  echo ""
  echo "Update your Helm values:"
  echo "  image.tag: \"$NEXT_TAG\""
  echo "================================================================"
else
  info "--no-docker: skipped binary build and docker steps"
  echo ""
  echo "================================================================"
  echo "Merge complete. integration/demo pushed to $ORIGIN_REMOTE."
  echo "================================================================"
fi
```

- [ ] **Step 2: Make executable**

```bash
chmod +x scripts/integration-build.sh
```

- [ ] **Step 3: Sanity-check for syntax errors**

```bash
bash -n scripts/integration-build.sh && echo "syntax OK"
```

Expected: `syntax OK`

- [ ] **Step 4: Commit**

```bash
git add scripts/integration-build.sh
git commit -m "feat: add integration build script (DBMON-1816)"
```

---

## Task 4: Dry-run verification (merge only, no build)

This task verifies the script runs without errors up to the merge step. It does NOT build or push anything.

**Prerequisites:** All 6 feature branches must exist on `origin` (cjksplunk fork). Verify with:

```bash
git fetch origin
git branch -r | grep -E "mysql-remove-processlist-eol|mysql-fix-log-resource-attributes|mysql-add-plan-hash-to-topx|tune-query-plan-obfuscation|add-support-for-tracing|mysql-add-service-resource-attributes-clean"
```

Expected: 6 lines, one per branch.

- [ ] **Step 1: Run merge-only dry run**

```bash
cd /Users/ckalbrener/git/cjksplunk-opentelemetry-collector-contrib
./scripts/integration-build.sh --no-docker
```

Expected output (abridged):
```
[INFO]  Fetching upstream main...
[INFO]  Fetching all feature branches from origin...
[INFO]  Resetting integration/demo to upstream/main...
[INFO]  Merging mysql-remove-processlist-eol (1/6)...
[OK]    Merged mysql-remove-processlist-eol
[INFO]  Merging mysql-fix-log-resource-attributes (2/6)...
[OK]    Merged mysql-fix-log-resource-attributes
...
[OK]    Merged mysql-add-service-resource-attributes-clean
[INFO]  Force-pushing integration/demo to origin...
[OK]    Pushed integration/demo
--no-docker: skipped binary build and docker steps
================================================================
Merge complete. integration/demo pushed to origin.
================================================================
```

If a conflict occurs, follow the conflict resolution flow (see Task 5).

- [ ] **Step 2: Verify integration/demo on GitHub fork**

Check that `integration/demo` appears on `https://github.com/cjksplunk/opentelemetry-collector-contrib/branches` with commits from all 6 branches.

---

## Task 5: Conflict resolution (reference)

This task documents the resolution flow. No code to write — record it here for reference.

**When the script stops with a conflict:**

```
CONFLICT: merging 'tune-query-plan-obfuscation' failed.

Resolve the conflicts, then run:
  git add <conflicted files>
  git merge --continue

Then resume the build with:
  ./scripts/integration-build.sh --continue
```

**Steps to resolve:**

- [ ] **Step 1: Identify conflicted files**

```bash
git status
```

Look for lines beginning with `UU` (both modified) or `AA` (both added).

- [ ] **Step 2: Open each conflicted file and resolve**

Edit the file to remove `<<<<<<<`, `=======`, `>>>>>>>` markers and produce the desired merged result.

- [ ] **Step 3: Stage resolved files and complete the merge**

```bash
git add <file1> <file2> ...
git merge --continue
```

Git will open an editor for the merge commit message — accept the default or edit it, then save.

- [ ] **Step 4: Resume the script**

```bash
./scripts/integration-build.sh --continue
```

The script picks up from the branch after the one that conflicted and continues through the remaining branches, build, and push.

---

## Task 6: Full build and push

Run the full pipeline once merges are verified clean.

**Prerequisite:** `docker login` must be active. If in doubt:

```bash
docker info --format '{{.Username}}'
```

Expected: your Docker Hub username. If blank or error, run `docker login` in your terminal first.

- [ ] **Step 1: Run full build**

```bash
cd /Users/ckalbrener/git/cjksplunk-opentelemetry-collector-contrib
./scripts/integration-build.sh
```

Expected final output:
```
================================================================
Build complete.
Image: ckalbren559/otelcol-demo:1

Update your Helm values:
  image.tag: "1"
================================================================
```

- [ ] **Step 2: Verify image exists locally**

```bash
docker images ckalbren559/otelcol-demo
```

Expected: one row with tag `1`.

- [ ] **Step 3: Verify image on Docker Hub**

Check `https://hub.docker.com/r/ckalbren559/otelcol-demo/tags` — tag `1` should appear.

- [ ] **Step 4: Update Helm values**

In `/Users/ckalbrener/git/helm/mysql/<values-file>.yaml`, update the image tag to `1` (or whatever tag was printed).

---

## Notes on Swapping a Provisional Branch

To swap `mysql-add-service-resource-attributes-clean` for a different branch:

1. Edit `scripts/integration-branches.txt` — replace the branch name with the new one
2. Commit the change: `git add scripts/integration-branches.txt && git commit -m "chore: swap provisional branch in integration build"`
3. Re-run `./scripts/integration-build.sh`

No other files need to change.

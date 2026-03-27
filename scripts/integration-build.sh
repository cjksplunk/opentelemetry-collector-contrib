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
#   ./scripts/integration-build.sh                          # full build (in-place)
#   ./scripts/integration-build.sh --worktree <path>        # full build via worktree
#   ./scripts/integration-build.sh --continue               # resume after conflict
#   ./scripts/integration-build.sh --no-push                # skip docker push
#   ./scripts/integration-build.sh --no-docker              # skip binary + docker build
#
# Worktree mode:
#   --worktree <path> keeps your main clone on its current branch throughout.
#   The integration/demo branch is checked out in an isolated worktree at <path>.
#   The worktree is created automatically if it does not exist (full checkout).
#   Example: ./scripts/integration-build.sh --worktree /tmp/otelcol-integration
#   Conflict resolution: cd <path>, resolve, git add <files>,
#   then: GIT_EDITOR=: git commit --no-verify
#   then run this script again with --continue (and the same --worktree <path>).
#
# Per-merge tests:
#   After each successful merge, go test ./receiver/mysqlreceiver/... is run
#   from the work directory. A test failure stops the build with instructions.
#   Pre-commit hooks are skipped on merge commits (--no-verify) since this is
#   a scratch branch; test validation is done explicitly instead.
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
TEST_DIR="receiver/mysqlreceiver"

# --- parse flags ---
CONTINUE=false
NO_PUSH=false
NO_DOCKER=false
WORKTREE_PATH=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --continue)           CONTINUE=true; shift ;;
    --no-push)            NO_PUSH=true; shift ;;
    --no-docker)          NO_DOCKER=true; shift ;;
    --worktree)
      [[ $# -ge 2 ]] || { echo "Usage: --worktree <path>" >&2; exit 1; }
      WORKTREE_PATH="$2"; shift 2 ;;
    *) echo "Unknown argument: $1" >&2; exit 1 ;;
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

# --- resolve work directory (main clone or worktree) ---
if [[ -n "$WORKTREE_PATH" ]]; then
  WORK_DIR="$WORKTREE_PATH"
  if [[ ! -d "$WORK_DIR" ]]; then
    info "Creating worktree at $WORK_DIR (full checkout, may take a moment)..."
    git -C "$REPO_ROOT" worktree add "$WORK_DIR" "$UPSTREAM_REMOTE/main" || \
      fail "Could not create worktree at $WORK_DIR"
    success "Worktree created at $WORK_DIR"
  else
    info "Using existing worktree at $WORK_DIR"
  fi
else
  WORK_DIR="$REPO_ROOT"
fi

# Enable rerere so recorded conflict resolutions are replayed automatically
git -C "$REPO_ROOT" config rerere.enabled true
git -C "$REPO_ROOT" config rerere.autoupdate true

# --- determine start index ---
START_INDEX=0
if $CONTINUE; then
  [[ -f "$STATE_FILE" ]] || fail "--continue specified but no .integration-state file found. Run without --continue for a fresh build."
  START_INDEX=$(cat "$STATE_FILE")
  info "Resuming from branch index $START_INDEX (${BRANCHES[$START_INDEX]})"
else
  # Fresh build: fetch remotes from main clone, reset integration/demo in work dir
  info "Fetching upstream main..."
  git -C "$REPO_ROOT" fetch "$UPSTREAM_REMOTE" main

  info "Fetching all feature branches from origin..."
  for branch in "${BRANCHES[@]}"; do
    git -C "$REPO_ROOT" fetch "$ORIGIN_REMOTE" "$branch" || \
      fail "Could not fetch $ORIGIN_REMOTE/$branch — does the branch exist?"
  done

  info "Resetting $INTEGRATION_BRANCH to upstream/main in $WORK_DIR..."
  git -C "$WORK_DIR" merge --abort 2>/dev/null || true
  git -C "$WORK_DIR" checkout -B "$INTEGRATION_BRANCH" "$UPSTREAM_REMOTE/main"
fi

# --- run_tests <branch> ---
# Runs go test for the receiver package from WORK_DIR.
# Stops the build with instructions on failure.
run_tests() {
  local branch="$1"
  info "Running tests after merging $branch..."
  if ! (cd "$WORK_DIR/$TEST_DIR" && go test ./... 2>&1); then
    echo ""
    echo "================================================================"
    echo "TEST FAILURE after merging '$branch'."
    echo ""
    echo "Fix the failing tests in $WORK_DIR, then re-run:"
    RESUME_CMD="./scripts/integration-build.sh --continue"
    [[ -n "$WORKTREE_PATH" ]] && RESUME_CMD="$RESUME_CMD --worktree $WORKTREE_PATH"
    echo "  $RESUME_CMD"
    echo "================================================================"
    # Save state so --continue re-runs from this branch (re-tests after fix)
    echo "$i" > "$STATE_FILE"
    exit 1
  fi
  success "Tests passed for $branch"
}

# --- merge loop ---
for (( i=START_INDEX; i<${#BRANCHES[@]}; i++ )); do
  branch="${BRANCHES[$i]}"
  info "Merging $branch ($((i+1))/${#BRANCHES[@]})..."
  if ! git -C "$WORK_DIR" merge --no-ff --no-verify "$ORIGIN_REMOTE/$branch" \
       -m "chore(integration): merge $branch"; then
    # Check whether rerere resolved all conflicts (no leftover markers remain)
    if ! git -C "$WORK_DIR" diff --check 2>/dev/null | grep -q "leftover conflict marker"; then
      info "rerere resolved all conflicts for $branch — auto-committing..."
      GIT_EDITOR=: git -C "$WORK_DIR" commit --no-verify \
        -m "chore(integration): merge $branch"
      success "Auto-committed rerere-resolved merge of $branch"
    else
      echo "$i" > "$STATE_FILE"
      RESUME_CMD="./scripts/integration-build.sh --continue"
      [[ -n "$WORKTREE_PATH" ]] && RESUME_CMD="$RESUME_CMD --worktree $WORKTREE_PATH"
      echo ""
      echo "================================================================"
      echo "CONFLICT: merging '$branch' failed."
      echo ""
      if [[ -n "$WORKTREE_PATH" ]]; then
        echo "Resolve the conflicts in the worktree:"
        echo "  cd $WORK_DIR"
      fi
      echo "  git add <conflicted files>"
      echo "  GIT_EDITOR=: git commit --no-verify"
      echo ""
      echo "Then resume the build with:"
      echo "  $RESUME_CMD"
      echo "================================================================"
      exit 1
    fi
  fi
  success "Merged $branch"
  run_tests "$branch"
done

# Clean up state file on successful merge + test run
rm -f "$STATE_FILE"

# --- push integration branch ---
info "Force-pushing $INTEGRATION_BRANCH to $ORIGIN_REMOTE..."
git -C "$WORK_DIR" push --force "$ORIGIN_REMOTE" "$INTEGRATION_BRANCH"
success "Pushed $INTEGRATION_BRANCH"

# --- build (always runs from REPO_ROOT where the Makefile lives) ---
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
    if ! cat ~/.docker/config.json 2>/dev/null | grep -q 'auths'; then
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

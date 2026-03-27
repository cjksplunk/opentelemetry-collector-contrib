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

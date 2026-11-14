#!/bin/sh
# Board updater. Runs inside the `updater` container.
# Follows the CI image (:latest) unless image.tag pins the board to another tag.
# Never reads or prints .env values. Never touches the updater service itself.
# Use --once to run a single check (for tests).
set -u

DIR="$(pwd)"
IMAGE="ghcr.io/sadensmol/blogger"
INTERVAL="${UPDATER_INTERVAL:-300}"
STATE=""

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) updater: $*"; }

# set_state logs one line when the state changes, not on every loop.
set_state() {
  if [ "$STATE" != "$1" ]; then
    STATE="$1"
    log "$2"
  fi
}

# pinned_tag prints the pin from image.tag, or nothing when following latest.
pinned_tag() {
  tag=latest
  if [ -f "$DIR/image.tag" ]; then
    tag="$(tr -d '[:space:]' < "$DIR/image.tag")"
  fi
  if [ -n "$tag" ] && [ "$tag" != "latest" ]; then
    echo "$tag"
  fi
}

check() {
  tag="$(pinned_tag)"
  if [ -n "$tag" ]; then
    set_state "pinned:$tag" "pinned to $tag, no automatic update"
    return 0
  fi
  set_state "following" "following ${IMAGE}:latest"

  if ! err="$(BLOGGER_IMAGE_TAG=latest timeout 240 docker compose -p blogger --project-directory "$DIR" -f "$DIR/compose.yaml" pull blogger 2>&1 >/dev/null)"; then
    set_state "pull-failed" "pull failed, will retry: $(echo "$err" | tail -n 1)"
    return 0
  fi
  new_id="$(docker image inspect -f '{{.Id}}' "${IMAGE}:latest" 2>/dev/null)"
  run_id="$(docker inspect -f '{{.Image}}' blogger 2>/dev/null)"
  if [ -n "$new_id" ] && [ "$new_id" != "$run_id" ]; then
    # A manual deploy may have pinned the board during the pull.
    tag="$(pinned_tag)"
    if [ -n "$tag" ]; then
      set_state "pinned:$tag" "pinned to $tag during the pull, no update"
      return 0
    fi
    log "new image $new_id (running: ${run_id:-none}), restarting blogger"
    if BLOGGER_IMAGE_TAG=latest timeout 120 docker compose -p blogger --project-directory "$DIR" -f "$DIR/compose.yaml" up -d --no-deps blogger >/dev/null 2>&1; then
      log "blogger updated"
    else
      log "restart failed, will retry"
    fi
  fi
}

if [ "${1:-}" = "--once" ]; then
  check
  exit 0
fi

while true; do
  check
  sleep "$INTERVAL"
done

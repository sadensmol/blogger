#!/usr/bin/env bash
# Point the board at the images built by CI (:latest) and start them.
# The pull uses the board user's docker login to ghcr.io (docs/setup.md, section 6).
# Never reads, prints or copies a secret.
set -euo pipefail

HOST="${BLOGGER_BOARD_HOST:-lockbox@lockbox-nv.local}"
cd "$(dirname "$0")/.."

board() {
  local secs="$1"
  shift
  ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 "$HOST" "timeout ${secs} $*"
}

if ! board 10 "grep -q '\"ghcr.io\"' ~/.docker/config.json"; then
  echo "The board is not logged in to ghcr.io. See docs/setup.md, section 6." >&2
  exit 1
fi
scp -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 deploy/compose.yaml deploy/settings.env deploy/updater.sh "${HOST}:blogger/"
board 20 "sh -c 'echo latest > ~/blogger/image.tag'"
board 300 "sh -c 'cd ~/blogger && export BLOGGER_IMAGE_TAG=\$(cat image.tag 2>/dev/null || echo latest) && docker compose pull blogger && docker compose up -d'"
board 20 "docker logs --tail 50 blogger"

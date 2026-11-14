#!/usr/bin/env bash
# Point the board at the images built by CI (:latest) and start them.
# Never reads, prints or copies a secret.
set -euo pipefail

HOST="${BLOGGER_BOARD_HOST:-lockbox@lockbox-nv.local}"
cd "$(dirname "$0")/.."

board() {
  local secs="$1"
  shift
  ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 "$HOST" "timeout ${secs} $*"
}

scp -o ConnectTimeout=5 deploy/compose.yaml deploy/settings.env "${HOST}:blogger/"
board 20 "sh -c 'echo latest > ~/blogger/image.tag'"
board 300 "sh -c 'cd ~/blogger && BLOGGER_IMAGE_TAG=latest docker compose pull blogger && BLOGGER_IMAGE_TAG=latest docker compose up -d'"
board 20 "docker logs --tail 50 blogger"

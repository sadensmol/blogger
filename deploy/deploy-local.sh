#!/usr/bin/env bash
# Build the arm64 image on this Mac, ship it to the board and start it pinned.
# Never reads, prints or copies a secret.
set -euo pipefail

HOST="${BLOGGER_BOARD_HOST:-lockbox@lockbox-nv.local}"
IMAGE="ghcr.io/sadensmol/blogger"
cd "$(dirname "$0")/.."

SHA="$(git rev-parse --short HEAD)"
DIRTY=""
if [ -n "$(git status --porcelain)" ]; then
  DIRTY="-dirty"
fi
TAG="local-${SHA}${DIRTY}"

board() {
  local secs="$1"
  shift
  ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 "$HOST" "timeout ${secs} $*"
}

docker build --platform linux/arm64 --build-arg "TAG=${TAG}" -t "${IMAGE}:${TAG}" .

docker save "${IMAGE}:${TAG}" | gzip \
  | ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 "$HOST" 'timeout 600 sh -c "gunzip | docker load"'

scp -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 deploy/compose.yaml deploy/settings.env deploy/updater.sh "${HOST}:blogger/"
board 20 "sh -c 'echo ${TAG} > ~/blogger/image.tag'"
board 120 "sh -c 'cd ~/blogger && BLOGGER_IMAGE_TAG=\$(cat image.tag 2>/dev/null || echo latest) docker compose up -d'"
board 20 "docker logs --tail 50 blogger"

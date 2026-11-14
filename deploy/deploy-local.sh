#!/usr/bin/env bash
# Build the arm64 image on this Mac, ship it to the board and start it pinned.
# Never reads, prints or copies a secret.
set -euo pipefail

HOST="${BLOGGER_BOARD_HOST:-lockbox@lockbox-nv.local}"
IMAGE="ghcr.io/sadensmol/blogger"
cd "$(dirname "$0")/.."

SHA="$(git rev-parse --short HEAD)"
TAG="local-${SHA}"

board() {
  local secs="$1"
  shift
  ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 "$HOST" "timeout ${secs} $*"
}

docker build --platform linux/arm64 --build-arg "TAG=${TAG}" -t "${IMAGE}:${TAG}" .

docker save "${IMAGE}:${TAG}" | gzip \
  | ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 "$HOST" 'timeout 600 sh -c "gunzip | docker load"'

scp -o ConnectTimeout=5 deploy/compose.yaml deploy/settings.env "${HOST}:blogger/"
board 20 "sh -c 'echo ${TAG} > ~/blogger/image.tag'"
board 120 "sh -c 'cd ~/blogger && BLOGGER_IMAGE_TAG=\$(cat image.tag) docker compose up -d'"
board 20 "docker logs --tail 50 blogger"

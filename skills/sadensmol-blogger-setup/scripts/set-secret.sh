#!/usr/bin/env bash
# Reads one secret from stdin, checks its shape and stores it in a file with mode 600.
# Prints the key name and the value length only, never the value.
#
#   pbpaste | set-secret.sh SLACK_BOT_TOKEN ~/.config/blogger/dev.env    # writes KEY=value, replaces an old KEY line
#   pbpaste | set-secret.sh --raw GHCR_TOKEN ~/.config/blogger/ghcr-token # writes the value alone
set -euo pipefail

raw=false
if [ "${1:-}" = "--raw" ]; then
  raw=true
  shift
fi
if [ $# -ne 2 ]; then
  echo "usage: set-secret.sh [--raw] KEY FILE < value" >&2
  exit 2
fi
key="$1"
file="$2"

case "$key" in
  SLACK_APP_TOKEN) pattern='^xapp-[A-Za-z0-9-]+$' ;;
  SLACK_BOT_TOKEN) pattern='^xoxb-[A-Za-z0-9-]+$' ;;
  SLACK_CHANNEL_ID) pattern='^[CG][A-Z0-9]{6,}$' ;;
  SLACK_OWNER_USER_ID) pattern='^[UW][A-Z0-9]{6,}$' ;;
  CLAUDE_CODE_OAUTH_TOKEN) pattern='^sk-ant-oat01-[A-Za-z0-9_-]+$' ;;
  GHCR_TOKEN) pattern='^(ghp_|github_pat_)[A-Za-z0-9_]+$' ;;
  *)
    echo "unknown key ${key}" >&2
    exit 2
    ;;
esac

value="$(cat)"
value="$(printf '%s' "$value" | tr -d '[:space:]')"
if ! [[ "$value" =~ $pattern ]]; then
  echo "${key}: the input does not look right (${#value} characters). Copy it again." >&2
  exit 1
fi

umask 077
mkdir -p "$(dirname "$file")"
tmp="$(mktemp "${file}.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
if [ "$raw" = true ]; then
  printf '%s\n' "$value" > "$tmp"
else
  if [ -f "$file" ]; then
    grep -v "^${key}=" "$file" > "$tmp" || true
  fi
  printf '%s=%s\n' "$key" "$value" >> "$tmp"
fi
chmod 600 "$tmp"
mv "$tmp" "$file"
trap - EXIT
echo "${key} saved (${#value} characters)"

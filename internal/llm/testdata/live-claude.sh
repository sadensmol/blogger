#!/bin/sh
# live-claude.sh: on the Mac, claude finds its keychain login only when USER is set,
# and the runner passes no USER. On the board the login is CLAUDE_CODE_OAUTH_TOKEN.
USER=$(id -un) exec claude "$@"

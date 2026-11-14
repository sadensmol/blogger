# Blogger setup guide

This guide lists every manual step. Never paste a secret value in chat, a log or the repo. This guide names keys only.

## 1. Overview

- **Mac, local test mode.** You run the real bot on the Mac. It uses a separate dev Slack app and the channel `#news-dev`. Use it to test and debug.
- **Board.** The Jetson `lockbox-nv` runs the bot in one Docker container. It uses the production Slack app and the channel `#news`.
- **Images.** `make deploy-local` builds an image on the Mac and installs it on the board. CI will build the same image later (section 6).
- **Secrets.** Secrets live in `~/.config/blogger/` on the Mac and in `~/blogger/.env` and `~/blogger/codex/` on the board. They never enter the repo.

Use two Slack apps. Two Socket Mode connections of one app split the events. Then the board and the Mac each miss some events.

## 2. Local test mode (do this first)

1. Create a private folder: `mkdir -p ~/.config/blogger && chmod 700 ~/.config/blogger`.
2. Open https://api.slack.com/apps and choose **Create New App**. Create it from an app manifest. Pick the workspace `sadensmol`. Paste the content of `deploy/slack-manifest-dev.yaml`. Help: https://docs.slack.dev/app-manifests/configuring-apps-with-app-manifests
3. Check that **Socket Mode** is on. The manifest turns it on. Help: https://docs.slack.dev/apis/events-api/using-socket-mode
4. Under **Basic Information → App-Level Tokens**, generate a token with the scope `connections:write`. You need it for `SLACK_APP_TOKEN`.
5. Under **Install App**, install the app to the workspace. Copy the Bot User OAuth Token. You need it for `SLACK_BOT_TOKEN`. Help: https://docs.slack.dev/authentication/tokens
6. Create the channel `#news-dev`. In it, type `/invite @blogger-dev`.
7. Copy the channel ID of `#news-dev` from the channel details. It starts with `C`. You need it for `SLACK_CHANNEL_ID`.
8. Copy your member ID from your Slack profile. It starts with `U`. You need it for `SLACK_OWNER_USER_ID`.
9. Copy `deploy/dev.env.example` to `~/.config/blogger/dev.env`. Run `chmod 600 ~/.config/blogger/dev.env`. Fill in the values.
10. Check the key names only: `cut -d= -f1 ~/.config/blogger/dev.env | sort`.
11. Run `make run-local`. The bot starts on the Mac. It stores data in `.scratch/dev-data`. It uses the `claude` and `codex` programs from your PATH and your own logins. `CLAUDE_CODE_OAUTH_TOKEN` is optional.
12. In `#news-dev`, try `/show`. Try `/blogs add <url>`. React with 👍 and 👎 to a post.
13. To debug, run `make debug-local`. It starts the bot under Delve. If `dlv` is missing, the command prints how to install it.

`make run-local` fails with a clear message when `~/.config/blogger/dev.env` is missing. Set `DEV_ENV=<path>` to use another file.

## 3. Managing blogs and notes

- **In Slack.** Use `/blogs list`, `/blogs add <url>`, `/blogs remove <id|url>`, `/blogs pause <id|url>` and `/blogs resume <id|url>`.
- **In Claude Code.** The skill `manage-blogs` (`.claude/skills/manage-blogs/SKILL.md`) lists, adds, removes, pauses and resumes blogs. It also shows and changes the interest notes. It works on the board and in local test mode.
- The bot never changes `## My notes`. Change the notes only through Claude Code on the Mac.

## 4. Board (production): secrets and logins

Make the Slack app for the board like section 2, with these changes. Use `deploy/slack-manifest.yaml`. Use the channel `#news`. Use `/invite @blogger`. Save the values in `~/.config/blogger/secrets.env`:

1. `mkdir -p ~/.config/blogger && chmod 700 ~/.config/blogger`, then `touch ~/.config/blogger/secrets.env && chmod 600 ~/.config/blogger/secrets.env`.
2. Add the keys `SLACK_APP_TOKEN`, `SLACK_BOT_TOKEN`, `SLACK_CHANNEL_ID` and `SLACK_OWNER_USER_ID`.
3. Run `claude setup-token`. It opens a browser login and prints a one-year token. It does not save the token. Help: https://code.claude.com/docs/en/authentication#generate-a-long-lived-token
4. Add the key `CLAUDE_CODE_OAUTH_TOKEN` to `secrets.env`.
5. Make a separate Codex login for the board:
   - `mkdir -p ~/.config/blogger/codex && chmod 700 ~/.config/blogger/codex`
   - `CODEX_HOME=~/.config/blogger/codex codex login -c 'cli_auth_credentials_store="file"'`
   - Sign in with ChatGPT in the browser. If this fails, add `--device-auth`. First turn on device code login in your ChatGPT security settings. Help: https://developers.openai.com/codex/auth
   - Check: `CODEX_HOME=~/.config/blogger/codex codex login status` reports a ChatGPT login.
   - Do not use this Codex home on the Mac again. The board owns this login and renews it.
6. Check the key names only: `cut -d= -f1 ~/.config/blogger/secrets.env | sort`. Expect `CLAUDE_CODE_OAUTH_TOKEN`, `SLACK_APP_TOKEN`, `SLACK_BOT_TOKEN`, `SLACK_CHANNEL_ID` and `SLACK_OWNER_USER_ID`. Check: `test -s ~/.config/blogger/codex/auth.json && echo present`.

Then Claude Code copies the files to the board without printing them. `B` means `ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 lockbox@lockbox-nv.local`. For reference:

```bash
B 'timeout 10 sh -c "mkdir -p ~/blogger/codex && chmod 700 ~/blogger ~/blogger/codex"'
B 'umask 077; timeout 20 cat > ~/blogger/.env' < ~/.config/blogger/secrets.env
B 'timeout 10 cut -d= -f1 ~/blogger/.env'
B 'umask 077; timeout 20 cat > ~/blogger/codex/auth.json' < ~/.config/blogger/codex/auth.json
B 'timeout 10 stat -c "%a %u %n" ~/blogger/codex ~/blogger/codex/auth.json'
```

The last check must show `700 1000 …/codex` and `600 1000 …/auth.json`.

## 5. Deploying to the board manually

The board changes only through Docker images.

1. `make deploy-local` builds the image on the Mac for `linux/arm64`. It sends the image to the board over SSH. It pins the board to the tag `local-<sha>`. Use it for a change that CI has not built yet.
2. `make deploy-ci` points the board back at the CI images (`:latest`) and starts them.

Both commands copy `deploy/compose.yaml`, `deploy/settings.env` and `deploy/updater.sh`. They never read or copy a secret.

The `updater` container runs next to the bot. It checks for a new CI image every 5 minutes (see section 6). A manual `make deploy-local` pins the board, so the updater then does nothing.

## 6. CI/CD and automatic updates

### What CI does

The workflow `.github/workflows/ci.yml` runs on every push and pull request:

- `test` runs `go vet` and `go test -race`.
- `secret-scan` runs gitleaks over the full git history.
- `image` runs only on a push to `main`, after both jobs pass. It builds the `linux/arm64` image on a native arm64 runner. It pushes `ghcr.io/sadensmol/blogger:latest` and `ghcr.io/sadensmol/blogger:sha-<short sha>`.

CI uses only the built-in `GITHUB_TOKEN`. It needs no other secret.

### One-time steps for you

1. Create the public GitHub repo `sadensmol/blogger`. Tell Claude when it may push. Claude does not push before your go.
2. Wait for the first `image` job on `main` to finish.
3. Make the package public, so the board can pull it without a login. Open the package page on GitHub, then Package settings, then Change visibility. Guide: https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility
4. Run `make deploy-ci`. It copies the compose file and `updater.sh` to the board and starts both containers.

### How the board updates

The `updater` container reads `~/blogger/image.tag` every 5 minutes.

- No file, or the content is `latest`: it pulls `ghcr.io/sadensmol/blogger:latest`. It restarts `blogger` only when the pulled image differs from the running one.
- Any other content, for example `local-abc1234`: a manual deploy pinned the board. The updater does nothing. It logs one line when this state starts.

The updater never reads `.env` and never restarts itself. It works in the folder it starts from, which is `~/blogger`. Always run `docker compose` from `~/blogger`.

### Build on the Mac and install by hand

`make deploy-local` builds the image on the Mac and loads it into the board Docker over SSH. It writes `local-<sha>` to `image.tag`. A dirty working tree adds `-dirty`. The updater then leaves the board alone. `make deploy-ci` writes `latest` to `image.tag` and the updater follows CI again.

### See what runs on the board

```sh
ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 lockbox@lockbox-nv.local 'timeout 20 sh -c "cat ~/blogger/image.tag; docker inspect -f {{.Config.Image}} blogger; docker logs --tail 20 blogger-updater"'
```

The `blogger` log also prints the build tag (`sha-<short sha>` or `local-<sha>`) at start.

## 7. Troubleshooting

- **Logs.** Run `ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 lockbox@lockbox-nv.local 'timeout 20 docker logs --tail 100 blogger'`.
- **Wrong board clock.** While the board date is before 2026-01-01, the bot waits. It does no fetch and posts no digest. It logs `board clock is wrong`. Run `ssh … 'timeout 10 timedatectl show -p NTP -p NTPSynchronized -p Timezone'`. Expect `NTP=yes` and `NTPSynchronized=yes`.
- **"Busy right now".** One agent job runs at a time. Wait for the running job to finish, then try again.
- **`make run-local` fails with `invalid_auth`.** A Slack token in `dev.env` is wrong. Check the app-level token (`xapp-`) and the bot token (`xoxb-`).

---
name: sadensmol-blogger-setup
description: Interactive, step-by-step setup of the Blogger Slack bot from this repo, on this machine (local run) or on a remote Docker host over SSH. Walks the user through the Slack app, the LLM logins (Claude, Codex, optional Ollama), the secrets files, the deploy and the first blog import, one step at a time, reading secrets only from the clipboard. Use when the user says "set up blogger", "install blogger", "blogger setup", "deploy blogger to my board", or runs /sadensmol-blogger-setup.
---

# Blogger setup

Guide the user through the setup in `README.md`, one step at a time. Do the checks and file writes yourself. The user does only what needs a browser, a terminal with a TTY, or `sudo`.

## Rules

- **One step per message.** Say exactly what to do, with the link. Then stop and wait for "done" or "copied". Do not continue on your own.
- **Never show a secret.** Never print the content of a secrets file, a token, `~/.docker/config.json` or `docker exec … env`, and never run `set -x` near one. Key-name and count checks are fine: `cut -d= -f1 <file>`, `grep -c`. Never ask the user to paste a secret into the chat. If they do, tell them to rotate it.
- **Secrets come only from the clipboard**, through `scripts/set-secret.sh`. It checks the shape (`xapp-`, `xoxb-`, `C…`, `U…`, `sk-ant-oat01-`, `ghp_`/`github_pat_`), writes the file with mode 600 and prints the key name and length only. Clipboard command: `pbpaste` on macOS, `wl-paste -n` on Wayland, `xclip -o -selection clipboard` on X11. With none of these, ask the user how to pass the value (for example: they run the same `set-secret.sh` command in their own terminal). After each secret, clear the clipboard (`printf '' | pbcopy`, or the matching tool).
- **Every remote command** has SSH watchdogs and a far-side timeout:
  `ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 "$HOST" 'timeout <N> <command>'`
  Below, `<opts>` means these three `-o` options.
- Run all commands from the repo root. `SS=skills/sadensmol-blogger-setup/scripts/set-secret.sh`.
- Never commit or push. Never write a secret inside the repo.

## Step 0 — Ask

Ask in one message, then wait:

1. Where does the bot run? **Local** (this machine, `make run-local`) or **remote** (a Linux arm64 host with Docker, reached as `<user>@<host>`). Set `HOST` from the answer.
2. Which LLM backends? `claude`, `codex`, both, and optionally a local Ollama model.
3. Time zone (IANA, for example `Europe/Berlin`) and digest time (`HH:MM`, default `10:00`).
4. Remote only: the image source. **CI** (their fork's GitHub Actions builds `ghcr.io/<github-user>/blogger`) or **build here** (`make deploy-local`, needs Docker on this machine).

Set `FILE`: local → `~/.config/blogger/dev.env`; remote → `~/.config/blogger/secrets.env`.

## Step 1 — Check the tools

On this machine, check only what the answers need and report what is missing, with the install link:

- Local: `go version` (the version in `go.mod`), `make --version`.
- `claude --version` → https://code.claude.com/docs/en/setup
- `codex --version` → https://developers.openai.com/codex/cli
- Build here: `docker version` and `docker buildx ls` (must list `linux/arm64`).

## Step 2 — The secrets file

```bash
mkdir -p ~/.config/blogger && chmod 700 ~/.config/blogger
```

- Local: `test -f ~/.config/blogger/dev.env || cp deploy/dev.env.example ~/.config/blogger/dev.env; chmod 600 ~/.config/blogger/dev.env`
- Remote: `touch ~/.config/blogger/secrets.env && chmod 600 ~/.config/blogger/secrets.env`

If the file already has keys (`cut -d= -f1 "$FILE"`), ask which ones to keep.

## Step 3 — Slack app

Explain once: one Slack app per running bot. A local test bot and a Docker bot need two apps and two channels, because two Socket Mode connections of one app split the events.

1. Offer to copy the manifest to the clipboard: `pbcopy < deploy/slack-manifest.yaml` (YAML). If the user's editor is in JSON mode, copy JSON instead: `yq -o=json deploy/slack-manifest.yaml | pbcopy`; without `yq`, write the JSON yourself from the YAML, check it with `python3 -m json.tool`, then copy it.
   Tell the user: open https://api.slack.com/apps → **Create New App** → **From a manifest** → pick the workspace → paste on the matching YAML or JSON tab → **Create**. Wait.
2. "Open **Basic Information → App-Level Tokens → Generate Token and Scopes**. Add the scope `connections:write`. Copy the `xapp-…` token." Wait for "copied", then: `pbpaste | $SS SLACK_APP_TOKEN "$FILE"`.
3. "Open **Install App**, install to the workspace, copy the **Bot User OAuth Token** (`xoxb-…`)." Then `pbpaste | $SS SLACK_BOT_TOKEN "$FILE"`.
4. "Create the channel (for example `#news`), and in it type `/invite @blogger`. Open the channel details and copy the **Channel ID** at the bottom." Then `pbpaste | $SS SLACK_CHANNEL_ID "$FILE"`.
5. "Open your Slack profile → **⋮** → **Copy member ID**." Then `pbpaste | $SS SLACK_OWNER_USER_ID "$FILE"`. Only this user's commands, votes and clicks count.

When `set-secret.sh` rejects a value, ask the user to copy it again. Do not show what it got.

## Step 4 — LLM logins

**Local.**
- Claude: the bot uses the user's own `claude` login. If `claude` is not logged in, ask the user to run `claude` once in a terminal and log in. `CLAUDE_CODE_OAUTH_TOKEN` is optional.
- Codex: `codex login status`. If it is not logged in, ask the user to run `codex login` in a terminal.

**Remote.**
- Claude: `claude setup-token` needs a real terminal (a TTY), so ask the user to run it in their own terminal. It opens a browser login and prints a one-year `sk-ant-oat01-…` token. They copy it and say "copied". Then `pbpaste | $SS CLAUDE_CODE_OAUTH_TOKEN "$FILE"`, clear the clipboard, and ask them to clear the terminal (`clear`, or Cmd+K).
- Codex: explain why a separate login: the host renews its own Codex token, and a login shared with this machine breaks when either side renews it. Ask the user to run in their terminal (the browser opens the login URL once; add `--device-auth` when there is no browser):

  ```bash
  mkdir -p ~/.config/blogger/codex && chmod 700 ~/.config/blogger/codex
  CODEX_HOME=~/.config/blogger/codex codex login -c 'cli_auth_credentials_store="file"'
  ```

  Check: `CODEX_HOME=~/.config/blogger/codex codex login status` and `test -s ~/.config/blogger/codex/auth.json && echo present`. Tell them never to use this Codex home on this machine again.

**Ollama (optional).** Local: `ollama list`; let the user pick a model. Remote (Jetson with the NVIDIA runtime): after Step 6b, start it with `ssh <opts> "$HOST" 'cd ~/blogger && timeout 300 docker compose --profile local up -d ollama'`, then pull the model in the background with `ssh <opts> "$HOST" 'cd ~/blogger && timeout 3600 docker compose exec -T ollama ollama pull <model>'`. Then set the Step 5 keys, copy `secrets.env` again (Step 6b.4) and deploy again (Step 6b.6).

## Step 5 — Settings

These values are not secret; you may show them. Set each key in `$FILE` with:

```bash
f="$HOME/.config/blogger/dev.env" k=BLOGGER_TIME_ZONE v=Europe/Berlin  # remote: secrets.env
(umask 077; { grep -v "^$k=" "$f" || true; echo "$k=$v"; } > "$f.tmp" && mv "$f.tmp" "$f")
```

- `BLOGGER_BACKENDS`: the chosen backends in order, for example `claude,codex` (add `codex-oss` only with a model).
- `BLOGGER_TIME_ZONE` (check: `test -f /usr/share/zoneinfo/<zone>`) and `BLOGGER_DIGEST_TIME`.
- With Ollama: `BLOGGER_LOCAL_MODEL=<model>`; local also `BLOGGER_OLLAMA_URL=http://localhost:11434/v1`.

On the remote host `.env` wins over `deploy/settings.env`, so these overrides belong in `secrets.env`. Finish with `cut -d= -f1 "$FILE" | sort` and show the key names.

## Step 6a — Local run

1. Start `make run-local` in the background. It uses `.scratch/dev-data` for data and runs `claude`/`codex` as the user.
2. Watch its output for `starting` and `connected to Slack`. On `invalid_auth`, a Slack token is wrong: redo that part of Step 3.
3. Go to Step 7.

## Step 6b — Remote run

Run each check, show the result, and stop on a failure with the fix.

1. **SSH:** `ssh -o BatchMode=yes -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 "$HOST" 'timeout 10 uname -m'`. A password prompt or `Permission denied` → ask the user to run `ssh-copy-id <user>@<host>`. The result must be `aarch64`: the image is `linux/arm64` only. On another architecture, stop and suggest the local run.
2. **Docker:** `timeout 20 sh -c "docker version --format {{.Server.Version}}; docker compose version --short; systemctl is-enabled docker"`. `docker: not found` → the user installs Docker Engine and the Compose plugin with the guide for their distribution: https://docs.docker.com/engine/install/ , then runs this check again. A permission error → the user runs `sudo usermod -aG docker <user>` on the host and logs in again. Not `enabled` → the user runs `sudo systemctl enable --now docker`.
3. **Clock:** `timeout 10 sh -c "date -u; timedatectl show -p NTP -p NTPSynchronized"`. Expect the right date and `NTP=yes`, `NTPSynchronized=yes`. Else the user runs `sudo timedatectl set-ntp true`. The bot does nothing while the clock is wrong.
4. **Folder and secrets** (stdin into `umask 077` files, never printed):

   ```bash
   ssh <opts> "$HOST" 'timeout 10 sh -c "mkdir -p ~/blogger/codex && chmod 700 ~/blogger ~/blogger/codex"'
   ssh <opts> "$HOST" 'umask 077; timeout 20 cat > ~/blogger/.env' < ~/.config/blogger/secrets.env
   ssh <opts> "$HOST" 'umask 077; timeout 20 cat > ~/blogger/codex/auth.json' < ~/.config/blogger/codex/auth.json
   ssh <opts> "$HOST" 'timeout 10 cut -d= -f1 ~/blogger/.env'
   ssh <opts> "$HOST" 'timeout 10 stat -c "%a %u %n" ~/blogger/codex ~/blogger/codex/auth.json'
   ```

   Without the `codex` backend, skip the two `auth.json` lines and the `stat` check. The uid must be `1000` (the container's agent user). Else the user runs `sudo chown -R 1000:1000 ~/blogger/codex` on the host.
5. **Image, CI way.** Check that the image name is theirs: `grep -rn 'ghcr.io/.*/blogger' .github deploy Makefile`. If it is not their GitHub user, send them to README section 7 (fork, rename the image, wait for the `image` job) and wait. Then the GHCR read token:
   - "Open https://github.com/settings/tokens → **Generate new token (classic)**. Select only `read:packages`, set an expiry, generate, copy." Wait, then `pbpaste | $SS --raw GHCR_TOKEN ~/.config/blogger/ghcr-token`.
   - Ask for their GitHub user name, then log in through stdin:
     `ssh <opts> "$HOST" 'timeout 30 docker login ghcr.io -u <github-user> --password-stdin' < ~/.config/blogger/ghcr-token`
   - Check: `ssh <opts> "$HOST" 'timeout 10 sh -c "grep -c ghcr.io ~/.docker/config.json; grep -c credsStore ~/.docker/config.json"'` must print `1` then `0`.
6. **Deploy** (allow up to 10 minutes): `BLOGGER_BOARD_HOST="$HOST" make deploy-ci`, or `BLOGGER_BOARD_HOST="$HOST" make deploy-local` to build here. The scripts stop without `BLOGGER_BOARD_HOST`. Offer to save the host (not a secret) for later runs and for `manage-blogs`: `mkdir -p ~/.config/blogger && echo "$HOST" > ~/.config/blogger/board-host`; `make` reads it when the variable is unset.
7. **Verify:** `ssh <opts> "$HOST" 'timeout 20 docker logs --tail 50 blogger'`. Look for `starting` and `connected to Slack`.
8. **Reboot test (optional, ask first).** The user runs `sudo reboot` on the host. Poll SSH every 15 s for up to 5 minutes, then check `timeout 10 docker ps --format "{{.Names}} {{.Status}}"` shows `blogger` and `blogger-updater` up, and the log again.

## Step 7 — First use

1. **Blog list.** Ask the user to review `blogs.yaml`. Run a dry run, show the change lines, ask for a yes, then repeat with `--apply`:
   - Local: `(set -a; . ~/.config/blogger/dev.env; set +a; export BLOGGER_DATA_DIR="$PWD/.scratch/dev-data" BLOGGER_AGENT_DIR="$PWD/agent"; go run ./cmd/blogger sources import -) < blogs.yaml`
   - Remote: `ssh <opts> "$HOST" 'timeout 300 docker exec -i blogger blogger sources import -' < blogs.yaml`
2. **First digest.** Ask the user to type `/show` in the channel and say what they see. Watch the log while they do. "not a valid command" → the app needs the manifest update and a reinstall (README section 3). `not_in_channel` → `/invite @blogger`.
3. **Explain, briefly:** 👍/👎 on each post teach the bot; `/blogs list|add|remove|pause|resume`; `/discover` suggests up to 10 new blogs with a **+ Add** button; the digest comes daily at `BLOGGER_DIGEST_TIME`. The `manage-blogs` skill edits blogs and notes from Claude Code; it finds the host in `BLOGGER_BOARD_HOST` or `~/.config/blogger/board-host`.
4. Name where the secrets live (`~/.config/blogger/` here; `~/blogger/.env` and `~/blogger/codex/` on the host) and that they never go into the repo.

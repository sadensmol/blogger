# Blogger

Blogger is a Slack bot for one reader. It reads the blogs you follow and posts a few good articles each morning. You rate each post with 👍 or 👎, and the bot learns what you like. It is one Go program. It runs on your own computer or in Docker on a small always-on machine, for example a Jetson or another arm64 board.

**Fast path:** open this repo in [Claude Code](https://code.claude.com/docs/en/setup) and run `/sadensmol-blogger-setup`. The skill walks you through every step below, one at a time. It reads secrets only from your clipboard and never shows them.

## 1. How the parts fit

- **Bot.** One Go program (`blogger serve`). It fetches feeds every few hours, keeps everything in one SQLite file and posts to one Slack channel.
- **LLM CLIs.** The bot does not call an LLM API. It starts the official `claude` CLI (your Claude subscription) or the `codex` CLI (your ChatGPT login) to pick articles, learn from votes and find new blogs. When one fails, it tries the next one. An optional local model runs through Codex and Ollama.
- **Slack Socket Mode.** The bot opens an outgoing connection to Slack. It needs no public URL, no open port and no web server.
- **SQLite.** Blogs, articles, posts and votes live in one file in the data folder.
- **CI and updater (optional).** GitHub Actions tests every push and builds a `linux/arm64` image on `main`. The `updater` container on the Docker host pulls a new image every 5 minutes and restarts the bot.

## 2. Requirements

- A Slack workspace where you may install apps.
- At least one LLM login:
  - a Claude subscription and [Claude Code](https://code.claude.com/docs/en/setup) (`claude`), and/or
  - a ChatGPT plan and the [Codex CLI](https://developers.openai.com/codex/cli) (`codex`).
- For a local run: Go (the version in `go.mod`, now 1.27), `make`, and the `claude` and/or `codex` CLIs, logged in.
- For a Docker run: a Linux **arm64** machine (Jetson, Raspberry Pi 4/5 with a 64-bit OS, an arm64 cloud server) with Docker Engine and the Compose plugin. The image is built for `linux/arm64` only. On an x86_64 machine, use the local run.
- Optional: [Ollama](https://ollama.com) with a model, for the `codex-oss` backend.

## 3. Create the Slack app

Make one Slack app for each running bot. Two Socket Mode connections of one app share the events, so each bot misses some of them. If you run the bot locally for tests and in Docker for real use, make two apps and two channels (for example `#news-dev` and `#news`).

1. Open https://api.slack.com/apps and choose **Create New App → From a manifest**. Pick your workspace.
2. Paste the content of `deploy/slack-manifest.yaml` (use `deploy/slack-manifest-dev.yaml` for a test app). The editor has a **YAML** and a **JSON** tab. If it is in JSON mode, switch it to YAML, or paste the manifest as JSON (for example `yq -o=json deploy/slack-manifest.yaml`). Help: https://docs.slack.dev/app-manifests/configuring-apps-with-app-manifests
3. Check that **Socket Mode** is on. The manifest turns it on.
4. Open **Basic Information → App-Level Tokens**. Generate a token with the scope `connections:write`. This is `SLACK_APP_TOKEN` (starts with `xapp-`).
5. Open **Install App** and install the app to the workspace. Copy the **Bot User OAuth Token**. This is `SLACK_BOT_TOKEN` (starts with `xoxb-`).
6. Create the channel, for example `#news`. In the channel, type `/invite @blogger`.
7. Open the channel details. Copy the **Channel ID** at the bottom. This is `SLACK_CHANNEL_ID` (starts with `C`).
8. Open your Slack profile, choose **⋮ → Copy member ID**. This is `SLACK_OWNER_USER_ID` (starts with `U`). Only this user's commands, votes and clicks count.

**After a manifest change.** A new release can change the manifest, for example to add a command. Open the app, open **App Manifest**, replace the content with the file, and save. Reinstall the app when Slack asks. Until then, Slack answers a new command with "not a valid command".

## 4. Secrets

Secrets never go into the repo. Keep them in a private folder on the computer where you set things up:

```bash
mkdir -p ~/.config/blogger && chmod 700 ~/.config/blogger
```

| File | Mode | Used by | Content |
|---|---|---|---|
| `~/.config/blogger/dev.env` | 600 | local run | `SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN`, `SLACK_CHANNEL_ID`, `SLACK_OWNER_USER_ID`, optional `CLAUDE_CODE_OAUTH_TOKEN` and `BLOGGER_*` overrides |
| `~/.config/blogger/secrets.env` | 600 | Docker run (copied to `~/blogger/.env` on the host) | the same Slack keys, `CLAUDE_CODE_OAUTH_TOKEN`, optional `BLOGGER_*` overrides |
| `~/.config/blogger/codex/auth.json` | 600 | Docker run (copied to `~/blogger/codex/` on the host) | a separate Codex login for the Docker host |
| `~/.config/blogger/ghcr-token` | 600 | Docker run with CI images | a GitHub token with `read:packages` |

Check key names only, never values: `cut -d= -f1 ~/.config/blogger/dev.env`.

Settings (all optional):

| Key | Default | Meaning |
|---|---|---|
| `BLOGGER_DIGEST_TIME` | `10:00` | time of the morning digest |
| `BLOGGER_TIME_ZONE` | `Asia/Tbilisi` | IANA time zone of the digest time |
| `BLOGGER_DIGEST_SIZE` | `5` | most articles in one digest |
| `BLOGGER_BACKENDS` | `claude,codex,codex-oss` | LLM order; remove the ones you do not use |
| `BLOGGER_LOCAL_MODEL` | empty | Ollama model for `codex-oss`; empty turns it off |
| `BLOGGER_OLLAMA_URL` | `http://ollama:11434/v1` | Ollama address |
| `BLOGGER_REPEAT_AFTER`, `BLOGGER_REPEAT_MAX` | `168h`, `3` | when and how often an unrated article may come back |
| `BLOGGER_FETCH_EVERY` | `3h` | feed fetch interval |
| `BLOGGER_LOG_LEVEL` | `info` | `debug` for more detail |

## 5. Local run

The bot runs on your computer and uses your own `claude` and `codex` logins.

1. Log in to the CLIs you use: run `claude` once and log in; run `codex login`.
2. Copy `deploy/dev.env.example` to `~/.config/blogger/dev.env` and run `chmod 600 ~/.config/blogger/dev.env`. Fill in the four Slack keys. Set `BLOGGER_BACKENDS` to the CLIs you have and `BLOGGER_TIME_ZONE` to your zone.
3. Run `make run-local`. The bot keeps its data in `.scratch/dev-data` in the repo. It starts `claude` and `codex` from your `PATH`, as your user, with your logins. `CLAUDE_CODE_OAUTH_TOKEN` is optional here.
4. Check the output for `starting` and `connected to Slack`. Then type `/show` in the channel.
5. To debug, run `make debug-local`. It starts the bot under Delve (`dlv`).

Set `DEV_ENV=<path>` to use another env file. For a local model, run Ollama, pull a model, add `codex-oss` to `BLOGGER_BACKENDS` (for example `claude,codex,codex-oss`), and add `BLOGGER_LOCAL_MODEL=<model>` and `BLOGGER_OLLAMA_URL=http://localhost:11434/v1`.

## 6. Docker run

The bot runs in one container with `restart: unless-stopped`. All files on the host live in `~/blogger`: `compose.yaml`, `settings.env`, `updater.sh`, `.env` (secrets), `codex/` (the Codex login) and `image.tag`. The deploy scripts copy the public files over SSH and never read a secret. Below, `H` is this shell function; define it in your shell first:

```bash
H() { ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 <user>@<board-host> "$@"; }
```

### 6.1 Prepare the host

1. Install Docker Engine and the Compose plugin. Turn on Docker at boot: `sudo systemctl enable --now docker`. Add your user to the `docker` group, then log in again.
2. Check the architecture: `H 'timeout 10 uname -m'` must print `aarch64`.
3. Check the clock: `H 'timeout 10 timedatectl show -p NTP -p NTPSynchronized'` must print `NTP=yes` and `NTPSynchronized=yes`. The bot does nothing while the clock is before 2026.

### 6.2 Make the host secrets (on your computer)

1. Create `~/.config/blogger/secrets.env` with mode 600. Add the four Slack keys of the app for this bot.
2. Run `claude setup-token` in a terminal. It opens a browser login and prints a one-year token (`sk-ant-oat01-…`). Add it as `CLAUDE_CODE_OAUTH_TOKEN`. Clear the terminal after. Help: https://code.claude.com/docs/en/authentication#generate-a-long-lived-token
3. Make a separate Codex login for the host. The host renews its own token, so it must not share a login with your computer:

   ```bash
   mkdir -p ~/.config/blogger/codex && chmod 700 ~/.config/blogger/codex
   CODEX_HOME=~/.config/blogger/codex codex login -c 'cli_auth_credentials_store="file"'
   CODEX_HOME=~/.config/blogger/codex codex login status
   ```

   Do not use this Codex home on your computer again. Help: https://developers.openai.com/codex/auth
4. Put setting overrides such as `BLOGGER_TIME_ZONE` and `BLOGGER_BACKENDS` in `secrets.env` too. On the host, `.env` wins over `settings.env`.

### 6.3 Copy the secrets to the host

The secrets go through stdin into files that only you can read:

```bash
H 'timeout 10 sh -c "mkdir -p ~/blogger/codex && chmod 700 ~/blogger ~/blogger/codex"'
H 'umask 077; timeout 20 cat > ~/blogger/.env' < ~/.config/blogger/secrets.env
H 'umask 077; timeout 20 cat > ~/blogger/codex/auth.json' < ~/.config/blogger/codex/auth.json
H 'timeout 10 cut -d= -f1 ~/blogger/.env'
H 'timeout 10 stat -c "%a %u %n" ~/blogger/codex ~/blogger/codex/auth.json'
```

In the container, the bot runs as root and starts the CLIs as the user `blogger` (uid 1000). The `codex` folder and `auth.json` must belong to uid 1000. If `stat` shows another uid, run on the host: `sudo chown -R 1000:1000 ~/blogger/codex`.

### 6.4 Get the image

Pick one way:

- **CI image (`make deploy-ci`).** The image is `ghcr.io/<owner>/blogger:latest` from your fork's CI (section 7). The package is private by default, so log the host in once with a read token:
  1. Open https://github.com/settings/tokens → **Generate new token (classic)**. Select only `read:packages`. Set an expiry date.
  2. Save it: `umask 077; pbpaste > ~/.config/blogger/ghcr-token` (macOS; use your clipboard tool elsewhere).
  3. Log in: `H 'timeout 30 docker login ghcr.io -u <github-user> --password-stdin' < ~/.config/blogger/ghcr-token`
  4. Check: `H 'timeout 10 sh -c "grep -c ghcr.io ~/.docker/config.json; grep -c credsStore ~/.docker/config.json"'` prints `1`, then `0`. A `credsStore` hides the login from the updater.
- **Build it yourself (`make deploy-local`).** Your computer builds the `linux/arm64` image with Docker and sends it over SSH. No registry is needed. An Apple Silicon Mac builds it natively; an x86_64 computer needs Docker Buildx with QEMU.

### 6.5 Deploy

```bash
export BLOGGER_BOARD_HOST=<user>@<board-host>
make deploy-ci      # or: make deploy-local
```

The scripts stop when `BLOGGER_BOARD_HOST` is not set. To skip the `export`, write `<user>@<board-host>` to `~/.config/blogger/board-host` (one line, outside the repo); `make` reads it when the variable is unset. `make deploy-ci` stops when the host has no ghcr.io login.

Check the log: `H 'timeout 20 docker logs --tail 50 blogger'`. Look for `blogger <tag> starting` and `connected to Slack`.

**Start at boot.** Docker starts at boot (`systemctl enable docker`) and starts the containers again (`restart: unless-stopped`). Test it once: `sudo reboot` on the host, then check the log again.

**Updater.** The `updater` container reads `~/blogger/image.tag` every 5 minutes. With `latest` (set by `make deploy-ci`) it pulls the CI image and restarts the bot when the image changed. With any other tag (set by `make deploy-local`) the host is pinned and the updater does nothing. When the GitHub token expires, the updater logs `pull failed`; make a new token and repeat the login.

**Docker on the same computer.** The computer must be Linux arm64 too: the image is `linux/arm64` only. Without SSH, copy `deploy/compose.yaml`, `deploy/settings.env` and `deploy/updater.sh` to `~/blogger`, and add `.env` and `codex/` as above. Then get the image: either log in with `docker login ghcr.io -u <github-user> --password-stdin < ~/.config/blogger/ghcr-token` and run `docker compose up -d` in `~/blogger` (it pulls `:latest`), or build it in the repo with `make image` (tag `local-<sha>`, where `<sha>` is `git rev-parse --short HEAD`), write `local-<sha>` to `~/blogger/image.tag` so the updater leaves it alone, and run `BLOGGER_IMAGE_TAG=local-<sha> docker compose up -d` in `~/blogger`.

**Local model in Docker (Jetson).** Start Ollama with the GPU: `H 'cd ~/blogger && timeout 300 docker compose --profile local up -d ollama'`. Pull a model (this can take long): `H 'cd ~/blogger && timeout 3600 docker compose exec -T ollama ollama pull <model>'`. Set `BLOGGER_LOCAL_MODEL=<model>` and add `codex-oss` to `BLOGGER_BACKENDS` in `secrets.env`, copy it to `~/blogger/.env` again (section 6.3) and deploy again.

## 7. CI/CD

`.github/workflows/ci.yml` runs on every push and pull request:

- `test`: `go vet` and `go test -race`.
- `secret-scan`: gitleaks over the full history. Run it locally with `make secret-scan`.
- `image`: on a push to `main` only, after both jobs pass. It builds the `linux/arm64` image on an arm64 runner and pushes `:latest` and `:sha-<short sha>` to GHCR. It uses only the built-in `GITHUB_TOKEN`.

**Fork it.** Fork the repo on GitHub and turn on Actions. Replace the upstream image name (`grep -rn 'ghcr.io/.*/blogger' .github deploy Makefile`) with `ghcr.io/<your-github-user>/blogger` in `.github/workflows/ci.yml`, `deploy/compose.yaml`, `deploy/updater.sh`, `deploy/deploy-local.sh` and `Makefile`. Push to `main` and wait for the `image` job.

## 8. The blog list

- **First import.** `blogs.yaml` holds a starter list. Edit it, then load it. Locally:

  ```bash
  (set -a; . ~/.config/blogger/dev.env; set +a; export BLOGGER_DATA_DIR="$PWD/.scratch/dev-data" BLOGGER_AGENT_DIR="$PWD/agent"; go run ./cmd/blogger sources import --apply -) < blogs.yaml
  ```

  In Docker: `H 'timeout 300 docker exec -i blogger blogger sources import --apply -' < blogs.yaml`. Without `--apply` the command only shows the changes.
- **In Slack.** `/blogs list`, `/blogs add <url>`, `/blogs remove <id|url>`, `/blogs pause <id|url>`, `/blogs resume <id|url>`.
- **Suggestions.** `/discover` searches the web for blogs like the ones you rate up. It posts up to 10 blogs, each with a **+ Add** button. It needs the `claude` or `codex` backend.
- **In Claude Code.** The `manage-blogs` skill lists, adds and removes blogs and edits your interest notes (`## My notes`). It reaches the host through `BLOGGER_BOARD_HOST` or `~/.config/blogger/board-host`.

## 9. Daily use

- Each morning at `BLOGGER_DIGEST_TIME` the bot posts up to `BLOGGER_DIGEST_SIZE` articles. When nothing is good enough, it posts one short line.
- Type `/show` for a digest now.
- Rate each post with 👍 or 👎. The bot learns your interests from the votes and pauses a blog with many 👎.
- An article you did not rate may come back after `BLOGGER_REPEAT_AFTER`, at most `BLOGGER_REPEAT_MAX` times. A rated article never comes back.
- When the bot was off at digest time, or the digest failed, it posts the missed digest when it starts again.

## 10. Troubleshooting

- **Logs.** Local: the terminal of `make run-local`. Docker: `H 'timeout 20 docker logs --tail 100 blogger'` and `H 'timeout 20 docker logs --tail 50 blogger-updater'`.
- **Wrong clock.** The log says `board clock is wrong` and the bot waits. Turn on NTP on the host (`sudo timedatectl set-ntp true`).
- **Bot is not in the channel.** Posts fail with `not_in_channel`. Type `/invite @blogger` in the channel.
- **"/show is not a valid command".** The app has an old manifest. Update the manifest and reinstall the app (section 3).
- **The manifest editor shows JSON.** Switch it to YAML, or paste the manifest as JSON.
- **`invalid_auth`.** A Slack token is wrong. `SLACK_APP_TOKEN` starts with `xapp-`, `SLACK_BOT_TOKEN` with `xoxb-`.
- **"Busy right now".** One LLM job runs at a time. Wait and try again.
- **A blog is skipped with "larger than 32 MiB".** Its feed is too big to read. Add a smaller feed of the blog, for example a category feed.

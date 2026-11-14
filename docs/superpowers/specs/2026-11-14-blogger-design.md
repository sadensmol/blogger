# Blogger — personal blog digest in Slack

Date: 2026-11-14. Status: draft for approval.

## Intent

The user wants a short daily list of blog articles that match their interests. The articles are
posted to the Slack `#news` channel in the `sadensmol` workspace. The user rates each article
with 👍 or 👎. The ratings change what the bot picks next time. The bot gets better over time.

## Goals

- Every morning, post the best few new articles: a link and a short description for each.
- Post the same kind of digest on demand when the user types `/show` in `#news`.
- Each article is its own Slack message, so the user can react to it alone.
- Read the user's 👍/👎 reactions and update the interest profile from them.
- Use a list of blogs from the user. Also find similar blogs and add them.
- Do the LLM work with the user's Claude Code subscription, the Codex subscription, or a
  local model. One config value selects the backend.
- Run on the user's Jetson in Docker.

## Non-goals

- No web UI. Slack is the only interface.
- No multi-user support. One user, one channel.
- No full-text archive or reader. The bot posts links.
- No public URL, no Cloudflare, no hosted Slack (Deno) functions.

## Anti-goals (failure even if it works)

- The bot posts everything new. The value is the selection.
- The bot posts the same article twice.
- The bot uses a Claude subscription token outside the official `claude` binary. Anthropic does
  not allow that (Claude Code legal and compliance page).
- The bot wedges the 8 GB board (memory thrash).

## Constraints

- Host: Jetson Orin Nano 8 GB, hostname `lockbox-nv`, L4T R39 rev 2.1 (JetPack 7.2), aarch64.
  About 5.7 GiB RAM is free. 374 GB free disk.
- The memoresse lockbox stack is stopped and does not start at boot (`lockbox-stack.service`
  disabled, containers set to `restart=no`). Blogger has the board to itself.
- One LLM job runs at a time. The app queues jobs (morning digest, `/show`, profile update,
  discovery) and runs them one by one.
- `claude` and `codex` are not installed on the host. They go into the Blogger image.
- Claude Code supports Linux ARM64. Codex ships `aarch64-unknown-linux-musl` binaries.
- Slack Socket Mode: the app connects out to Slack. No inbound port is needed.

## Approaches

| # | Approach | Trade-off | Result |
|---|---|---|---|
| 1 | Hermes Agent (Slack gateway, cron, skills) | Most features exist, but its loop is hard to extend. Claude subscription only via Max extra credits. | Rejected by the user. |
| 2 | Host cron runs `claude -p`, no running app | Smallest. No `/show`, because a slash command needs a running receiver. | Rejected: `/show` is required. |
| 3 | Cloudflare Worker receives `/show`, puts a message on a Queue, the board polls | No app on the board, but extra infra and up to 1 min delay. | Rejected: the user accepts a small app on the board. |
| 4 | Cloudflared tunnel to an HTTP app on the board | Needs a running app anyway, plus a tunnel. | Rejected: Socket Mode does the same with no tunnel. |
| 5 | **Small app on the board in Docker, Slack Socket Mode, LLM work through the agent CLIs, rules as skills** | We own the code. The subscriptions are used through the official CLIs. | **Chosen.** |

## Design

### Components (one container, one process)

1. **Slack connection** — Socket Mode. Receives `/show`, `reaction_added` and
   `reaction_removed`. Posts with `chat.postMessage`.
2. **Scheduler** — in-process. Runs the morning digest at a configured time and time zone. Runs
   the feed fetch every few hours. Runs blog discovery once a week.
3. **Feed fetcher** — reads RSS/Atom feeds of all active sources. Stores new articles in
   SQLite. The article URL (normalised) is the dedupe key.
4. **LLM runner** — starts one agent CLI per job, with the job input as a file and a strict JSON
   output. Backends:
   - `claude -p --output-format json` (Claude subscription, login token from `claude setup-token`
     in `CLAUDE_CODE_OAUTH_TOKEN`),
   - `codex exec` (Codex subscription, `auth.json` mounted from the host),
   - `codex exec --oss --local-provider ollama` (local model).
   The config gives an order. On failure or bad output, the runner tries the next backend.
5. **Skills** — the selection rules live as `SKILL.md` skills in the repo. One copy serves both
   CLIs: `.claude/skills/` for Claude Code and `.agents/skills/` for Codex (a symlink or a copy at
   image build).
6. **Store** — SQLite in a Docker volume: sources, articles, posts (Slack message ts → article),
   reactions, runs.
7. **Interest profile** — a markdown file in the volume (`interests.md`). The LLM reads it on each
   pick and rewrites it after feedback. Each version is kept in SQLite for rollback.

### Skills

| Skill | Input | Output |
|---|---|---|
| `pick-articles` | candidate articles (title, source, date, excerpt), `interests.md`, max count N | JSON: 0 to N picks with url, one or two sentence description, topics, reason |
| `update-profile` | `interests.md`, recent 👍/👎 articles with topics and sources | new `interests.md` |
| `init-profile` | the blog list | first `interests.md`, made from the topics of the listed blogs |

`interests.md` has two sections. `## My notes` is the user's own text. The LLM never changes it,
and the app restores it if an LLM output changes it. `## Learned` is written by the LLM.

Picks are "up to N" with a quality bar. The skill posts fewer when few articles fit. If nothing
fits, the morning run posts one short line ("nothing worth reading today"). `/show` uses the
same rule.
| `discover-blogs` | current sources with their 👍/👎 totals | JSON: proposed blogs with feed URLs and reasons |

The app validates every JSON output. A pick URL must exist in the candidate list.

### Flows

**Digest (morning or `/show`):**
1. Fetch feeds now if the last fetch is older than one hour.
2. Candidates: articles not posted before, from the last 14 days, cut to a size the model can
   take (newest first, at most a few per source).
3. Run `pick-articles`. On `/show`, first reply "picking articles…" at once.
4. Post a header message, then one message per pick. The bot adds 👍 and 👎 to each, so the user
   only clicks.
5. Store each post with its message ts.

**Feedback:**
1. Store each user reaction (👍 or 👎) on a stored post. Ignore the bot's own reactions and other
   emoji. A removed reaction removes the vote.
2. Before each digest, if new votes exist, run `update-profile`.
3. Source score: the app keeps a per-source vote total. A source with a clearly negative total is
   paused.

**Discovery (weekly):**
1. Run `discover-blogs`. This needs web search, so only the Claude or Codex backends run it.
2. The app checks that each proposed feed URL parses. New sources start as "trial".
3. Trial sources compete in normal picks. Votes decide if they stay.

### Source management

Sources live in SQLite (url, feed url, status `active` / `trial` / `paused`, origin `user` /
`discovered`, vote totals). Two ways to change them, both on the same store:

1. **Slack `/blogs` command** in `#news`:
   - `/blogs list` — all sources with status and vote totals.
   - `/blogs add <url>` — the app finds the feed URL (the page's RSS/Atom link) and checks that it
     parses.
   - `/blogs remove <url|id>`, `/blogs pause <url|id>`, `/blogs resume <url|id>`.
   Replies are visible only to the user (ephemeral).
2. **Admin CLI in the same binary**, for Claude Code on the user's Mac (over SSH, with
   `docker exec`):
   - `blogger sources export` — prints the full list as YAML.
   - `blogger sources import <file|->` — replaces the full list from YAML. It shows the diff
     (added, removed, changed) and applies it only with `--apply`. Vote history of kept sources
     stays.
   - `blogger sources add|remove|pause|resume` — same as the Slack commands.
   A Claude Code skill in the repo (`manage-blogs`) tells Claude how to reach the board and use
   these commands.

The first blog list is loaded with `blogger sources import`.

### Deployment

- Docker Compose project `blogger` on the board, `restart: unless-stopped`, a memory limit.
- The image is built for `linux/arm64`. It holds the Go binary, the `claude` and `codex` CLIs,
  and the skills.
- Secrets in an `.env` file on the board: Slack bot token, Slack app token, channel id, Claude
  token. Codex `auth.json` mounted read-only.
- Ollama is not part of the default stack. It is an optional extra service. Only one model may be
  loaded at a time on this board.

### Slack app (manual setup by the user)

- Socket Mode on, an app-level token with `connections:write`.
- Bot scopes: `chat:write`, `commands`, `reactions:read`, `reactions:write`.
- Events: `reaction_added`, `reaction_removed`.
- Slash commands `/show` and `/blogs`.
- The bot is invited to `#news`.

## Builder's calls (marked as the designer's, can be changed)

- Language: Go. One static binary, small memory use. Libraries: `slack-go/slack` (Socket Mode),
  `mmcdole/gofeed`, a pure-Go SQLite driver.
- Default backend order: Claude, then Codex, then local.
- Default digest size: up to 5 articles.
- Morning time and time zone: config values.
- First profile: generated by `init-profile` on the first run.

## Left to the builder

- Exact prompts inside the skills.
- Candidate cut size and feed fetch interval.
- URL normalisation rules.
- Log format and retention.

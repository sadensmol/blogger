---
name: manage-blogs
description: Manage the Blogger bot's blog list and the user's interest notes on the Jetson board, or in local test mode on the Mac. Use when the user asks to list, add, remove, pause or resume blogs, show or replace the full blog list, show the interests, or change the notes.
---

# Manage blogs and notes

This skill is the only way the user changes the `## My notes` part of the interests file. The bot never changes the notes itself.

## Where the commands run

**Board (default).** The bot runs in the `blogger` container on the board `<user>@<board-host>`. Take the real address from `BLOGGER_BOARD_HOST`, else from `~/.config/blogger/board-host` (`cat` it; it is not a secret), else ask the user. Run each board command in the tables below through SSH with the watchdog options:

```bash
ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 <user>@<board-host> '<board command>'
```

**Local test mode.** When the user says they work locally, run the same `blogger` arguments on the Mac, from the repo root, with `go run ./cmd/blogger` and the dev env file. Source the env file in a subshell, then set the same data and agent folders as `make run-local`. Never print the env file:

```bash
(set -a; . ~/.config/blogger/dev.env; set +a; export BLOGGER_DATA_DIR="$PWD/.scratch/dev-data" BLOGGER_AGENT_DIR="$PWD/agent"; go run ./cmd/blogger sources export)
```

A command that reads stdin gets the file through a redirect outside the subshell:

```bash
(set -a; . ~/.config/blogger/dev.env; set +a; export BLOGGER_DATA_DIR="$PWD/.scratch/dev-data" BLOGGER_AGENT_DIR="$PWD/agent"; go run ./cmd/blogger sources import -) < .scratch/sources.yaml
```

## Blogs

| User asks | Board command |
|---|---|
| list the blogs | `timeout 20 docker exec blogger blogger sources list` |
| add a blog (the page that lists its posts; a blog without a feed needs at least 3 articles on that page) | `timeout 60 docker exec blogger blogger sources add <url>` |
| remove a blog | `timeout 60 docker exec blogger blogger sources remove <id-or-url>` |
| pause a blog | `timeout 60 docker exec blogger blogger sources pause <id-or-url>` |
| resume a blog | `timeout 60 docker exec blogger blogger sources resume <id-or-url>` |
| show the full list | `timeout 60 docker exec blogger blogger sources export` |

To replace the full list:

1. Save the output of `sources export` to `.scratch/sources.yaml`.
2. Edit the file as the user asks. Keep its YAML shape. Keep every field the user does not change, as exported: `id`, `url`, `feed`, `title`, `status`, `origin`, `up` and `down`.
   - `url` is the page that lists the blog's posts, for example `https://example.com/writing`. The bot reads this page and the feed. The Slack lists link to it.
   - `feed` is optional. Leave it out for a blog without a feed. A new entry without `feed` gets the feed found at its page, when the blog has one.
   - To move a blog to a better page, change its `url` and keep its `feed`. The blog keeps its votes and articles.
3. Run a dry run. The `< file` redirect stays outside the ssh quotes, so it runs on the Mac:

   ```bash
   ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 <user>@<board-host> 'timeout 300 docker exec -i blogger blogger sources import -' < .scratch/sources.yaml
   ```

4. Show the user the change lines (`+ add`, `- remove`, `~ change`). Ask for a yes.
5. Only after the user's yes, run it again with `--apply`:

   ```bash
   ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 <user>@<board-host> 'timeout 300 docker exec -i blogger blogger sources import --apply -' < .scratch/sources.yaml
   ```

## Interests and notes

Show the interests:

```bash
ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 <user>@<board-host> 'timeout 20 docker exec blogger blogger profile show'
```

It prints the whole interests file, or `No interests file yet.`

To change the notes:

1. Run `profile show`. Read the current `## My notes` text.
2. Write the new notes text to `.scratch/notes.md`: the text only, without the `## My notes` heading.
3. Show the new notes to the user. Ask for a yes.
4. Only after the user's yes, send the notes. The `< file` redirect stays outside the ssh quotes:

   ```bash
   ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 <user>@<board-host> 'timeout 20 docker exec -i blogger blogger profile set-notes -' < .scratch/notes.md
   ```

   It prints `Notes saved.`
5. Run `profile show` again. Show the user the result.

The bot rewrites `## Learned` itself from the votes. Do not change `## Learned`.

## Secrets

Never print, copy or show secrets. Never print or `cat` the files in `~/.config/blogger/`, `~/blogger/.env` or `~/blogger/codex/`, or the output of `docker exec … env`. Sourcing `~/.config/blogger/dev.env` in a subshell for local test mode is allowed. When you must check a key, print its name only.

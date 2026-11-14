---
name: manage-blogs
description: Manage the Blogger bot's blog list and the user's interest notes on the Jetson board, or in local test mode on the Mac. Use when the user asks to list, add, remove, pause or resume blogs, show or replace the full blog list, show the interests, or change the notes.
---

# Manage blogs and notes

This skill is the only way the user changes the `## My notes` part of the interests file. The bot never changes the notes itself.

## Where the commands run

**Board (default).** The bot runs in the `blogger` container on `lockbox@lockbox-nv.local`. Run each board command in the tables below through SSH with the watchdog options:

```bash
ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 lockbox@lockbox-nv.local '<board command>'
```

Put a `< file` redirect outside the quotes, so the local file goes to the container's input:

```bash
ssh -o ConnectTimeout=5 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 lockbox@lockbox-nv.local 'timeout 300 docker exec -i blogger blogger sources import -' < .scratch/sources.yaml
```

**Local test mode.** When the user says they work locally, run the same `blogger` arguments on the Mac, from the repo root, with `go run ./cmd/blogger` and the dev env file. Load the env file in a subshell. Never print it:

```bash
(set -a; . ~/.config/blogger/dev.env; set +a; go run ./cmd/blogger sources export)
(set -a; . ~/.config/blogger/dev.env; set +a; go run ./cmd/blogger sources import -) < .scratch/sources.yaml
```

## Blogs

| User asks | Board command |
|---|---|
| list the blogs | `timeout 20 docker exec blogger blogger sources list` |
| add a blog | `timeout 60 docker exec blogger blogger sources add <url>` |
| remove a blog | `timeout 60 docker exec blogger blogger sources remove <id-or-url>` |
| pause a blog | `timeout 60 docker exec blogger blogger sources pause <id-or-url>` |
| resume a blog | `timeout 60 docker exec blogger blogger sources resume <id-or-url>` |
| show the full list | `timeout 60 docker exec blogger blogger sources export` |

To replace the full list:

1. Save the output of `sources export` to `.scratch/sources.yaml`.
2. Edit the file as the user asks. Keep its YAML shape: `sources:` with `url`, `feed`, `title` and `status` entries.
3. Run a dry run: `timeout 300 docker exec -i blogger blogger sources import - < .scratch/sources.yaml`.
4. Show the user the change lines (`+ add`, `- remove`, `~ change`). Ask for a yes.
5. Only after the user's yes, run it again with `--apply`: `timeout 300 docker exec -i blogger blogger sources import --apply - < .scratch/sources.yaml`.

## Interests and notes

| User asks | Board command |
|---|---|
| show the interests | `timeout 20 docker exec blogger blogger profile show` |
| change the notes | `timeout 20 docker exec -i blogger blogger profile set-notes - < .scratch/notes.md` |

`profile show` prints the whole interests file, or `No interests file yet.`

To change the notes:

1. Run `profile show`. Read the current `## My notes` text.
2. Write the new notes text to `.scratch/notes.md`: the text only, without the `## My notes` heading.
3. Show the new notes to the user. Ask for a yes.
4. Only after the user's yes, run `profile set-notes`. It prints `Notes saved.`
5. Run `profile show` again. Show the user the result.

The bot rewrites `## Learned` itself from the votes. Do not change `## Learned`.

## Secrets

Never print, copy or show secrets. Never read or print `~/blogger/.env`, the files in `~/blogger/codex/` or `~/.config/blogger/`, or the output of `docker exec … env`. When you must check a key, print its name only.

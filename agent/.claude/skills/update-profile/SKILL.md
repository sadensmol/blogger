---
name: update-profile
description: Update the user's interests file from their votes on posted articles. Use when a job asks for update-profile with an input JSON file.
---

# Update the interests profile

1. Read the input file. Its path is in the prompt. It is JSON:
   - `interests_md`: the current interests file.
   - `votes`: a list of votes. Each has `vote` (`up` or `down`), `title`, `url`, `source` and `topics`.
2. Keep the `## My notes` section exactly as it is: the same heading, the same text, character for character. The user owns it. Never edit, move or add to it.
3. Rewrite the `## Learned` section from the votes and the old `## Learned` text:
   - `up` votes: the user wants more of these topics and blogs.
   - `down` votes: the user wants less of these topics and blogs.
   - Keep what the old `## Learned` says unless new votes go against it.
4. Stay short: a few plain bullet lines, at most about 15. Merge lines that say the same thing.
5. Keep any text above `## My notes` as it is. Keep both headings. `## Learned` must be in the result.
6. Return the whole file, not only the changed section.

Answer with the JSON object only:

```json
{"interests_md": "<the whole interests file>"}
```

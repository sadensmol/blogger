---
name: init-profile
description: Build the first learned interests of the user from the blogs they follow. Use when a job asks for init-profile with an input JSON file.
---

# Build the first interests profile

1. Read the input file. Its path is in the prompt. It is JSON:
   - `interests_md`: the current interests file. It can hold only `## My notes`, or nothing.
   - `sources`: the blogs the user follows. Each has `title`, `site_url` and `recent_titles` (titles of recent articles).
2. Build the `## Learned` section from the blog titles and the recent article titles:
   - Use the user's `## My notes` from the input as the strongest hint.
   - Name the topics and kinds of articles the user most likely wants.
   - Write a few plain bullet lines, at most about 15.
3. Keep the `## My notes` section exactly as it is: the same heading, the same text, character for character. Never edit, move or add to it.
4. Keep any text above `## My notes` as it is. When the input file is empty, start it with `# Interests`.
5. The result must have both headings, `## My notes` and then `## Learned`. `## Learned` must not be empty.
6. Return the whole file.

Answer with the JSON object only:

```json
{"interests_md": "<the whole interests file>"}
```

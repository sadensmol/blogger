---
name: update-profile
description: Update the user's interests file from their votes on posted articles. Use when a job asks for update-profile with an input JSON file.
---

# Update the interests profile

1. Read the input file. Its path is in the prompt. It is JSON:
   - `interests_md`: the current interests file.
   - `votes`: a list of votes. Each has `vote` (`up` or `down`), `title`, `url`, `source` and `tags`. `tags` are the themes of the voted article. They can be empty.
   - `themes`: the theme scores over all votes. Each has `tag`, `up` and `down`: the 👍 and 👎 votes on articles with this tag. The list can be empty.
2. Keep the `## My notes` section exactly as it is: the same heading, the same text, character for character. The user owns it. Never edit, move or add to it.
3. Rewrite the `## Learned` section from the themes, the votes and the old `## Learned` text:
   - A vote is about the theme of the article, never about its blog. Learn themes only.
   - Themes with more `up` than `down` votes: the user wants more of these themes.
   - Themes with more `down` than `up` votes: the user wants less of these themes.
   - Use the vote titles and tags to name each theme clearly.
   - Never write that the user likes or dislikes a blog. Remove such lines from the old `## Learned` text.
   - Keep what the old `## Learned` says about themes unless the scores go against it.
4. Stay short: a few plain bullet lines, at most about 15. Merge lines that say the same thing.
5. Keep any text above `## My notes` as it is. Keep both headings. `## Learned` must be in the result.
6. Return the whole file, not only the changed section.

Answer with the JSON object only:

```json
{"interests_md": "<the whole interests file>"}
```

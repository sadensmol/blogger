---
name: pick-articles
description: Choose the blog articles that fit the user best from a list of candidates. Use when a job asks for pick-articles with an input JSON file.
---

# Pick articles

1. Read the input file. Its path is in the prompt. It is JSON:
   - `min`: the fewest picks you must return. It is 0 or 1.
   - `max`: the most picks you may return.
   - `interests_md`: the user's interests file.
   - `candidates`: a list of articles, best theme first. Each has `url`, `title`, `source`, `published`, `excerpt`, `shown` and `tags`. `published` is empty when the date is unknown. `shown` is how many times the user saw the article before without a vote. It is 0 for a new article. `tags` are the themes of the article. They can be empty.
   - `themes`: the user's theme scores. Each has `tag`, `up` and `down`: the 👍 and 👎 votes on articles with this tag. The list can be empty.
2. Judge each candidate by its themes and its content against the interests file:
   - First against `## My notes`. The user wrote these notes. They are the strongest rule.
   - Then against `## Learned`. These lines come from the user's past votes.
   - Then against `themes`. Prefer articles whose tags have more `up` than `down` votes. Avoid articles whose tags have more `down` than `up` votes. A tag that is not in `themes` is neutral.
   - Never prefer or avoid an article because of its blog (`source`). A vote is about the theme of an article, not about its blog.
   - Older articles are fine. Judge by interest, not by date.
   - When two articles fit about equally well, prefer the one with `shown` 0. An article with `shown` above 0 is fine when it fits the interests well.
3. Choose between `min` and `max` articles:
   - When `min` is 0: choose fewer when few articles fit. Choose none when nothing fits. Never fill the list with weak matches.
   - When `min` is 1: the user asked for something to read now. Always choose at least one: the best candidate, even when it fits only a little. Add more only when they fit well.
4. Copy each candidate `url` exactly, character for character. Never change, shorten or invent a URL. Never pick the same URL twice.
5. For each pick, write:
   - `description`: 1–2 plain sentences that say what the article is about. At most 300 characters. No marketing words.
   - `topics`: 1–4 short lower-case topics, for example `go`, `databases`, `distributed systems`.
   - `reason`: one short sentence that says why it fits the user.
6. Do not open the article URLs. Use only the input file.

Answer with the JSON object only:

```json
{"picks": [{"url": "...", "description": "...", "topics": ["..."], "reason": "..."}]}
```

When `min` is 0 and nothing fits, answer `{"picks": []}`.

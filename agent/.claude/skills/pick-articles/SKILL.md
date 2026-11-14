---
name: pick-articles
description: Choose the blog articles that fit the user best from a list of candidates. Use when a job asks for pick-articles with an input JSON file.
---

# Pick articles

1. Read the input file. Its path is in the prompt. It is JSON:
   - `min`: the fewest picks you must return. It is 0 or 1.
   - `max`: the most picks you may return.
   - `interests_md`: the user's interests file.
   - `candidates`: a list of articles. Each has `url`, `title`, `source`, `published` and `excerpt`. `published` is empty when the date is unknown.
2. Judge each candidate against the interests file:
   - First against `## My notes`. The user wrote these notes. They are the strongest rule.
   - Then against `## Learned`. These lines come from the user's past votes.
   - Older articles are fine. Judge by interest, not by date.
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

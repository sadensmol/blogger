---
name: pick-articles
description: Choose the best new blog articles for the user's daily digest from a list of candidates. Use when a job asks for pick-articles with an input JSON file.
---

# Pick articles

1. Read the input file. Its path is in the prompt. It is JSON:
   - `max`: the most picks you may return.
   - `interests_md`: the user's interests file.
   - `candidates`: a list of articles. Each has `url`, `title`, `source`, `published` and `excerpt`.
2. Judge each candidate against the interests file:
   - First against `## My notes`. The user wrote these notes. They are the strongest rule.
   - Then against `## Learned`. These lines come from the user's past votes.
3. Choose up to `max` articles. Choose fewer when few articles fit. Choose none when nothing fits. Never fill the list with weak matches.
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

When nothing fits, answer `{"picks": []}`.

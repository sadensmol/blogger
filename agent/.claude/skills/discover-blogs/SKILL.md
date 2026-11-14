---
name: discover-blogs
description: Search the web for new blogs the user may like, based on how they voted on the blogs they follow. Use when a job asks for discover-blogs with an input JSON file.
---

# Discover new blogs

1. Read the input file. Its path is in the prompt. It is JSON:
   - `max`: the most blogs you may propose.
   - `sources`: the blogs the user already has. Each has `title`, `site_url`, `feed_url`, `status`, `up` and `down` (vote counts).
2. Find what the user likes: blogs with more `up` than `down` votes are well voted. Blogs with more `down` votes are disliked.
3. Search the web for blogs like the well-voted ones and unlike the down-voted ones. Prefer active personal or engineering blogs with real articles.
4. Propose up to `max` blogs.
5. Look for a working RSS or Atom feed for each blog. Fetch the feed URL to check that it answers with a feed. A blog without a feed is fine when its page lists at least 3 articles.
6. Never propose a blog that is in `sources`, in any status. Compare by site and by feed URL.
7. For each blog, write:
   - `site_url`: the page that lists the blog's posts, for example `https://example.com/writing` or `https://example.com/blog`. Use the site's home page only when the posts are listed there. Open the page to check that it lists posts.
   - `feed_url`: the RSS or Atom feed URL, or an empty string when the blog has no feed.
   - `title`: the blog's name.
   - `about`: one or two plain sentences on what the blog writes about. No markup, no links.
   - `reason`: one short sentence that says why it fits the user.

Answer with the JSON object only:

```json
{"blogs": [{"site_url": "...", "feed_url": "...", "title": "...", "about": "...", "reason": "..."}]}
```

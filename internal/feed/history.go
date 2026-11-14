package feed

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/html"

	"github.com/sadensmol/blogger/internal/domain"
)

const (
	maxSitemaps  = 20
	maxTitleRune = 300
)

var (
	defaultSitemapPaths = []string{"/sitemap.xml", "/sitemap_index.xml"}
	lastModLayouts      = []string{time.RFC3339, "2006-01-02T15:04Z07:00", time.DateOnly}
	listingSegments     = []string{"tag", "tags", "category", "categories", "page", "author"}
)

type SitemapEntry struct {
	URL     domain.URL
	LastMod time.Time
}

type ArticleInfo struct {
	Title, Description string
	PublishedAt        time.Time // zero: the page gives no article:published_time
}

type sitemapXML struct {
	Sitemaps []sitemapLocXML `xml:"sitemap"`
	URLs     []sitemapLocXML `xml:"url"`
}

type sitemapLocXML struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

// hostGates lets one request at a time reach a host, with a pause between them.
type hostGates struct {
	delay time.Duration
	mu    sync.Mutex
	hosts map[string]*hostGate
}

type hostGate struct {
	mu   sync.Mutex
	last time.Time
}

// enter waits for the host's turn; the caller calls leave when its request ends.
func (g *hostGates) enter(ctx context.Context, host string) (func(), error) {
	g.mu.Lock()
	gate, ok := g.hosts[host]
	if !ok {
		gate = &hostGate{}
		g.hosts[host] = gate
	}
	g.mu.Unlock()

	gate.mu.Lock()
	if wait := time.Until(gate.last.Add(g.delay)); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			gate.mu.Unlock()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return func() {
		gate.last = time.Now()
		gate.mu.Unlock()
	}, nil
}

// FetchPaged reads page n of a feed that pages like WordPress, with ?paged=n.
func (c *Client) FetchPaged(ctx context.Context, feedURL domain.URL, page int) ([]Item, error) {
	u, err := url.Parse(string(feedURL))
	if err != nil {
		return nil, fmt.Errorf("cannot parse feed url %s: %w", feedURL, err)
	}
	query := u.Query()
	query.Set("paged", strconv.Itoa(page))
	u.RawQuery = query.Encode()
	return c.Fetch(ctx, domain.URL(u.String()))
}

// ReadSitemaps lists the article urls of a blog changed after after, newest first.
// It reads the sitemaps that robots.txt names, else the common sitemap paths, and follows sitemap indexes.
// A sitemap that a root lists and that cannot be read stops it: it returns what it has with the error.
func (c *Client) ReadSitemaps(ctx context.Context, siteURL domain.URL, after time.Time) ([]SitemapEntry, error) {
	site, err := url.Parse(string(siteURL))
	if err != nil {
		return nil, fmt.Errorf("cannot parse site url %s: %w", siteURL, err)
	}

	roots := c.sitemapRoots(ctx, siteURL)
	queue := slices.Clone(roots)
	seen := map[string]bool{}
	found := map[domain.URL]time.Time{}
	var readErr error
	for read := 0; len(queue) > 0 && read < maxSitemaps; read++ {
		loc := queue[0]
		queue = queue[1:]
		if seen[loc] {
			continue
		}
		seen[loc] = true

		doc, err := c.readSitemap(ctx, loc)
		if err != nil && slices.Contains(roots, loc) {
			log.Ctx(ctx).Debug().Err(err).Msgf("sitemap %s of %s is not usable", loc, siteURL)
			continue
		}
		if err != nil {
			readErr = err
			break
		}
		for _, child := range doc.Sitemaps {
			lastMod := parseLastMod(child.LastMod)
			if !lastMod.IsZero() && !lastMod.After(after) {
				continue
			}
			queue = append(queue, resolveReference(domain.URL(loc), child.Loc))
		}
		for _, entry := range doc.URLs {
			lastMod := parseLastMod(entry.LastMod)
			if !lastMod.After(after) {
				continue
			}
			pageURL, err := domain.ParseURL(entry.Loc)
			if err != nil || !isArticleOf(site, pageURL) {
				continue
			}
			found[pageURL] = lastMod
		}
	}

	entries := make([]SitemapEntry, 0, len(found))
	for pageURL, lastMod := range found {
		entries = append(entries, SitemapEntry{URL: pageURL, LastMod: lastMod})
	}
	slices.SortFunc(entries, func(a, b SitemapEntry) int {
		return cmp.Or(b.LastMod.Compare(a.LastMod), strings.Compare(string(a.URL), string(b.URL)))
	})
	return entries, readErr
}

func (c *Client) sitemapRoots(ctx context.Context, siteURL domain.URL) []string {
	base := strings.TrimSuffix(string(origin(siteURL)), "/")
	robotsURL := domain.URL(fmt.Sprintf("%s/robots.txt", base))
	body, err := c.get(ctx, robotsURL)
	if err != nil {
		log.Ctx(ctx).Debug().Err(err).Msgf("no robots.txt for %s", siteURL)
	}

	var roots []string
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), "sitemap") {
			roots = append(roots, resolveReference(robotsURL, strings.TrimSpace(value)))
		}
	}
	if len(roots) > 0 {
		return roots
	}
	for _, path := range defaultSitemapPaths {
		roots = append(roots, fmt.Sprintf("%s%s", base, path))
	}
	return roots
}

func (c *Client) readSitemap(ctx context.Context, loc string) (sitemapXML, error) {
	body, err := c.get(ctx, domain.URL(loc))
	if err != nil {
		return sitemapXML{}, err
	}
	var doc sitemapXML
	if err := xml.Unmarshal(body, &doc); err != nil {
		return sitemapXML{}, fmt.Errorf("cannot parse sitemap %s: %w", loc, err)
	}
	return doc, nil
}

func parseLastMod(raw string) time.Time {
	for _, layout := range lastModLayouts {
		if t, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// isArticleOf keeps pages under the site path; the site home, tag, category, author and pagination pages are not articles.
func isArticleOf(site *url.URL, page domain.URL) bool {
	u, err := url.Parse(string(page))
	if err != nil {
		return false
	}
	if strings.TrimPrefix(u.Hostname(), "www.") != strings.TrimPrefix(strings.ToLower(site.Hostname()), "www.") {
		return false
	}
	sitePath := strings.TrimSuffix(site.Path, "/")
	path := strings.TrimSuffix(u.Path, "/")
	if !strings.HasPrefix(path, fmt.Sprintf("%s/", sitePath)) {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if slices.Contains(listingSegments, strings.ToLower(segment)) {
			return false
		}
	}
	return true
}

// ReadArticle reads the title, description and date of an article page: the og tags first, then <title> and the meta description.
func (c *Client) ReadArticle(ctx context.Context, pageURL domain.URL) (ArticleInfo, error) {
	body, err := c.get(ctx, pageURL)
	if err != nil {
		return ArticleInfo{}, err
	}

	meta := map[string]string{}
	var title strings.Builder
	inTitle := false
	tokens := html.NewTokenizer(bytes.NewReader(body))
	for done := false; !done; {
		switch tokens.Next() {
		case html.ErrorToken:
			done = true
		case html.TextToken:
			if inTitle {
				title.Write(tokens.Text())
			}
		case html.EndTagToken:
			name, _ := tokens.TagName()
			inTitle = inTitle && string(name) != "title"
			done = string(name) == "head"
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := tokens.TagName()
			switch string(name) {
			case "title":
				inTitle = true
			case "body":
				done = true
			case "meta":
				attrs := map[string]string{}
				for hasAttr {
					var key, value []byte
					key, value, hasAttr = tokens.TagAttr()
					attrs[string(key)] = string(value)
				}
				meta[strings.ToLower(cmp.Or(attrs["property"], attrs["name"]))] = attrs["content"]
			}
		}
	}

	titleRunes := []rune(excerpt(cmp.Or(meta["og:title"], title.String())))
	if len(titleRunes) > maxTitleRune {
		titleRunes = titleRunes[:maxTitleRune]
	}
	return ArticleInfo{
		Title:       string(titleRunes),
		Description: excerpt(cmp.Or(meta["og:description"], meta["description"])),
		PublishedAt: parseLastMod(meta["article:published_time"]),
	}, nil
}

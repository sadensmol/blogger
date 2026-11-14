package feed

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/html"

	"github.com/sadensmol/blogger/internal/domain"
)

const (
	scanNextPages   = 2
	minSiblingLinks = 3
	maxLinkTextRune = 200
)

var (
	assetExtensions = []string{
		".css", ".js", ".json", ".xml", ".rss", ".atom", ".txt", ".pdf", ".zip", ".png", ".jpg", ".jpeg", ".gif",
		".svg", ".webp", ".ico", ".mp3", ".mp4", ".webm", ".woff", ".woff2",
	}
	feedSections = []string{"feed", "rss", "atom"}
)

// IRenderer renders a page in a browser and returns its html after the page scripts ran.
type IRenderer interface {
	Render(ctx context.Context, pageURL domain.URL) ([]byte, error)
}

// WithRenderer lets ScanPage render a page whose plain html lists no article.
func WithRenderer(r IRenderer) Option {
	return func(o *options) { o.renderer = r }
}

type PageScan struct {
	Title    string            // the blog's name: og:site_name, else the <title> of its page
	Articles []domain.PageLink // the links the rules take as articles, in page order
	Unclear  []domain.PageLink // the links of the site the rules cannot decide on
}

type page struct {
	title             string
	articles, unclear []domain.PageLink
	next              domain.URL // empty: the last page
}

// ScanPage reads a blog page and up to two next pages, and sorts their links.
// With render, a page whose plain html lists no article is read again in the browser, when the client has one.
// Only an unreadable first page is an error.
func (c *Client) ScanPage(ctx context.Context, blogURL domain.URL, render bool) (PageScan, error) {
	var scan PageScan
	seen := map[domain.URL]bool{}
	articles := map[domain.URL]bool{}
	visited := map[domain.URL]bool{}
	next := blogURL
	for n := 0; n <= scanNextPages && next != "" && !visited[next]; n++ {
		visited[next] = true
		p, err := c.readPage(ctx, blogURL, next, render)
		if err != nil && n == 0 {
			return PageScan{}, err
		}
		if err != nil {
			log.Ctx(ctx).Debug().Err(err).Msgf("blog %s: page %s skipped", blogURL, next)
			break
		}
		if n == 0 {
			scan.Title = p.title
		}
		for _, link := range p.articles {
			articles[link.URL] = true
			if !seen[link.URL] {
				seen[link.URL] = true
				scan.Articles = append(scan.Articles, link)
			}
		}
		for _, link := range p.unclear {
			if !seen[link.URL] {
				seen[link.URL] = true
				scan.Unclear = append(scan.Unclear, link)
			}
		}
		next = p.next
	}
	scan.Unclear = slices.DeleteFunc(scan.Unclear, func(link domain.PageLink) bool { return articles[link.URL] })
	return scan, nil
}

func (c *Client) readPage(ctx context.Context, blogURL, target domain.URL, render bool) (page, error) {
	body, err := c.get(ctx, target, maxPageBytes)
	if err != nil {
		return page{}, err
	}
	p := parsePage(blogURL, target, body)
	if len(p.articles) > 0 || !render {
		return p, nil
	}
	if c.renderer == nil {
		log.Ctx(ctx).Debug().Msgf("page %s lists no article and no browser is here to render it", target)
		return p, nil
	}
	rendered, err := c.renderer.Render(ctx, target)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("page %s lists no article and cannot be rendered", target)
		return p, nil
	}
	return parsePage(blogURL, target, rendered), nil
}

func parsePage(blogURL, target domain.URL, body []byte) page {
	var p page
	var links []domain.PageLink
	var title, linkText strings.Builder
	var siteName, relNext string
	inTitle, inLink := false, false
	var linkHref string
	tokens := html.NewTokenizer(bytes.NewReader(body))
	for done := false; !done; {
		switch tokens.Next() {
		case html.ErrorToken:
			done = true
		case html.TextToken:
			if inTitle {
				title.Write(tokens.Text())
			}
			if inLink {
				linkText.Write(tokens.Text())
			}
		case html.EndTagToken:
			name, _ := tokens.TagName()
			switch string(name) {
			case "title":
				inTitle = false
			case "a":
				if inLink {
					links = append(links, pageLink(target, linkHref, linkText.String()))
				}
				inLink = false
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := tokens.TagName()
			attrs := map[string]string{}
			for hasAttr {
				var key, value []byte
				key, value, hasAttr = tokens.TagAttr()
				attrs[string(key)] = string(value)
			}
			switch string(name) {
			case "title":
				inTitle = title.Len() == 0
			case "meta":
				if strings.EqualFold(attrs["property"], "og:site_name") {
					siteName = attrs["content"]
				}
			case "link":
				if hasRel(attrs["rel"], "next") {
					relNext = attrs["href"]
				}
			case "a":
				if hasRel(attrs["rel"], "next") {
					relNext = cmp.Or(relNext, attrs["href"])
				}
				if href, ok := attrs["href"]; ok {
					inLink, linkHref = true, href
					linkText.Reset()
				}
			}
		}
	}

	p.title = excerpt(cmp.Or(siteName, title.String()))
	p.articles, p.unclear = sortLinks(blogURL, links)
	p.next = nextPage(target, relNext, links)
	return p
}

func pageLink(base domain.URL, href, text string) domain.PageLink {
	u, err := domain.ParseURL(resolveReference(base, href))
	if err != nil {
		return domain.PageLink{}
	}
	runes := []rune(excerpt(text))
	return domain.PageLink{URL: u, Text: string(runes[:min(len(runes), maxLinkTextRune)])}
}

func hasRel(rel, value string) bool {
	return slices.ContainsFunc(strings.Fields(rel), func(r string) bool { return strings.EqualFold(r, value) })
}

// nextPage takes the rel=next link, else a link to the page numbered one more than target.
func nextPage(target domain.URL, relNext string, links []domain.PageLink) domain.URL {
	if relNext != "" {
		if u, err := domain.ParseURL(resolveReference(target, relNext)); err == nil && sameSite(target, u) {
			return u
		}
	}
	current, ok := pageNumber(target)
	if !ok {
		current = 1
	}
	for _, link := range links {
		if n, ok := pageNumber(link.URL); ok && n == current+1 && sameSite(target, link.URL) {
			return link.URL
		}
	}
	return ""
}

// pageNumber reads ?page=n, ?paged=n and /page/n.
func pageNumber(target domain.URL) (int, bool) {
	u, err := url.Parse(string(target))
	if err != nil {
		return 0, false
	}
	for _, key := range []string{"page", "paged"} {
		if n, err := strconv.Atoi(u.Query().Get(key)); err == nil {
			return n, true
		}
	}
	segments := strings.Split(u.Path, "/")
	for i := 0; i+1 < len(segments); i++ {
		if n, err := strconv.Atoi(segments[i+1]); segments[i] == "page" && err == nil {
			return n, true
		}
	}
	return 0, false
}

func sameSite(a, b domain.URL) bool {
	ua, errA := url.Parse(string(a))
	ub, errB := url.Parse(string(b))
	return errA == nil && errB == nil &&
		strings.TrimPrefix(ua.Hostname(), "www.") == strings.TrimPrefix(ub.Hostname(), "www.")
}

// sortLinks takes as articles the links under the blog's folder. When there are none, it takes the links of a
// folder above the blog's that the page links at least three times: a topic page can list posts of the whole blog.
// The other links of the site are unclear. Links to the blog's own page or the folders above it, listings,
// pagination, feeds and files are dropped.
func sortLinks(blogURL domain.URL, links []domain.PageLink) ([]domain.PageLink, []domain.PageLink) {
	blog, err := url.Parse(string(blogURL))
	if err != nil {
		return nil, nil
	}
	blogPath := strings.TrimSuffix(blog.Path, "/")
	folder := blogPath
	if strings.Contains(path.Base(folder), ".") {
		folder = strings.TrimSuffix(path.Dir(folder), "/")
	}
	site := &url.URL{Host: blog.Host, Path: folder}

	var articles, unclear []domain.PageLink
	seen := map[domain.URL]bool{}
	for _, link := range links {
		if link.URL == "" || seen[link.URL] || !sameSite(blogURL, link.URL) {
			continue
		}
		seen[link.URL] = true
		u, err := url.Parse(string(link.URL))
		if err != nil || dropped(u, blogPath, folder) {
			continue
		}
		if folder != "" && isArticleOf(site, link.URL) {
			articles = append(articles, link)
			continue
		}
		unclear = append(unclear, link)
	}
	if len(articles) > 0 || folder == "" {
		return articles, unclear
	}

	byParent := map[string]int{}
	for _, link := range unclear {
		if parent := linkParent(link.URL); parent != "" && strings.HasPrefix(folder, fmt.Sprintf("%s/", parent)) {
			byParent[parent]++
		}
	}
	var rest []domain.PageLink
	for _, link := range unclear {
		if byParent[linkParent(link.URL)] >= minSiblingLinks {
			articles = append(articles, link)
			continue
		}
		rest = append(rest, link)
	}
	return articles, rest
}

func linkParent(link domain.URL) string {
	u, err := url.Parse(string(link))
	if err != nil {
		return ""
	}
	return path.Dir(strings.TrimSuffix(u.Path, "/"))
}

// dropped reports a link that is never an article: the blog's page or a folder above it, a listing, a page of a listing,
// a feed or a file. Only the segments after the ones the link shares with the blog's folder can make it a listing.
func dropped(u *url.URL, blogPath, folder string) bool {
	p := strings.TrimSuffix(u.Path, "/")
	if p == "" || p == blogPath || p == folder || strings.HasPrefix(folder, fmt.Sprintf("%s/", p)) {
		return true
	}
	if _, ok := pageNumber(domain.URL(u.String())); ok {
		return true
	}
	if slices.Contains(assetExtensions, strings.ToLower(path.Ext(p))) {
		return true
	}
	segments := strings.Split(strings.ToLower(p), "/")
	folderSegments := strings.Split(strings.ToLower(folder), "/")
	shared := 0
	for shared < min(len(segments), len(folderSegments)) && segments[shared] == folderSegments[shared] {
		shared++
	}
	for _, segment := range segments[shared:] {
		if slices.Contains(listingSections, segment) || slices.Contains(feedSections, segment) {
			return true
		}
	}
	return false
}

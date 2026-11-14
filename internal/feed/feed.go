package feed

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/html"

	"github.com/sadensmol/blogger/internal/domain"
)

const (
	userAgent      = "blogger"
	maxRedirects   = 5
	maxBodyBytes   = 5 << 20
	maxExcerptRune = 500
)

var (
	feedMediaTypes = []string{"application/rss+xml", "application/atom+xml"}
	inlineTags     = []string{"a", "b", "i", "em", "strong", "span", "code", "u", "small", "sub", "sup"}
	commonPaths    = []string{"/feed", "/feed.xml", "/rss.xml", "/atom.xml", "/index.xml"}
)

type Client struct {
	http   *http.Client
	parser *gofeed.Parser
	gates  *hostGates // nil: no pause between requests
}

type Info struct {
	FeedURL, SiteURL domain.URL
	Title            string
	Items            int
}

type Item struct {
	URL         domain.URL
	Title       string
	Excerpt     string
	PublishedAt time.Time
}

type Option func(*options)

type options struct {
	allowLoopback bool
	hostDelay     time.Duration
}

func WithLoopbackAllowed() Option {
	return func(o *options) { o.allowLoopback = true }
}

// WithHostDelay sends one request at a time to a host and waits delay between them.
func WithHostDelay(delay time.Duration) Option {
	return func(o *options) { o.hostDelay = delay }
}

func NewClient(timeout time.Duration, opts ...Option) *Client {
	var cfg options
	for _, opt := range opts {
		opt(&cfg)
	}

	dialer := &net.Dialer{
		Timeout: timeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			return checkDialAddress(address, cfg.allowLoopback)
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext

	var gates *hostGates
	if cfg.hostDelay > 0 {
		gates = &hostGates{delay: cfg.hostDelay, hosts: map[string]*hostGate{}}
	}
	return &Client{
		gates: gates,
		http: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("cannot follow more than %d redirects", maxRedirects)
				}

				return checkScheme(req.URL)
			},
		},
		parser: gofeed.NewParser(),
	}
}

func checkScheme(target *url.URL) error {
	if target.Scheme != "http" && target.Scheme != "https" {
		return fmt.Errorf("cannot use scheme %q: only http and https are allowed", target.Scheme)
	}

	return nil
}

func checkDialAddress(address string, allowLoopback bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("cannot parse dial address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("cannot parse dial ip %q", host)
	}

	if ip.IsLoopback() && allowLoopback {
		return nil
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("cannot connect to %s: address is not public", ip)
	}

	return nil
}

func (c *Client) Fetch(ctx context.Context, feedURL domain.URL) ([]Item, error) {
	parsed, err := c.fetchFeed(ctx, feedURL)
	if err != nil {
		return nil, err
	}

	return c.items(ctx, feedURL, parsed), nil
}

func (c *Client) Resolve(ctx context.Context, pageURL domain.URL) (Info, error) {
	body, err := c.get(ctx, pageURL)
	if err != nil {
		return Info{}, err
	}

	if parsed, err := c.parser.Parse(bytes.NewReader(body)); err == nil {
		return c.info(ctx, pageURL, parsed), nil
	}

	for _, candidate := range append(alternateLinks(pageURL, body), commonPathURLs(pageURL)...) {
		parsed, err := c.fetchFeed(ctx, candidate)
		if err != nil {
			log.Ctx(ctx).Debug().Err(err).Msgf("feed candidate %s is not usable", candidate)
			continue
		}

		return c.info(ctx, candidate, parsed), nil
	}

	return Info{}, fmt.Errorf("cannot find a feed for %s: %w", pageURL, domain.ErrNotFound)
}

func (c *Client) fetchFeed(ctx context.Context, feedURL domain.URL) (*gofeed.Feed, error) {
	body, err := c.get(ctx, feedURL)
	if err != nil {
		return nil, err
	}

	parsed, err := c.parser.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cannot parse feed %s: %w", feedURL, err)
	}

	return parsed, nil
}

func (c *Client) get(ctx context.Context, target domain.URL) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, string(target), nil)
	if err != nil {
		return nil, fmt.Errorf("cannot build request for %s: %w", target, err)
	}
	if err := checkScheme(req.URL); err != nil {
		return nil, fmt.Errorf("cannot get %s: %w", target, err)
	}
	req.Header.Set("User-Agent", userAgent)

	if c.gates != nil {
		leave, err := c.gates.enter(ctx, req.URL.Host)
		if err != nil {
			return nil, fmt.Errorf("cannot get %s: %w", target, err)
		}
		defer leave()
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot get %s: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot get %s: status %d", target, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", target, err)
	}

	return body, nil
}

func (c *Client) info(ctx context.Context, feedURL domain.URL, parsed *gofeed.Feed) Info {
	siteURL, err := domain.ParseURL(parsed.Link)
	if err != nil {
		log.Ctx(ctx).Debug().Err(err).Msgf("feed %s has no usable site link, using its origin", feedURL)
		siteURL = origin(feedURL)
	}

	return Info{
		FeedURL: feedURL,
		SiteURL: siteURL,
		Title:   parsed.Title,
		Items:   len(parsed.Items),
	}
}

func (c *Client) items(ctx context.Context, feedURL domain.URL, parsed *gofeed.Feed) []Item {
	items := make([]Item, 0, len(parsed.Items))
	for _, entry := range parsed.Items {
		itemURL, err := domain.ParseURL(resolveReference(feedURL, entry.Link))
		if err != nil {
			log.Ctx(ctx).Debug().Err(err).Msgf("skipping item %q of feed %s", entry.Title, feedURL)
			continue
		}

		description := entry.Description
		if description == "" {
			description = entry.Content
		}

		item := Item{
			URL:     itemURL,
			Title:   strings.TrimSpace(entry.Title),
			Excerpt: excerpt(description),
		}
		switch {
		case entry.PublishedParsed != nil:
			item.PublishedAt = entry.PublishedParsed.UTC()
		case entry.UpdatedParsed != nil:
			item.PublishedAt = entry.UpdatedParsed.UTC()
		}
		items = append(items, item)
	}

	return items
}

func excerpt(markup string) string {
	var text strings.Builder
	tokens := html.NewTokenizer(strings.NewReader(markup))
	for {
		tokenType := tokens.Next()
		if tokenType == html.ErrorToken {
			break
		}
		switch tokenType {
		case html.TextToken:
			text.Write(tokens.Text())
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := tokens.TagName()
			if !slices.Contains(inlineTags, string(name)) {
				text.WriteByte(' ')
			}
		}
	}

	runes := []rune(strings.Join(strings.Fields(text.String()), " "))
	if len(runes) > maxExcerptRune {
		runes = runes[:maxExcerptRune]
	}

	return string(runes)
}

func alternateLinks(pageURL domain.URL, body []byte) []domain.URL {
	var links []domain.URL
	tokens := html.NewTokenizer(bytes.NewReader(body))
	for {
		tokenType := tokens.Next()
		if tokenType == html.ErrorToken {
			return links
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}

		name, hasAttr := tokens.TagName()
		if string(name) != "link" || !hasAttr {
			continue
		}

		attrs := map[string]string{}
		for hasAttr {
			var key, value []byte
			key, value, hasAttr = tokens.TagAttr()
			attrs[string(key)] = string(value)
		}

		if !strings.EqualFold(attrs["rel"], "alternate") || !isFeedMediaType(attrs["type"]) {
			continue
		}
		link, err := domain.ParseURL(resolveReference(pageURL, attrs["href"]))
		if err != nil {
			continue
		}
		links = append(links, link)
	}
}

func isFeedMediaType(mediaType string) bool {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	for _, feedType := range feedMediaTypes {
		if mediaType == feedType {
			return true
		}
	}

	return false
}

func commonPathURLs(pageURL domain.URL) []domain.URL {
	base := origin(pageURL)
	urls := make([]domain.URL, 0, len(commonPaths))
	for _, path := range commonPaths {
		urls = append(urls, domain.URL(fmt.Sprintf("%s%s", strings.TrimSuffix(string(base), "/"), path)))
	}

	return urls
}

func origin(target domain.URL) domain.URL {
	parsed, err := url.Parse(string(target))
	if err != nil {
		return target
	}

	return domain.URL(fmt.Sprintf("%s://%s/", parsed.Scheme, parsed.Host))
}

func resolveReference(base domain.URL, ref string) string {
	baseURL, err := url.Parse(string(base))
	if err != nil {
		return ref
	}
	refURL, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return ref
	}

	return baseURL.ResolveReference(refURL).String()
}


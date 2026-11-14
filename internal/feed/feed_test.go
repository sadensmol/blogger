package feed_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
)

const timeout = 5 * time.Second

func serveFixtures(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	for path, fixture := range routes {
		body, err := os.ReadFile(filepath.Join("testdata", fixture))
		require.NoError(t, err)
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func mustURL(t *testing.T, raw string) domain.URL {
	t.Helper()

	u, err := domain.ParseURL(raw)
	require.NoError(t, err)

	return u
}

func TestFetchRSSReturnsPlainExcerpt(t *testing.T) {
	srv := serveFixtures(t, map[string]string{"/feed.xml": "rss.xml"})

	items, err := feed.NewClient(timeout).Fetch(context.Background(), mustURL(t, srv.URL+"/feed.xml"))

	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "First post", items[0].Title)
	require.Equal(t, domain.URL("https://example.com/posts/first"), items[0].URL)
	require.Equal(t, "Hello world.", items[0].Excerpt)
	require.Equal(t, time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), items[0].PublishedAt)
}

func TestFetchAtom(t *testing.T) {
	srv := serveFixtures(t, map[string]string{"/atom.xml": "atom.xml"})

	items, err := feed.NewClient(timeout).Fetch(context.Background(), mustURL(t, srv.URL+"/atom.xml"))

	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, domain.URL("https://example.org/entry-1"), items[0].URL)
	require.Equal(t, "Atom entry", items[0].Title)
}

func TestFetchItemWithoutDateHasZeroTime(t *testing.T) {
	srv := serveFixtures(t, map[string]string{"/nodate.xml": "nodate.xml"})

	items, err := feed.NewClient(timeout).Fetch(context.Background(), mustURL(t, srv.URL+"/nodate.xml"))

	require.NoError(t, err)
	require.Len(t, items, 1)
	require.True(t, items[0].PublishedAt.IsZero())
}

func TestFetchCutsExcerptAt500Characters(t *testing.T) {
	body := fmt.Sprintf(`<?xml version="1.0"?><rss version="2.0"><channel><title>t</title><item>
<title>Long</title><link>https://example.com/long</link><description>%s</description></item></channel></rss>`,
		strings.Repeat("ж", 800))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	items, err := feed.NewClient(timeout).Fetch(context.Background(), mustURL(t, srv.URL))

	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, []rune(items[0].Excerpt), 500)
}

func TestFetchTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)

	started := time.Now()
	_, err := feed.NewClient(200*time.Millisecond).Fetch(context.Background(), mustURL(t, srv.URL))

	require.Error(t, err)
	require.Less(t, time.Since(started), time.Second)
}

func TestResolveFeedURLItself(t *testing.T) {
	srv := serveFixtures(t, map[string]string{"/rss.xml": "rss.xml"})
	feedURL := mustURL(t, srv.URL+"/rss.xml")

	info, err := feed.NewClient(timeout).Resolve(context.Background(), feedURL)

	require.NoError(t, err)
	require.Equal(t, feedURL, info.FeedURL)
	require.Equal(t, "Example Blog", info.Title)
	require.Equal(t, domain.URL("https://example.com/"), info.SiteURL)
	require.Equal(t, 2, info.Items)
}

func TestResolveFromAlternateLink(t *testing.T) {
	srv := serveFixtures(t, map[string]string{"/blog": "page.html", "/feed.xml": "rss.xml"})

	info, err := feed.NewClient(timeout).Resolve(context.Background(), mustURL(t, srv.URL+"/blog"))

	require.NoError(t, err)
	require.Equal(t, mustURL(t, srv.URL+"/feed.xml"), info.FeedURL)
}

func TestResolveFromCommonPath(t *testing.T) {
	srv := serveFixtures(t, map[string]string{"/blog": "page-nolink.html", "/rss.xml": "rss.xml"})

	info, err := feed.NewClient(timeout).Resolve(context.Background(), mustURL(t, srv.URL+"/blog"))

	require.NoError(t, err)
	require.Equal(t, mustURL(t, srv.URL+"/rss.xml"), info.FeedURL)
}

func TestResolveWithoutFeedIsNotFound(t *testing.T) {
	srv := serveFixtures(t, map[string]string{"/blog": "page-nolink.html"})

	_, err := feed.NewClient(timeout).Resolve(context.Background(), mustURL(t, srv.URL+"/blog"))

	require.ErrorIs(t, err, domain.ErrNotFound)
}

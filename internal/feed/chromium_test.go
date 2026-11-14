//go:build chromium

package feed_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
)

const (
	agentUser    = "blogger"
	secretMarker = "local-only-secret"
	scriptPage   = `<!doctype html><html><head><title>Script blog</title></head><body><main id="list"></main><script>
for (const slug of ["first", "second", "third"]) {
  const a = document.createElement("a");
  a.href = "/blog/" + slug;
  a.textContent = slug;
  document.getElementById("list").appendChild(a);
}
</script></body></html>`
)

// chromium is the browser as serve runs it: as the agent user when the test runs as root.
func chromium(t *testing.T, allowLoopback bool) *feed.Chromium {
	t.Helper()

	bin := feed.FindChromium()
	require.NotEmpty(t, bin, "no Chromium or Chrome found")
	d := feed.ChromiumData{Bin: bin, Home: os.TempDir(), AllowLoopback: allowLoopback}
	if os.Geteuid() == 0 {
		u, err := user.Lookup(agentUser)
		require.NoError(t, err)
		uid, err := strconv.ParseUint(u.Uid, 10, 32)
		require.NoError(t, err)
		gid, err := strconv.ParseUint(u.Gid, 10, 32)
		require.NoError(t, err)
		d.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
		d.Home = u.HomeDir
	}
	return feed.NewChromium(d)
}

func TestChromiumFindsLinksThatScriptsAdd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(scriptPage))
	}))
	t.Cleanup(srv.Close)
	client := feed.NewClient(timeout, feed.WithLoopbackAllowed(), feed.WithRenderer(chromium(t, true)))

	scan, err := client.ScanPage(context.Background(), mustURL(t, srv.URL+"/blog"), true)

	require.NoError(t, err)
	var urls []domain.URL
	for _, link := range scan.Articles {
		urls = append(urls, link.URL)
	}
	require.Equal(t, []domain.URL{
		mustURL(t, srv.URL+"/blog/first"), mustURL(t, srv.URL+"/blog/second"), mustURL(t, srv.URL+"/blog/third"),
	}, urls)
}

func TestChromiumCannotReachLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(secretMarker))
	}))
	t.Cleanup(srv.Close)

	rendered, err := chromium(t, false).Render(context.Background(), mustURL(t, srv.URL))

	t.Logf("render error: %v", err)
	require.NotContains(t, string(rendered), secretMarker)
	require.NotEmpty(t, rendered)
}

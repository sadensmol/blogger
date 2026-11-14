package feed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
)

const (
	renderTimeout = 45 * time.Second
	renderSettle  = 2 * time.Second
)

var chromiumPaths = []string{
	"chromium", "chromium-browser", "google-chrome", "google-chrome-stable",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
}

// FindChromium returns the path of a Chromium or Chrome program; it is empty when there is none.
func FindChromium() string {
	for _, name := range chromiumPaths {
		if found, err := exec.LookPath(name); err == nil {
			return found
		}
	}
	return ""
}

type ChromiumData struct {
	Bin           string
	Credential    *syscall.Credential // nil: the bot's own user
	Home          string
	AllowLoopback bool
}

// Chromium renders one page at a time in headless Chromium, as the agent user when the bot runs as root.
// Every request of the browser goes through a proxy that dials only public addresses.
type Chromium struct {
	d    ChromiumData
	turn chan struct{}
}

func NewChromium(d ChromiumData) *Chromium {
	return &Chromium{d: d, turn: make(chan struct{}, 1)}
}

func (c *Chromium) Render(ctx context.Context, pageURL domain.URL) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("cannot render %s: %w", pageURL, ctx.Err())
	case c.turn <- struct{}{}:
	}
	defer func() { <-c.turn }()
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	proxyURL, err := startStrictProxy(ctx, c.d.AllowLoopback)
	if err != nil {
		return nil, fmt.Errorf("cannot render %s: %w", pageURL, err)
	}
	dataDir, err := os.MkdirTemp("", "blogger-chromium-")
	if err != nil {
		return nil, fmt.Errorf("cannot create a browser profile folder: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(dataDir); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("cannot remove browser profile folder %s", dataDir)
		}
	}()
	if c.d.Credential != nil {
		if err := os.Chown(dataDir, int(c.d.Credential.Uid), int(c.d.Credential.Gid)); err != nil {
			return nil, fmt.Errorf("cannot give the browser profile folder to the agent user: %w", err)
		}
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(c.d.Bin),
		chromedp.NoInheritEnv,
		chromedp.Env(fmt.Sprintf("HOME=%s", c.d.Home)),
		chromedp.UserDataDir(dataDir),
		chromedp.ProxyServer(proxyURL),
		// without it chromium sends requests to loopback around the proxy
		chromedp.Flag("proxy-bypass-list", "<-loopback>"),
		chromedp.Flag("force-webrtc-ip-handling-policy", "disable_non_proxied_udp"),
		chromedp.Flag("blink-settings", "imagesEnabled=false"),
		chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) { cmd.SysProcAttr = browserProcAttr(c.d.Credential) }),
	)
	if c.d.Credential != nil {
		// a container has no user namespaces for the sandbox; the agent user cannot read the bot's secrets anyway
		opts = append(opts, chromedp.NoSandbox)
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	if err := chromedp.Do(browserCtx, chromedp.Navigate(string(pageURL)), chromedp.Sleep(renderSettle)); err != nil {
		return nil, fmt.Errorf("cannot render %s: %w", pageURL, err)
	}
	rendered, err := chromedp.Run(browserCtx, chromedp.OuterHTML(chromedp.CSS("html")))
	if err != nil {
		return nil, fmt.Errorf("cannot read rendered %s: %w", pageURL, err)
	}
	return []byte(rendered[:min(len(rendered), maxPageBytes)]), nil
}

// startStrictProxy serves an http proxy on loopback until ctx ends. It dials like the strict client.
func startStrictProxy(ctx context.Context, allowLoopback bool) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("cannot start the browser proxy: %w", err)
	}
	proxy := &strictProxy{
		ctx:       ctx,
		dialer:    strictDialer(renderTimeout, allowLoopback),
		transport: strictTransport(renderTimeout, allowLoopback),
	}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: renderTimeout}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Ctx(ctx).Warn().Err(err).Msg("browser proxy stopped")
		}
	}()
	context.AfterFunc(ctx, func() {
		if err := server.Close(); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("cannot close the browser proxy")
		}
		proxy.transport.CloseIdleConnections()
	})
	return fmt.Sprintf("http://%s", listener.Addr()), nil
}

type strictProxy struct {
	ctx       context.Context // the render: its end closes the tunnels
	dialer    *net.Dialer
	transport *http.Transport
}

func (p *strictProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.tunnel(w, r)
		return
	}
	if r.URL.Scheme != "http" {
		http.Error(w, "only http and https", http.StatusBadRequest)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header.Del("Proxy-Connection")
	out.Header.Del("Proxy-Authorization")
	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		log.Ctx(p.ctx).Debug().Err(err).Msgf("browser proxy refused %s", r.URL.Host)
		http.Error(w, "refused", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Ctx(p.ctx).Debug().Err(err).Msgf("browser proxy cut a response from %s", r.URL.Host)
	}
}

func (p *strictProxy) tunnel(w http.ResponseWriter, r *http.Request) {
	upstream, err := p.dialer.DialContext(p.ctx, "tcp", r.Host)
	if err != nil {
		log.Ctx(p.ctx).Debug().Err(err).Msgf("browser proxy refused %s", r.Host)
		http.Error(w, "refused", http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		closeConn(p.ctx, upstream)
		http.Error(w, "no tunnel", http.StatusInternalServerError)
		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		closeConn(p.ctx, upstream)
		log.Ctx(p.ctx).Debug().Err(err).Msgf("browser proxy cannot open a tunnel to %s", r.Host)
		return
	}
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		closeConn(p.ctx, upstream)
		closeConn(p.ctx, client)
		return
	}
	stop := context.AfterFunc(p.ctx, func() {
		closeConn(p.ctx, upstream)
		closeConn(p.ctx, client)
	})
	go func() {
		defer stop()
		go func() {
			if _, err := io.Copy(upstream, client); err != nil {
				log.Ctx(p.ctx).Debug().Err(err).Msgf("browser tunnel to %s ended", r.Host)
			}
			closeConn(p.ctx, upstream)
		}()
		if _, err := io.Copy(client, upstream); err != nil {
			log.Ctx(p.ctx).Debug().Err(err).Msgf("browser tunnel from %s ended", r.Host)
		}
		closeConn(p.ctx, client)
	}()
}

func closeConn(ctx context.Context, conn net.Conn) {
	if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		log.Ctx(ctx).Debug().Err(err).Msg("cannot close a browser proxy connection")
	}
}

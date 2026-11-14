package domain

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

type URL string

func ParseURL(raw string) (URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("cannot parse url %q: %w", raw, err)
	}

	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("cannot use url %q: scheme must be http or https", raw)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("cannot use url %q: host is empty", raw)
	}

	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "80" && u.Scheme == "http" || port == "443" && u.Scheme == "https" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = fmt.Sprintf("[%s]", host)
	}
	u.Host = host

	u.Fragment = ""
	u.RawFragment = ""

	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	if u.Path == "" {
		u.Path = "/"
		u.RawPath = ""
	}

	query := u.Query()
	for key := range query {
		if strings.HasPrefix(key, "utm_") || key == "fbclid" || key == "gclid" || key == "ref" {
			query.Del(key)
		}
	}
	u.RawQuery = query.Encode()

	return URL(u.String()), nil
}

// SameSiteAs reports whether u and other are on one host, with or without www.
func (u URL) SameSiteAs(other URL) bool {
	a, errA := url.Parse(string(u))
	b, errB := url.Parse(string(other))
	return errA == nil && errB == nil &&
		strings.TrimPrefix(strings.ToLower(a.Hostname()), "www.") == strings.TrimPrefix(strings.ToLower(b.Hostname()), "www.")
}

// WithOriginOf gives u the scheme and host of other when both are on one site: the same article under http or www
// is one url. A url of another host stays as it is.
func (u URL) WithOriginOf(other URL) URL {
	if !u.SameSiteAs(other) {
		return u
	}
	target, err := url.Parse(string(u))
	if err != nil {
		return u
	}
	source, err := url.Parse(string(other))
	if err != nil {
		return u
	}
	target.Scheme = source.Scheme
	target.Host = source.Host
	return URL(target.String())
}

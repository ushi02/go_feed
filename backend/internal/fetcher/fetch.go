package fetcher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
)

const maxBytes = 2 << 20

var blocked = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("2001:db8::/32"),
}

type Candidate struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type Result struct {
	Feed        *gofeed.Feed
	FinalURL    string
	ETag        string
	Modified    string
	NotModified bool
}

func Canonical(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil {
		return "", errors.New("invalid feed URL")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("only HTTP and HTTPS feeds are supported")
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		if _, err := strconv.Atoi(port); err != nil {
			return "", errors.New("invalid port")
		}
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	u.Fragment = ""
	u.RawFragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

// IPAllowed decides which resolved addresses the client may dial.
// Tests replace it so they can reach httptest servers on 127.0.0.1.
var IPAllowed = allowedIP

func allowedIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range blocked {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

func client() *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		MaxIdleConns:          16,
		IdleConnTimeout:       20 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !IPAllowed(ip) {
					return nil, errors.New("feed resolves to a blocked address")
				}
			}
			if len(ips) == 0 {
				return nil, errors.New("feed has no address")
			}
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}
	return &http.Client{Timeout: 12 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		_, err := Canonical(req.URL.String())
		return err
	}}
}

func get(ctx context.Context, raw, etag, modified string) ([]byte, *http.Response, error) {
	canonical, err := Canonical(raw)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, canonical, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "GoFeedReader/0.1 (+RSS reader)")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if modified != "" {
		req.Header.Set("If-Modified-Since", modified)
	}
	resp, err := client().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, resp, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, &StatusError{Code: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > maxBytes {
		return nil, nil, errors.New("feed response exceeds 2 MiB")
	}
	return data, resp, nil
}

func Fetch(ctx context.Context, raw, etag, modified string) (Result, error) {
	data, resp, err := get(ctx, raw, etag, modified)
	if err != nil {
		return Result{}, err
	}
	final, err := Canonical(resp.Request.URL.String())
	if err != nil {
		return Result{}, err
	}
	result := Result{FinalURL: final, ETag: resp.Header.Get("ETag"), Modified: resp.Header.Get("Last-Modified"), NotModified: resp.StatusCode == http.StatusNotModified}
	if result.NotModified {
		return result, nil
	}
	result.Feed, err = gofeed.NewParser().Parse(bytes.NewReader(data))
	return result, err
}

func Discover(ctx context.Context, raw string) ([]Candidate, error) {
	data, resp, err := get(ctx, raw, "", "")
	if err != nil {
		return nil, err
	}
	final, err := Canonical(resp.Request.URL.String())
	if err != nil {
		return nil, err
	}
	if feed, err := gofeed.NewParser().Parse(bytes.NewReader(data)); err == nil {
		return []Candidate{{URL: final, Title: feed.Title}}, nil
	}
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(final)
	seen := map[string]bool{}
	out := []Candidate{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[strings.ToLower(a.Key)] = a.Val
			}
			typ := strings.ToLower(attrs["type"])
			if strings.Contains(" "+strings.ToLower(attrs["rel"])+" ", " alternate ") &&
				(strings.Contains(typ, "rss+xml") || strings.Contains(typ, "atom+xml") || strings.Contains(typ, "feed+json")) {
				u, err := base.Parse(attrs["href"])
				if err == nil {
					if c, err := Canonical(u.String()); err == nil && !seen[c] {
						seen[c] = true
						out = append(out, Candidate{URL: c, Title: attrs["title"]})
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if len(out) == 0 {
		return nil, errors.New("no RSS, Atom, or JSON Feed found")
	}
	return out, nil
}

// StatusError is returned when the server answers with a status other than 200 or 304.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("feed returned HTTP %d", e.Code) }

func Hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func ResolvePublicURL(raw, baseRaw string) string {
	if raw == "" {
		return ""
	}
	base, err := url.Parse(baseRaw)
	if err != nil {
		return ""
	}
	u, err := base.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	return u.String()
}

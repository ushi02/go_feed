package fetcher

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestCanonical(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"HTTPS://Example.COM:443/feed/?order=old#section", "https://example.com/feed/?order=old"},
		{"https://example.com/feed?order=old", "https://example.com/feed?order=old"}, // trailing slash is meaningful
		{"http://example.com:80/rss", "http://example.com/rss"},
		{"http://example.com", "http://example.com/"},
		{"http://example.com:8080/rss", "http://example.com:8080/rss"},
		{"  https://example.com/a  ", "https://example.com/a"},
		{"http://[::1]/feed", "http://[::1]/feed"},
		{"http://[::1]:8080/feed", "http://[::1]:8080/feed"},
	}
	for _, tt := range tests {
		got, err := Canonical(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("Canonical(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestCanonicalRejects(t *testing.T) {
	for _, in := range []string{
		"", "not a url", "ftp://example.com/feed", "javascript:alert(1)", "file:///etc/passwd",
		"https://user:pass@example.com/feed", "https:///no-host",
	} {
		if got, err := Canonical(in); err == nil {
			t.Errorf("Canonical(%q) = %q, want error", in, got)
		}
	}
}

func TestAllowedIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"2606:4700:4700::1111", true},
		{"::ffff:8.8.8.8", true},
		{"127.0.0.1", false},       // loopback
		{"10.0.0.4", false},        // private
		{"172.16.0.1", false},      // private
		{"192.168.1.1", false},     // private
		{"169.254.169.254", false}, // link-local, cloud metadata
		{"100.64.0.1", false},      // carrier-grade NAT
		{"192.0.0.1", false},       // IETF protocol assignments
		{"198.18.0.1", false},      // benchmarking
		{"0.0.0.0", false},
		{"224.0.0.1", false}, // multicast
		{"255.255.255.255", false},
		{"::1", false},
		{"fc00::1", false},
		{"fe80::1", false},
		{"2001:db8::1", false},      // documentation
		{"::ffff:127.0.0.1", false}, // IPv4-mapped loopback
		{"::ffff:10.0.0.1", false},  // IPv4-mapped private
	}
	for _, tt := range tests {
		if got := allowedIP(netip.MustParseAddr(tt.ip)); got != tt.want {
			t.Errorf("allowedIP(%s) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

// allowLoopback lets tests reach httptest servers while keeping every other rule.
func allowLoopback(t *testing.T) {
	orig := IPAllowed
	IPAllowed = func(ip netip.Addr) bool { return ip.Unmap().IsLoopback() || allowedIP(ip) }
	t.Cleanup(func() { IPAllowed = orig })
}

const tinyRSS = `<?xml version="1.0"?><rss version="2.0"><channel><title>T</title><item><guid>1</guid><title>A</title></item></channel></rss>`

func TestFetchNotModified(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(tinyRSS))
	}))
	defer srv.Close()

	first, err := Fetch(context.Background(), srv.URL+"/feed", "", "")
	if err != nil || first.NotModified || first.Feed == nil || first.ETag != `"v1"` {
		t.Fatalf("first fetch = %+v, %v", first, err)
	}
	second, err := Fetch(context.Background(), srv.URL+"/feed", first.ETag, "")
	if err != nil || !second.NotModified || second.Feed != nil {
		t.Fatalf("conditional fetch = %+v, %v; want NotModified", second, err)
	}
}

func TestFetchRejectsOversizedBody(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), maxBytes+1))
	}))
	defer srv.Close()

	_, err := Fetch(context.Background(), srv.URL+"/feed", "", "")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Fetch oversized body err = %v, want size error", err)
	}
}

func TestFetchBlocksRedirectToInternalAddress(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	_, err := Fetch(context.Background(), srv.URL+"/feed", "", "")
	if err == nil || !strings.Contains(err.Error(), "blocked address") {
		t.Fatalf("Fetch redirect err = %v, want blocked address", err)
	}
}

func TestCleanHTMLResolvesLinksAndRemovesScripts(t *testing.T) {
	got := CleanHTML(`<p><a href="/story">story</a><img src="/cover.png"><script>alert(1)</script><a href="javascript:alert(1)">bad</a></p>`, "https://example.com/post")
	if !strings.Contains(got, "https://example.com/story") || !strings.Contains(got, "https://example.com/cover.png") {
		t.Fatalf("relative URLs not resolved: %s", got)
	}
	if strings.Contains(got, "<script") || strings.Contains(got, "javascript:") {
		t.Fatalf("unsafe HTML survived: %s", got)
	}
	if !strings.Contains(got, "noopener noreferrer") {
		t.Fatalf("external link protection missing: %s", got)
	}
	if !strings.Contains(got, `target="_blank"`) {
		t.Fatalf("links should open in a new tab: %s", got)
	}
}

func TestResolvePublicURLRejectsScriptScheme(t *testing.T) {
	if got := ResolvePublicURL("javascript:alert(1)", "https://example.com/feed"); got != "" {
		t.Fatalf("unsafe entry URL: %s", got)
	}
}

package feedfetch

import (
	"net/netip"
	"strings"
	"testing"
)

func TestCanonicalPreservesMeaningfulPathAndQuery(t *testing.T) {
	got, err := Canonical("HTTPS://Example.COM:443/feed/?order=old#section")
	if err != nil || got != "https://example.com/feed/?order=old" {
		t.Fatalf("Canonical = %q, %v", got, err)
	}
	other, err := Canonical("https://example.com/feed?order=old")
	if err != nil || other == got {
		t.Fatalf("trailing slash was collapsed: %q", other)
	}
}

func TestBlockedNetworkRanges(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.4", "169.254.169.254", "100.64.0.1", "::1", "fc00::1", "2001:db8::1"} {
		if allowedIP(netip.MustParseAddr(raw)) {
			t.Errorf("allowed private address %s", raw)
		}
	}
	if !allowedIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address was rejected")
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
}

func TestResolvePublicURLRejectsScriptScheme(t *testing.T) {
	if got := ResolvePublicURL("javascript:alert(1)", "https://example.com/feed"); got != "" {
		t.Fatalf("unsafe entry URL: %s", got)
	}
}

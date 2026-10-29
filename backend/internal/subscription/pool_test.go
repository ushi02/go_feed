package subscription

import (
	"strings"
	"testing"
)

func TestParsePool(t *testing.T) {
	csv := "Introduction, Address, RSS feed, tags\n" +
		"阮一峰的网络日志, https://www.ruanyifeng.com/blog/, HTTP://Feeds.Feedburner.com/ruanyifeng, 编程\n" +
		"没有 RSS 的博客, https://example.com/, , 生活\n" +
		"坏地址, https://example.org/, ftp://example.org/feed, 生活\n"
	feeds, skipped, err := parsePool(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 || skipped != 2 {
		t.Fatalf("got %d feeds, %d skipped; want 1, 2", len(feeds), skipped)
	}
	f := feeds[0]
	if f.URL != "http://feeds.feedburner.com/ruanyifeng" || f.Title != "阮一峰的网络日志" ||
		f.SiteURL != "https://www.ruanyifeng.com/blog/" || !f.InPool {
		t.Fatalf("unexpected feed: %+v", f)
	}
}

// The embedded snapshot must parse; it has 1569 rows, 1426 of them with a feed URL.
func TestEmbeddedPool(t *testing.T) {
	feeds, skipped, err := parsePool(strings.NewReader(poolCSV))
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) < 1400 {
		t.Fatalf("only %d feeds parsed (%d skipped)", len(feeds), skipped)
	}
}

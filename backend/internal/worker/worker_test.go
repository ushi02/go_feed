package worker

import (
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

func TestFutureDateIsClampedAndLinkResolved(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	future := now.Add(72 * time.Hour)
	item := &gofeed.Item{GUID: "old-guid", Title: "Article", Link: "/article", PublishedParsed: &future}
	entry := entryFromItem(4, "https://example.com/feed", item, now)
	if entry == nil || !entry.SortAt.Equal(now) || entry.URL != "https://example.com/article" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

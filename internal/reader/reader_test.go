package reader

import (
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

func TestCursorRoundTrip(t *testing.T) {
	want := time.Date(2026, 10, 25, 13, 42, 1, 123456000, time.UTC)
	raw := encodeCursor(want, 481)
	got, id, err := decodeCursor(raw)
	if err != nil || id != 481 || !got.Equal(want) {
		t.Fatalf("decoded cursor = %v, %d, %v", got, id, err)
	}
}

func TestFutureDateIsClampedAndLinkResolved(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	future := now.Add(72 * time.Hour)
	item := &gofeed.Item{GUID: "old-guid", Title: "Article", Link: "/article", PublishedParsed: &future}
	entry := entryFromItem(4, "https://example.com/feed", item, now)
	if entry == nil || !entry.SortAt.Equal(now) || entry.URL != "https://example.com/article" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

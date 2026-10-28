package worker

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/ushi02/go_feed/backend/internal/fetcher"
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

// Some feeds carry zero dates (year 1), which MySQL rejects. Treat them as unknown.
func TestImpossibleDateIsTreatedAsUnknown(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	zero := time.Time{}
	item := &gofeed.Item{GUID: "g", Title: "Article", PublishedParsed: &zero}
	entry := entryFromItem(4, "https://example.com/feed", item, now)
	if entry == nil || entry.PublishedAt != nil || !entry.SortAt.Equal(now) {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestIsDead(t *testing.T) {
	gone := fmt.Errorf("fetch: %w", &fetcher.StatusError{Code: 410})
	notFound := &fetcher.StatusError{Code: 404}
	other := errors.New("connection refused")
	tests := []struct {
		err      error
		failures int
		want     bool
	}{
		{gone, 1, true},
		{notFound, 1, false},
		{notFound, maxFailures - 1, false},
		{notFound, maxFailures, true},
		{other, maxFailures, true},
	}
	for _, tt := range tests {
		if got := isDead(tt.err, tt.failures); got != tt.want {
			t.Errorf("isDead(%v, %d) = %v, want %v", tt.err, tt.failures, got, tt.want)
		}
	}
}

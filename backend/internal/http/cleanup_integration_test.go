package http_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ushi02/go_feed/backend/internal/entry"
	"github.com/ushi02/go_feed/backend/internal/fetcher"
	"github.com/ushi02/go_feed/backend/internal/subscription"
	"github.com/ushi02/go_feed/backend/internal/worker"
)

// addEntries inserts n entries; entry i is published i days ago, so entry 0 is the newest.
func addEntries(t *testing.T, f subscription.Feed, n int) []entry.Entry {
	t.Helper()
	now := time.Now().UTC()
	out := make([]entry.Entry, n)
	for i := range n {
		at := now.Add(-time.Duration(i) * 24 * time.Hour)
		out[i] = entry.Entry{FeedID: f.ID, GUIDHash: fetcher.Hash(fmt.Sprintf("%d-%d", f.ID, i)), Title: fmt.Sprintf("Old %02d", i), Content: "<p>text</p>", SortAt: at, PublishedAt: &at, CreatedAt: now}
		if err := database.Create(&out[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// withContent returns the ids of the feed's entries that still have article text.
func withContent(t *testing.T, f subscription.Feed) map[uint64]bool {
	t.Helper()
	var ids []uint64
	database.Table("entries").Where("feed_id = ? AND content IS NOT NULL", f.ID).Pluck("id", &ids)
	out := map[uint64]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// Feeds nobody subscribes to keep article text only for their 30 newest
// entries, plus any entry a user still has (a starred one). Older rows stay,
// without text. Subscribed feeds keep everything.
func TestCleanupKeepsTextOfNewest30AndStarred(t *testing.T) {
	skipShort(t)
	future := time.Now().UTC().Add(24 * time.Hour)
	pool := insertFeed(t, "https://cleanup-pool.example/feed", true, future)
	poolEntries := addEntries(t, pool, 35)

	// alice subscribed once, starred the oldest entry, then unsubscribed.
	alice := newClient(t, "cleanup-alice")
	var aliceID uint64
	database.Table("users").Where("username = ?", "cleanup-alice").Pluck("id", &aliceID)
	starred := poolEntries[34]
	database.Exec("INSERT INTO user_entries (user_id, entry_id, starred_at) VALUES (?, ?, ?)", aliceID, starred.ID, time.Now().UTC())

	subscribedURL := "https://cleanup-subscribed.example/feed"
	alice.subscribe(subscribedURL)
	subscribed := reloadFeed(t, subscribedURL)
	database.Model(&subscribed).Update("next_fetch_at", future)
	addEntries(t, subscribed, 35)

	if _, err := worker.NewWorker(database).Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}

	var rows int64
	database.Table("entries").Where("feed_id = ?", pool.ID).Count(&rows)
	if rows != 35 {
		t.Errorf("pool feed has %d rows, want all 35 kept", rows)
	}
	left := withContent(t, pool)
	if len(left) != 31 {
		t.Errorf("pool feed has text for %d entries, want 30 newest + 1 starred", len(left))
	}
	for i, e := range poolEntries {
		if keep := i < 30 || e.ID == starred.ID; keep != left[e.ID] {
			t.Errorf("entry %d (%d days old): kept=%v, want %v", e.ID, i, left[e.ID], keep)
		}
	}
	if n := len(withContent(t, subscribed)); n != 35 {
		t.Errorf("subscribed feed has text for %d entries, want all 35", n)
	}
}

// The feed still lists old articles after cleanup. When it is fetched again,
// they must not come back as "new" entries and land in a new subscriber's
// unread list.
func TestCleanedEntriesDoNotComeBackAsNew(t *testing.T) {
	skipShort(t)
	items := make([]string, 35)
	for i := range items {
		items[i] = item(i + 1) // Jan 1..35, 2024
	}
	feed := newFeedServer(t, items...)
	insertFeed(t, feed.URL(), true, time.Now().UTC().Add(-time.Hour))
	w := worker.NewWorker(database)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	waitFetched(t, feed.URL())
	cancel()
	<-done
	if _, err := w.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}

	alice := newClient(t, "cleanup-resubscribe")
	alice.subscribe(feed.URL()) // already fetched: gets the newest 20 right away
	before := len(alice.entries("unread"))
	f := reloadFeed(t, feed.URL())
	database.Model(&f).Update("last_fetched_at", time.Now().UTC().Add(-time.Hour)) // allow a manual refresh
	var sub struct{ ID uint64 }
	database.Table("subscriptions").Where("feed_id = ?", f.ID).Select("id").Scan(&sub)
	alice.do("POST", fmt.Sprintf("/api/subscriptions/%d/refresh", sub.ID), nil, 202, nil)
	startWorker(t)
	deadline := time.Now().Add(15 * time.Second)
	for reloadFeed(t, feed.URL()).LastFetchedAt.Before(time.Now().UTC().Add(-time.Minute)) && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if after := len(alice.entries("unread")); before != 20 || after != 20 {
		t.Fatalf("unread before refetch %d, after %d; want 20 and 20", before, after)
	}
}

// Someone may subscribe after Cleanup listed a feed but before it is pruned.
// PruneFeed re-checks under the feed row lock and leaves the feed alone.
func TestPruneSkipsFeedSubscribedMeanwhile(t *testing.T) {
	skipShort(t)
	url := "https://cleanup-race.example/feed"
	newClient(t, "cleanup-race").subscribe(url)
	f := reloadFeed(t, url)
	database.Model(&f).Update("next_fetch_at", time.Now().UTC().Add(24*time.Hour))
	addEntries(t, f, 35)
	n, err := worker.NewRepo(database).PruneFeed(context.Background(), f.ID, 30)
	if err != nil || n != 0 || len(withContent(t, f)) != 35 {
		t.Fatalf("pruned %d entries (err %v); a subscribed feed must be left alone", n, err)
	}
}

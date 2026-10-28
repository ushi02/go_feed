package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ushi02/go_feed/backend/internal/fetcher"
	"github.com/ushi02/go_feed/backend/internal/subscription"
	"github.com/ushi02/go_feed/backend/internal/worker"
)

// insertFeed creates a feed row directly, as the blog pool import does.
func insertFeed(t *testing.T, url string, inPool bool, nextFetch time.Time) subscription.Feed {
	t.Helper()
	canonical, err := fetcher.Canonical(url)
	if err != nil {
		t.Fatal(err)
	}
	f := subscription.Feed{URL: canonical, URLHash: fetcher.Hash(canonical), InPool: inPool, NextFetchAt: nextFetch}
	if err := database.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func reloadFeed(t *testing.T, url string) subscription.Feed {
	t.Helper()
	canonical, _ := fetcher.Canonical(url)
	var f subscription.Feed
	if err := database.Where("url_hash = ?", fetcher.Hash(canonical)).First(&f).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func waitFetched(t *testing.T, url string) subscription.Feed {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if f := reloadFeed(t, url); f.LastFetchedAt != nil {
			return f
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("feed %s was never fetched", url)
	return subscription.Feed{}
}

func assertInterval(t *testing.T, f subscription.Feed, want time.Duration) {
	t.Helper()
	got := f.NextFetchAt.Sub(*f.LastFetchedAt)
	if got < want-time.Minute || got > want+time.Minute {
		t.Errorf("feed %s: next fetch %s after last fetch, want %s", f.URL, got, want)
	}
}

// Subscribed feeds are fetched every 6 hours, pool-only feeds daily, and feeds
// that are neither are not fetched at all.
func TestWorkerSchedulesByAudience(t *testing.T) {
	skipShort(t)
	now := time.Now().UTC()
	pool := newFeedServer(t, item(1))
	orphan := newFeedServer(t, item(1))
	subscribed := newFeedServer(t, item(1))
	insertFeed(t, pool.URL(), true, now.Add(-time.Minute))
	// Due earlier than the pool feed, so it would be claimed first if it were eligible.
	insertFeed(t, orphan.URL(), false, now.Add(-2*time.Minute))
	newClient(t, "audience").subscribe(subscribed.URL())

	startWorker(t)
	assertInterval(t, waitFetched(t, pool.URL()), 24*time.Hour)
	assertInterval(t, waitFetched(t, subscribed.URL()), 6*time.Hour)
	if f := reloadFeed(t, orphan.URL()); f.LastFetchedAt != nil {
		t.Errorf("feed with no subscribers outside the pool was fetched")
	}
}

// Stopping the Worker cancels in-flight fetches, returns promptly, and gives the
// lease back without counting the interruption as a feed failure.
func TestWorkerShutdownReleasesLease(t *testing.T) {
	skipShort(t)
	started := make(chan struct{}, 1)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	url := slow.URL + "/feed.xml"
	insertFeed(t, url, true, time.Now().UTC().Add(-time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.NewWorker(database).Run(ctx)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(15 * time.Second):
		cancel()
		t.Fatal("worker never fetched the feed")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	f := reloadFeed(t, url)
	if f.LeaseToken != nil || f.FailureCount != 0 || f.LastError != "" {
		t.Fatalf("after shutdown: lease=%v failures=%d error=%q", f.LeaseToken, f.FailureCount, f.LastError)
	}
}

// A feed whose entries fail to store is a failed fetch: it gets a failure and a
// backoff instead of keeping its lease and being retried every 2 minutes.
func TestStoreFailureIsRecordedAsFeedFailure(t *testing.T) {
	skipShort(t)
	bad := newFeedServer(t, `<item><guid>g</guid><title>Bad</title><pubDate>Mon, 01 Jan 0001 00:00:00 +0000</pubDate></item>`)
	insertFeed(t, bad.URL(), true, time.Now().UTC().Add(-time.Hour))
	startWorker(t)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		f := reloadFeed(t, bad.URL())
		if f.LastFetchedAt != nil || f.FailureCount > 0 {
			if f.LeaseToken != nil || f.NextFetchAt.Before(time.Now().UTC()) {
				t.Fatalf("feed left leased or due again: lease=%v next=%s", f.LeaseToken, f.NextFetchAt)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("feed was neither stored nor marked failed")
}

// A feed keeps the URL it was created with even if the server redirects.
// Otherwise re-importing the blog pool no longer finds it and inserts a duplicate.
func TestRedirectDoesNotChangeFeedURL(t *testing.T) {
	skipShort(t)
	feed := newFeedServer(t, item(1))
	mux := http.NewServeMux()
	mux.Handle("/old.xml", http.RedirectHandler(feed.URL(), http.StatusMovedPermanently))
	old := httptest.NewServer(mux)
	t.Cleanup(old.Close)
	oldURL := old.URL + "/old.xml"
	insertFeed(t, oldURL, true, time.Now().UTC().Add(-time.Hour))
	startWorker(t)
	f := waitFetched(t, oldURL) // looks the feed up by its original URL
	var n int64
	database.Table("entries").Where("feed_id = ?", f.ID).Count(&n)
	if n != 1 {
		t.Fatalf("redirected feed stored %d entries, want 1", n)
	}
}

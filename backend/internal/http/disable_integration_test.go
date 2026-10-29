package http_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ushi02/go_feed/backend/internal/subscription"
)

// statusServer answers every request with the given HTTP status.
func statusServer(t *testing.T, code int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
	t.Cleanup(srv.Close)
	return srv.URL + "/feed.xml"
}

// waitFailed waits until the Worker has recorded at least `failures` failures.
func waitFailed(t *testing.T, url string, failures int) subscription.Feed {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if f := reloadFeed(t, url); f.FailureCount >= failures {
			return f
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("feed %s never reached %d failures", url, failures)
	return subscription.Feed{}
}

func setFailures(t *testing.T, f subscription.Feed, n int) {
	t.Helper()
	if err := database.Model(&f).Update("failure_count", n).Error; err != nil {
		t.Fatal(err)
	}
}

// 410 Gone means the feed was removed on purpose: stop at the first failure.
func TestGoneFeedIsDisabledAtOnce(t *testing.T) {
	skipShort(t)
	url := statusServer(t, http.StatusGone)
	insertFeed(t, url, true, time.Now().UTC().Add(-time.Hour))
	startWorker(t)
	if f := waitFailed(t, url, 1); f.DisabledAt == nil {
		t.Fatal("410 feed was not disabled")
	}
}

// Other failures may be temporary: only the 10th failure in a row disables.
func TestFeedDisabledAfterTenFailures(t *testing.T) {
	skipShort(t)
	ninth := statusServer(t, http.StatusNotFound)
	fourth := statusServer(t, http.StatusNotFound)
	past := time.Now().UTC().Add(-time.Hour)
	setFailures(t, insertFeed(t, ninth, true, past), 9)
	setFailures(t, insertFeed(t, fourth, true, past), 3)
	startWorker(t)
	if f := waitFailed(t, ninth, 10); f.DisabledAt == nil {
		t.Errorf("feed with 10 failures in a row was not disabled")
	}
	if f := waitFailed(t, fourth, 4); f.DisabledAt != nil {
		t.Errorf("feed with 4 failures was disabled too early")
	}
}

// A disabled feed is never claimed, even when it is due.
func TestDisabledFeedIsNotFetched(t *testing.T) {
	skipShort(t)
	dead := newFeedServer(t, item(1))
	alive := newFeedServer(t, item(1))
	now := time.Now().UTC()
	f := insertFeed(t, dead.URL(), true, now.Add(-2*time.Minute)) // due first
	database.Model(&f).Update("disabled_at", now)
	insertFeed(t, alive.URL(), true, now.Add(-time.Minute))
	startWorker(t)
	waitFetched(t, alive.URL())
	if got := reloadFeed(t, dead.URL()); got.LastFetchedAt != nil {
		t.Fatal("disabled feed was fetched")
	}
}

// Refreshing by hand or subscribing gives a dead feed one more chance.
func TestRefreshAndSubscribeReEnableFeed(t *testing.T) {
	skipShort(t)
	refreshed := newFeedServer(t, item(1))
	subscribed := newFeedServer(t, item(1))
	alice := newClient(t, "revive")

	id := alice.subscribe(refreshed.URL())
	f := reloadFeed(t, refreshed.URL())
	database.Model(&f).Updates(map[string]any{"disabled_at": time.Now().UTC(), "next_fetch_at": time.Now().UTC().Add(time.Hour)})
	alice.do("POST", fmt.Sprintf("/api/subscriptions/%d/refresh", id), nil, 202, nil)
	if got := reloadFeed(t, refreshed.URL()); got.DisabledAt != nil {
		t.Error("refresh did not re-enable the feed")
	}

	// The feed already has a subscriber (bob) when alice subscribes.
	newClient(t, "revive-bob").subscribe(subscribed.URL())
	g := reloadFeed(t, subscribed.URL())
	database.Model(&g).Update("disabled_at", time.Now().UTC())
	alice.subscribe(subscribed.URL())
	if got := reloadFeed(t, subscribed.URL()); got.DisabledAt != nil {
		t.Error("subscribing did not re-enable the feed")
	}
}

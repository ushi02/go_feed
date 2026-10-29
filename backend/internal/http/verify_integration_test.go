package http_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ushi02/go_feed/backend/internal/entry"
	"github.com/ushi02/go_feed/backend/internal/subscription"
)

// Verify is what stands between the chatbot and the user: only a feed that
// was really downloaded and parsed may be shown as a card.
func TestVerifyFeed(t *testing.T) {
	skipShort(t)
	feed := newFeedServer(t, item(1))
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprintf(w, `<html><head><link rel="alternate" type="application/rss+xml" href="%s"></head></html>`, feed.URL())
		case "/no-feed":
			fmt.Fprint(w, `<html><head><title>没有 RSS</title></head></html>`)
		case "/broken-feed":
			// The page advertises a feed, but the feed address returns 404.
			fmt.Fprint(w, `<html><head><link rel="alternate" type="application/rss+xml" href="/missing.xml"></head></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(site.Close)

	svc := subscription.NewService(subscription.NewRepo(database), entry.NewRepo(database))
	alice := newClient(t, "verify-alice")
	var aliceID uint64
	database.Table("users").Where("username = ?", "verify-alice").Pluck("id", &aliceID)
	ctx := context.Background()

	t.Run("feed URL", func(t *testing.T) {
		v, err := svc.Verify(ctx, aliceID, feed.URL())
		if err != nil {
			t.Fatal(err)
		}
		if v.FeedURL != feed.URL() || v.Title != "Test Blog" || v.SiteURL != "https://blog.example.com/" || v.Subscribed {
			t.Fatalf("got %+v", v)
		}
	})

	t.Run("homepage finds its feed", func(t *testing.T) {
		v, err := svc.Verify(ctx, aliceID, site.URL+"/")
		if err != nil || v.FeedURL != feed.URL() {
			t.Fatalf("got %+v, %v", v, err)
		}
	})

	t.Run("knows when the user already subscribes", func(t *testing.T) {
		alice.subscribe(feed.URL())
		v, err := svc.Verify(ctx, aliceID, feed.URL())
		if err != nil || !v.Subscribed {
			t.Fatalf("got %+v, %v", v, err)
		}
	})

	for _, path := range []string{"/no-feed", "/broken-feed", "/does-not-exist"} {
		t.Run("rejects "+path, func(t *testing.T) {
			if v, err := svc.Verify(ctx, aliceID, site.URL+path); err == nil {
				t.Fatalf("verified %+v", v)
			} else if path == "/broken-feed" && !errors.Is(err, subscription.ErrNoFeed) {
				// Discover alone would accept this page; the fetch must reject it.
				t.Fatalf("err = %v, want ErrNoFeed", err)
			}
		})
	}
}

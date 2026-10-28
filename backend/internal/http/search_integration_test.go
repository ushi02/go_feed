package http_test

import (
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/ushi02/go_feed/backend/internal/entry"
	"github.com/ushi02/go_feed/backend/internal/fetcher"
	"github.com/ushi02/go_feed/backend/internal/subscription"
)

type searchResult struct {
	EntryID    uint64 `json:"entry_id"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	FeedURL    string `json:"feed_url"`
	Subscribed bool   `json:"subscribed"`
}

func (c *client) search(q string) []searchResult {
	var out struct {
		Results []searchResult `json:"results"`
	}
	c.do("GET", "/api/search?q="+url.QueryEscape(q), nil, 200, &out)
	return out.Results
}

func addEntry(t *testing.T, f subscription.Feed, title, summary string) entry.Entry {
	t.Helper()
	now := time.Now().UTC()
	e := entry.Entry{FeedID: f.ID, GUIDHash: fetcher.Hash(title + summary), Title: title, Summary: summary, SortAt: now, CreatedAt: now}
	if err := database.Create(&e).Error; err != nil {
		t.Fatal(err)
	}
	return e
}

func resultTitles(rs []searchResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Title
	}
	sort.Strings(out)
	return out
}

func TestSearch(t *testing.T) {
	skipShort(t)
	future := time.Now().UTC().Add(24 * time.Hour) // keep the Worker away from these fake URLs
	pool := insertFeed(t, "https://whale-pool.example/feed", true, future)
	addEntry(t, pool, "关于鲸鱼歌声的研究", "")
	addEntry(t, pool, "日常", "今天在海边听到了鲸鱼歌声")
	addEntry(t, pool, "鲸鱼和海豚", "")
	addEntry(t, pool, "Learning Golang channels", "")

	dead := insertFeed(t, "https://whale-dead.example/feed", true, future)
	database.Model(&dead).Update("disabled_at", time.Now().UTC())
	addEntry(t, dead, "失效博客里的鲸鱼歌声", "")

	// A feed outside the pool that only bob reads: alice must not find it.
	bob := newClient(t, "whale-bob")
	privateURL := "https://whale-private.example/feed"
	bob.subscribe(privateURL)
	private := reloadFeed(t, privateURL)
	database.Model(&private).Update("next_fetch_at", future)
	secret := addEntry(t, private, "私人订阅里的鲸鱼歌声", "")
	var bobID uint64
	database.Table("users").Where("username = ?", "whale-bob").Pluck("id", &bobID)
	database.Exec("INSERT INTO user_entries (user_id, entry_id) VALUES (?, ?)", bobID, secret.ID)

	alice := newClient(t, "whale-alice")

	t.Run("title or summary, live pool only", func(t *testing.T) {
		got := resultTitles(alice.search("鲸鱼歌声"))
		want := []string{"关于鲸鱼歌声的研究", "日常"}
		if !equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("every word must match", func(t *testing.T) {
		if got := resultTitles(alice.search("鲸鱼 海豚")); !equal(got, []string{"鲸鱼和海豚"}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("english word", func(t *testing.T) {
		if got := resultTitles(alice.search("golang")); !equal(got, []string{"Learning Golang channels"}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("own entries outside the pool", func(t *testing.T) {
		found := false
		for _, r := range bob.search("鲸鱼歌声") {
			if r.EntryID == secret.ID {
				found = true
				if !r.Subscribed {
					t.Error("bob's own feed not marked subscribed")
				}
			}
		}
		if !found {
			t.Fatal("bob cannot find an entry from his own subscription")
		}
	})
	t.Run("search operators in input are harmless", func(t *testing.T) {
		alice.search(`鲸鱼歌声 -"(*`)
	})
	t.Run("empty query", func(t *testing.T) {
		alice.do("GET", "/api/search?q=%20", nil, 400, nil)
	})
}

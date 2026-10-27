package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/ushi02/go_feed/backend/internal/entry"
	"github.com/ushi02/go_feed/backend/internal/fetcher"
	"github.com/ushi02/go_feed/backend/internal/subscription"
	"gorm.io/gorm"
)

type Worker struct {
	repo    *Repo
	entries *entry.Repo
}

func NewWorker(db *gorm.DB) *Worker { return &Worker{repo: NewRepo(db), entries: entry.NewRepo(db)} }

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 10; i++ {
			token := randomToken()
			feed, err := w.repo.Claim(ctx, token, 2*time.Minute)
			if err != nil {
				log.Printf("claim feed: %v", err)
				break
			}
			if feed == nil {
				break
			}
			if err := w.process(ctx, *feed, token); err != nil {
				log.Printf("fetch feed %d: %v", feed.ID, err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func randomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (w *Worker) process(ctx context.Context, feed subscription.Feed, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result, err := fetcher.Fetch(ctx, feed.URL, feed.ETag, feed.LastModified)
	if err != nil {
		backoff := time.Duration(1<<min(feed.FailureCount, 5)) * time.Hour
		updateCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = w.repo.RecordFailure(updateCtx, feed, token, truncate(err.Error(), 1024), time.Now().UTC().Add(backoff))
		return err
	}
	return w.repo.Transaction(ctx, func(tx *gorm.DB) error {
		feeds, entries := w.repo.WithTx(tx), w.entries.WithTx(tx)
		// Same row lock as subscribing, so a new subscriber is either seen here or sees us.
		current, err := feeds.LockLeased(ctx, feed.ID, token)
		if err != nil {
			return err
		}
		// Follow a permanent move unless another feed already has the new URL.
		if result.FinalURL != "" && result.FinalURL != current.URL {
			taken, err := feeds.URLTaken(ctx, result.FinalURL)
			if err != nil {
				return err
			}
			if !taken {
				if err := feeds.UpdateURL(ctx, feed.ID, result.FinalURL); err != nil {
					return err
				}
				current.URL = result.FinalURL
			}
		}
		now := time.Now().UTC()
		if !result.NotModified {
			var added []uint64
			for _, item := range result.Feed.Items {
				e := entryFromItem(feed.ID, current.URL, item, now)
				if e == nil {
					continue
				}
				inserted, err := entries.Insert(ctx, e)
				if err != nil {
					return err
				}
				if inserted {
					added = append(added, e.ID)
				}
			}
			// First fetch: each subscriber starts with the most recent entries, not the whole archive.
			// Later fetches: every new entry goes to every subscriber.
			if !current.Initialized {
				err = entries.Backfill(ctx, feed.ID, nil)
			} else {
				err = entries.Deliver(ctx, feed.ID, added)
			}
			if err != nil {
				return err
			}
		}
		updates := map[string]any{"last_fetched_at": now, "next_fetch_at": now.Add(6 * time.Hour), "lease_until": nil, "lease_token": nil,
			"failure_count": 0, "last_error": "", "initialized": true}
		if !result.NotModified {
			updates["title"] = truncate(result.Feed.Title, 512)
			updates["site_url"] = result.Feed.Link
		}
		if result.ETag != "" {
			updates["etag"] = truncate(result.ETag, 512)
		}
		if result.Modified != "" {
			updates["last_modified"] = truncate(result.Modified, 512)
		}
		return feeds.Finish(ctx, feed.ID, token, updates)
	})
}

func entryFromItem(feedID uint64, feedURL string, item *gofeed.Item, now time.Time) *entry.Entry {
	key := strings.TrimSpace(item.GUID)
	if key == "" {
		key = strings.TrimSpace(item.Link)
	}
	if key == "" {
		key = item.Title + "\x00" + item.Published
	}
	if strings.TrimSpace(key) == "" {
		return nil
	}
	var published *time.Time
	sortAt := now
	if item.PublishedParsed != nil {
		t := item.PublishedParsed.UTC()
		if t.After(now) {
			t = now
		}
		published = &t
		sortAt = t
	}
	link := fetcher.ResolvePublicURL(item.Link, feedURL)
	base := link
	if base == "" {
		base = feedURL
	}
	content := item.Content
	if content == "" {
		content = item.Description
	}
	return &entry.Entry{FeedID: feedID, GUIDHash: fetcher.Hash(key), Title: truncate(item.Title, 512), URL: link,
		Content: fetcher.CleanHTML(content, base), Summary: truncate(item.Description, 4000), PublishedAt: published, SortAt: sortAt, CreatedAt: now}
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}

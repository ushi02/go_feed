package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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

const (
	concurrency      = 8              // feeds fetched at the same time
	subscribedEvery  = 6 * time.Hour  // feeds someone subscribes to
	poolOnlyEvery    = 24 * time.Hour // pool feeds nobody subscribes to
	idlePollInterval = 2 * time.Second
	leaseDuration    = 2 * time.Minute
	// A feed is judged dead after this many failures in a row. With the backoff
	// below that is about a week, so a blog that is down for a weekend survives.
	maxFailures = 10
	// Summaries feed the search index (title + summary), so keep them short.
	summaryRunes = 500
)

// Run fetches due feeds round after round until ctx is cancelled. It returns
// only after every in-flight fetch has stopped.
func (w *Worker) Run(ctx context.Context) {
	for {
		w.runRound(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(idlePollInterval):
		}
	}
}

type job struct {
	feed  subscription.Feed
	token string
}

// runRound claims due feeds one by one and hands them to a fixed pool of
// goroutines, so at most `concurrency` fetches run at once. When nothing is
// due, it waits for the pool to drain and logs how the round went.
func (w *Worker) runRound(ctx context.Context) {
	jobs := make(chan job) // unbuffered: a feed is claimed only when a goroutine is free
	var wg sync.WaitGroup
	var fetched, failed atomic.Int64
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				err := w.process(ctx, j.feed, j.token)
				if ctx.Err() != nil {
					w.release(j)
					continue
				}
				fetched.Add(1)
				if err != nil {
					failed.Add(1)
					log.Printf("fetch feed %d: %v", j.feed.ID, err)
				}
			}
		}()
	}
	start := time.Now()
	for ctx.Err() == nil {
		token := randomToken()
		feed, err := w.repo.Claim(ctx, token, leaseDuration)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("claim feed: %v", err)
			}
			break
		}
		if feed == nil {
			break
		}
		select {
		case jobs <- job{*feed, token}:
		case <-ctx.Done():
			w.release(job{*feed, token})
		}
	}
	close(jobs)
	wg.Wait()
	if n := fetched.Load(); n > 0 {
		f := failed.Load()
		log.Printf("round done: %d feeds in %s, %d failed (%.1f%%)", n, time.Since(start).Round(time.Millisecond), f, float64(f)*100/float64(n))
	}
}

// release hands the lease back after shutdown interrupted a fetch. It uses its
// own context because the Worker's context is already cancelled.
func (w *Worker) release(j job) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.repo.Release(ctx, j.feed.ID, j.token); err != nil {
		log.Printf("release feed %d: %v", j.feed.ID, err)
	}
}

func randomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// process fetches one feed and stores it. Any failure, in fetching or in
// storing, backs the feed off; otherwise it would keep its lease and be
// retried as soon as the lease expires.
func (w *Worker) process(parent context.Context, feed subscription.Feed, token string) error {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	err := w.fetchAndStore(ctx, feed, token)
	if err != nil && parent.Err() == nil { // on shutdown it is not the feed's fault; the caller releases the lease
		now := time.Now().UTC()
		backoff := time.Duration(1<<min(feed.FailureCount, 5)) * time.Hour
		var disabledAt *time.Time
		if isDead(err, feed.FailureCount+1) {
			disabledAt = &now
		}
		updateCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = w.repo.RecordFailure(updateCtx, feed, token, truncate(err.Error(), 1024), now.Add(backoff), disabledAt)
	}
	return err
}

// isDead reports whether a feed should stop being fetched: the server says it
// is gone for good (410), or it has failed maxFailures times in a row.
func isDead(err error, failures int) bool {
	var status *fetcher.StatusError
	if errors.As(err, &status) && status.Code == http.StatusGone {
		return true
	}
	return failures >= maxFailures
}

func (w *Worker) fetchAndStore(ctx context.Context, feed subscription.Feed, token string) error {
	result, err := fetcher.Fetch(ctx, feed.URL, feed.ETag, feed.LastModified)
	if err != nil {
		return err
	}
	return w.repo.Transaction(ctx, func(tx *gorm.DB) error {
		feeds, entries := w.repo.WithTx(tx), w.entries.WithTx(tx)
		// Same row lock as subscribing, so a new subscriber is either seen here or sees us.
		current, err := feeds.LockLeased(ctx, feed.ID, token)
		if err != nil {
			return err
		}
		// The feed keeps its original URL (the blog pool import and subscriptions
		// find it by that URL); only relative links resolve against where we landed.
		base := current.URL
		if result.FinalURL != "" {
			base = result.FinalURL
		}
		now := time.Now().UTC()
		subscribed, err := feeds.HasSubscribers(ctx, feed.ID)
		if err != nil {
			return err
		}
		interval := poolOnlyEvery
		if subscribed {
			interval = subscribedEvery
		}
		if !result.NotModified {
			var added []uint64
			for _, item := range result.Feed.Items {
				e := entryFromItem(feed.ID, base, item, now)
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
		updates := map[string]any{"last_fetched_at": now, "next_fetch_at": now.Add(interval), "lease_until": nil, "lease_token": nil,
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
	// Dates before 1970 are bogus (often Go's zero time) and outside MySQL's DATETIME range.
	if item.PublishedParsed != nil && item.PublishedParsed.Year() >= 1970 {
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
	// The summary is plain text for the search index; feeds without a
	// description get the start of the article instead.
	summary := item.Description
	if summary == "" {
		summary = content
	}
	return &entry.Entry{FeedID: feedID, GUIDHash: fetcher.Hash(key), Title: truncate(item.Title, 512), URL: link,
		Content: fetcher.CleanHTML(content, base), Summary: fetcher.PlainText(summary, summaryRunes), PublishedAt: published, SortAt: sortAt, CreatedAt: now}
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}

package reader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/ushi02/go_feed/internal/feedfetch"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Worker struct{ DB *gorm.DB }

func NewWorker(db *gorm.DB) *Worker { return &Worker{DB: db} }

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 10; i++ {
			feed, token, err := w.claim(ctx)
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

func (w *Worker) claim(ctx context.Context) (*Feed, string, error) {
	var feed Feed
	token := randomToken()
	err := w.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("next_fetch_at <= UTC_TIMESTAMP(6) AND (lease_until IS NULL OR lease_until < UTC_TIMESTAMP(6))").
			Where("EXISTS (SELECT 1 FROM subscriptions WHERE subscriptions.feed_id = feeds.id)").
			Order("next_fetch_at, id").Limit(1).Find(&feed)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Model(&Feed{}).Where("id = ?", feed.ID).Updates(map[string]any{
			"lease_until": time.Now().UTC().Add(2 * time.Minute), "lease_token": token,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return &feed, token, nil
}

func (w *Worker) process(ctx context.Context, feed Feed, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result, err := feedfetch.Fetch(ctx, feed.URL, feed.ETag, feed.LastModified)
	if err != nil {
		backoff := time.Duration(1<<min(feed.FailureCount, 5)) * time.Hour
		updateCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = w.DB.WithContext(updateCtx).Model(&Feed{}).Where("id = ? AND lease_token = ?", feed.ID, token).Updates(map[string]any{
			"failure_count": feed.FailureCount + 1, "last_error": truncate(err.Error(), 1024),
			"next_fetch_at": time.Now().UTC().Add(backoff), "lease_until": nil, "lease_token": nil,
		}).Error
		return err
	}
	return w.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Feed
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND lease_token = ?", feed.ID, token).First(&current).Error; err != nil {
			return err
		}
		if result.FinalURL != "" && result.FinalURL != current.URL {
			newHash := feedfetch.Hash(result.FinalURL)
			var existing Feed
			err := tx.Where("url_hash = ?", newHash).First(&existing).Error
			if err == nil && existing.ID != current.ID && !current.Initialized {
				var subs []Subscription
				if err := tx.Where("feed_id = ?", current.ID).Find(&subs).Error; err != nil {
					return err
				}
				for _, sub := range subs {
					var duplicate int64
					if err := tx.Model(&Subscription{}).Where("user_id = ? AND feed_id = ?", sub.UserID, existing.ID).Count(&duplicate).Error; err != nil {
						return err
					}
					if duplicate != 0 {
						if err := tx.Delete(&sub).Error; err != nil {
							return err
						}
						continue
					}
					updates := map[string]any{"feed_id": existing.ID, "pending_baseline": !existing.Initialized}
					if existing.Initialized {
						baseline, err := baselineID(tx, existing.ID)
						if err != nil {
							return err
						}
						updates["read_through_entry_id"] = baseline
					}
					if err := tx.Model(&sub).Updates(updates).Error; err != nil {
						return err
					}
				}
				return tx.Delete(&current).Error
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err != nil {
				if err := tx.Model(&current).Updates(map[string]any{"url": result.FinalURL, "url_hash": newHash}).Error; err != nil {
					return err
				}
				current.URL = result.FinalURL
			}
		}
		now := time.Now().UTC()
		if !result.NotModified {
			for _, item := range result.Feed.Items {
				entry := entryFromItem(feed.ID, current.URL, item, now)
				if entry == nil {
					continue
				}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(entry).Error; err != nil {
					return err
				}
			}
			baseline, err := baselineID(tx, feed.ID)
			if err != nil {
				return err
			}
			if err := tx.Model(&Subscription{}).Where("feed_id = ? AND pending_baseline = 1", feed.ID).
				Updates(map[string]any{"read_through_entry_id": baseline, "pending_baseline": false}).Error; err != nil {
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
		return tx.Model(&Feed{}).Where("id = ? AND lease_token = ?", feed.ID, token).Updates(updates).Error
	})
}

func baselineID(tx *gorm.DB, feedID uint64) (uint64, error) {
	var ids []uint64
	if err := tx.Model(&Entry{}).Where("feed_id = ?", feedID).Order("id DESC").Limit(21).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) <= 20 {
		return 0, nil
	}
	return ids[20], nil
}

func entryFromItem(feedID uint64, feedURL string, item *gofeed.Item, now time.Time) *Entry {
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
	link := feedfetch.ResolvePublicURL(item.Link, feedURL)
	base := link
	if base == "" {
		base = feedURL
	}
	content := item.Content
	if content == "" {
		content = item.Description
	}
	return &Entry{FeedID: feedID, GUIDHash: feedfetch.Hash(key), Title: truncate(item.Title, 512), URL: link,
		Content: feedfetch.CleanHTML(content, base), Summary: truncate(item.Description, 4000), PublishedAt: published, SortAt: sortAt, CreatedAt: now}
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}

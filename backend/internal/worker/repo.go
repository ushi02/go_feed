package worker

import (
	"context"
	"errors"
	"time"

	"github.com/ushi02/go_feed/backend/internal/subscription"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repo struct{ db *gorm.DB }

func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

func (r *Repo) WithTx(tx *gorm.DB) *Repo { return &Repo{db: tx} }

func (r *Repo) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

// Claim leases one due feed that has subscribers. SKIP LOCKED lets several
// workers claim different feeds at the same time without waiting on each other.
func (r *Repo) Claim(ctx context.Context, token string, lease time.Duration) (*subscription.Feed, error) {
	var feed subscription.Feed
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("next_fetch_at <= UTC_TIMESTAMP(6) AND (lease_until IS NULL OR lease_until < UTC_TIMESTAMP(6))").
			Where("feeds.disabled_at IS NULL").
			// Pool feeds are fetched even without subscribers; other feeds only while someone reads them.
			Where("feeds.in_pool OR EXISTS (SELECT 1 FROM subscriptions WHERE subscriptions.feed_id = feeds.id)").
			Order("next_fetch_at, id").Limit(1).Find(&feed)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Model(&subscription.Feed{}).Where("id = ?", feed.ID).Updates(map[string]any{
			"lease_until": time.Now().UTC().Add(lease), "lease_token": token,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &feed, nil
}

// RecordFailure stores a failed fetch. disabledAt is non-nil when the feed is
// judged dead and should no longer be claimed.
func (r *Repo) RecordFailure(ctx context.Context, feed subscription.Feed, token, msg string, next time.Time, disabledAt *time.Time) error {
	return r.db.WithContext(ctx).Model(&subscription.Feed{}).Where("id = ? AND lease_token = ?", feed.ID, token).Updates(map[string]any{
		"failure_count": feed.FailureCount + 1, "last_error": msg,
		"next_fetch_at": next, "lease_until": nil, "lease_token": nil, "disabled_at": disabledAt,
	}).Error
}

// Release gives the lease back without recording a failure, so the feed is
// claimed again right away (used when the Worker shuts down mid-fetch).
func (r *Repo) Release(ctx context.Context, feedID uint64, token string) error {
	return r.db.WithContext(ctx).Model(&subscription.Feed{}).Where("id = ? AND lease_token = ?", feedID, token).
		Updates(map[string]any{"lease_until": nil, "lease_token": nil}).Error
}

func (r *Repo) HasSubscribers(ctx context.Context, feedID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Table("subscriptions").Where("feed_id = ?", feedID).Limit(1).Count(&n).Error
	return n != 0, err
}

// LockLeased locks the feed row, failing if our lease was taken over meanwhile.
func (r *Repo) LockLeased(ctx context.Context, feedID uint64, token string) (subscription.Feed, error) {
	var feed subscription.Feed
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND lease_token = ?", feedID, token).First(&feed).Error
	return feed, err
}

func (r *Repo) Finish(ctx context.Context, feedID uint64, token string, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&subscription.Feed{}).Where("id = ? AND lease_token = ?", feedID, token).Updates(updates).Error
}

// FeedsToPrune returns feeds nobody subscribes to with more than keep entries
// that still have their article text.
func (r *Repo) FeedsToPrune(ctx context.Context, keep int) ([]uint64, error) {
	var ids []uint64
	err := r.db.WithContext(ctx).Raw(`SELECT f.id FROM feeds f
WHERE NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.feed_id = f.id)
  AND (SELECT COUNT(*) FROM entries e WHERE e.feed_id = f.id AND e.content IS NOT NULL) > ?`, keep).Scan(&ids).Error
	return ids, err
}

// PruneFeed drops the article text of all but the newest keep entries of a
// feed nobody subscribes to. The rows stay: the feed still lists those
// articles, and without the row (its guid_hash) the next fetch would insert
// them again as new entries. An entry some user still has a row for (a
// starred one) keeps its text. It takes the same feed row lock as subscribing
// and re-checks for subscribers under it.
func (r *Repo) PruneFeed(ctx context.Context, feedID uint64, keep int) (int64, error) {
	var deleted int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var feed subscription.Feed
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", feedID).First(&feed).Error; err != nil {
			return err
		}
		var subscribers int64
		if err := tx.Table("subscriptions").Where("feed_id = ?", feedID).Count(&subscribers).Error; err != nil || subscribers > 0 {
			return err
		}
		var keepIDs []uint64
		if err := tx.Table("entries").Where("feed_id = ?", feedID).Order("sort_at DESC, id DESC").Limit(keep).Pluck("id", &keepIDs).Error; err != nil || len(keepIDs) == 0 {
			return err
		}
		res := tx.Exec(`UPDATE entries SET content = NULL
WHERE feed_id = ? AND id NOT IN ? AND content IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM user_entries ue WHERE ue.entry_id = entries.id)`, feedID, keepIDs)
		deleted = res.RowsAffected
		return res.Error
	})
	return deleted, err
}

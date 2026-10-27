package worker

import (
	"context"
	"errors"
	"time"

	"github.com/ushi02/go_feed/backend/internal/fetcher"
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
			Where("EXISTS (SELECT 1 FROM subscriptions WHERE subscriptions.feed_id = feeds.id)").
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

func (r *Repo) RecordFailure(ctx context.Context, feed subscription.Feed, token, msg string, next time.Time) error {
	return r.db.WithContext(ctx).Model(&subscription.Feed{}).Where("id = ? AND lease_token = ?", feed.ID, token).Updates(map[string]any{
		"failure_count": feed.FailureCount + 1, "last_error": msg,
		"next_fetch_at": next, "lease_until": nil, "lease_token": nil,
	}).Error
}

// LockLeased locks the feed row, failing if our lease was taken over meanwhile.
func (r *Repo) LockLeased(ctx context.Context, feedID uint64, token string) (subscription.Feed, error) {
	var feed subscription.Feed
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND lease_token = ?", feedID, token).First(&feed).Error
	return feed, err
}

func (r *Repo) URLTaken(ctx context.Context, url string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&subscription.Feed{}).Where("url_hash = ?", fetcher.Hash(url)).Count(&n).Error
	return n != 0, err
}

func (r *Repo) UpdateURL(ctx context.Context, feedID uint64, url string) error {
	return r.db.WithContext(ctx).Model(&subscription.Feed{}).Where("id = ?", feedID).
		Updates(map[string]any{"url": url, "url_hash": fetcher.Hash(url)}).Error
}

func (r *Repo) Finish(ctx context.Context, feedID uint64, token string, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&subscription.Feed{}).Where("id = ? AND lease_token = ?", feedID, token).Updates(updates).Error
}

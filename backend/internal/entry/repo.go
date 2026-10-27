package entry

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repo struct{ db *gorm.DB }

func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// WithTx returns a Repo that runs its queries inside tx.
func (r *Repo) WithTx(tx *gorm.DB) *Repo { return &Repo{db: tx} }

const viewColumns = "e.id, e.feed_id, f.title AS feed_title, e.title, e.url, e.sort_at, e.published_at, " +
	"ue.read_at IS NOT NULL AS `read`, ue.starred_at"

// userEntries is the user's entry rows. The subscription is a LEFT JOIN because
// starred entries stay after unsubscribing, when there is no subscription row.
func (r *Repo) userEntries(ctx context.Context, userID uint64) *gorm.DB {
	return r.db.WithContext(ctx).Table("user_entries AS ue").
		Joins("JOIN entries AS e ON e.id = ue.entry_id").
		Joins("LEFT JOIN subscriptions AS s ON s.feed_id = e.feed_id AND s.user_id = ue.user_id").
		Joins("JOIN feeds AS f ON f.id = e.feed_id").
		Where("ue.user_id = ?", userID)
}

func (r *Repo) List(ctx context.Context, q ListQuery) ([]EntryView, error) {
	db := r.userEntries(ctx, q.UserID).Select(viewColumns)
	if q.FolderID != nil {
		db = db.Where("s.folder_id = ?", *q.FolderID)
	}
	if q.SubscriptionID != nil {
		db = db.Where("s.id = ?", *q.SubscriptionID)
	}
	switch q.Filter {
	case "unread":
		db = db.Where("ue.read_at IS NULL")
	case "starred":
		db = db.Where("ue.starred_at IS NOT NULL")
	}
	if q.CursorTime != nil {
		db = db.Where("(e.sort_at < ? OR (e.sort_at = ? AND e.id < ?))", *q.CursorTime, *q.CursorTime, q.CursorID)
	}
	entries := []EntryView{}
	err := db.Order("e.sort_at DESC, e.id DESC").Limit(q.Limit).Scan(&entries).Error
	return entries, err
}

func (r *Repo) Get(ctx context.Context, userID, entryID uint64) (EntryDetail, error) {
	var d EntryDetail
	res := r.userEntries(ctx, userID).Select(viewColumns+", e.content").Where("e.id = ?", entryID).Limit(1).Scan(&d)
	if res.Error == nil && res.RowsAffected == 0 {
		return d, ErrNotFound
	}
	return d, res.Error
}

// UnreadCounts returns unread entries per subscription id.
func (r *Repo) UnreadCounts(ctx context.Context, userID uint64) (map[uint64]int64, error) {
	var rows []struct {
		SubscriptionID uint64
		Unread         int64
	}
	err := r.db.WithContext(ctx).Table("user_entries AS ue").
		Joins("JOIN entries AS e ON e.id = ue.entry_id").
		Joins("JOIN subscriptions AS s ON s.feed_id = e.feed_id AND s.user_id = ue.user_id").
		Select("s.id AS subscription_id, COUNT(*) AS unread").
		Where("ue.user_id = ? AND ue.read_at IS NULL", userID).
		Group("s.id").Scan(&rows).Error
	counts := make(map[uint64]int64, len(rows))
	for _, row := range rows {
		counts[row.SubscriptionID] = row.Unread
	}
	return counts, err
}

func (r *Repo) Delivered(ctx context.Context, userID, entryID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&UserEntry{}).Where("user_id = ? AND entry_id = ?", userID, entryID).Count(&n).Error
	return n == 1, err
}

func (r *Repo) UpdateState(ctx context.Context, userID, entryID uint64, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&UserEntry{}).Where("user_id = ? AND entry_id = ?", userID, entryID).Updates(updates).Error
}

func (r *Repo) SubscriptionExists(ctx context.Context, userID, subscriptionID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Table("subscriptions").Where("id = ? AND user_id = ?", subscriptionID, userID).Count(&n).Error
	return n == 1, err
}

// MarkRead marks every unread entry in scope as read. Scope is all subscriptions,
// one subscription, or one folder.
func (r *Repo) MarkRead(ctx context.Context, userID uint64, subscriptionID, folderID *uint64, now time.Time) error {
	sql := "UPDATE user_entries AS ue JOIN entries AS e ON e.id = ue.entry_id " +
		"LEFT JOIN subscriptions AS s ON s.feed_id = e.feed_id AND s.user_id = ue.user_id " +
		"SET ue.read_at = ? WHERE ue.user_id = ? AND ue.read_at IS NULL"
	args := []any{now, userID}
	if subscriptionID != nil {
		sql += " AND s.id = ?"
		args = append(args, *subscriptionID)
	}
	if folderID != nil {
		sql += " AND s.folder_id = ?"
		args = append(args, *folderID)
	}
	return r.db.WithContext(ctx).Exec(sql, args...).Error
}

// Insert stores e unless the feed already has an entry with the same GUID.
// It reports whether a new row was written.
func (r *Repo) Insert(ctx context.Context, e *Entry) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(e)
	return res.RowsAffected == 1, res.Error
}

// Deliver fans new entries out to every current subscriber of the feed.
func (r *Repo) Deliver(ctx context.Context, feedID uint64, entryIDs []uint64) error {
	if len(entryIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Exec("INSERT INTO user_entries (user_id, entry_id) "+
		"SELECT s.user_id, e.id FROM subscriptions AS s JOIN entries AS e ON e.feed_id = s.feed_id "+
		"WHERE s.feed_id = ? AND e.id IN ? "+
		"ON DUPLICATE KEY UPDATE read_at = user_entries.read_at", feedID, entryIDs).Error
}

// Backfill gives subscribers the feed's most recently published entries.
// With userID nil it fills every subscriber (used on a feed's first fetch).
func (r *Repo) Backfill(ctx context.Context, feedID uint64, userID *uint64) error {
	sql := "INSERT INTO user_entries (user_id, entry_id) " +
		"SELECT s.user_id, e.id FROM subscriptions AS s " +
		"CROSS JOIN (SELECT id FROM entries WHERE feed_id = ? ORDER BY sort_at DESC, id DESC LIMIT ?) AS e " +
		"WHERE s.feed_id = ?"
	args := []any{feedID, NewSubscriptionBacklog, feedID}
	if userID != nil {
		sql += " AND s.user_id = ?"
		args = append(args, *userID)
	}
	sql += " ON DUPLICATE KEY UPDATE read_at = user_entries.read_at"
	return r.db.WithContext(ctx).Exec(sql, args...).Error
}

// RemoveFeed drops one user's entries from a feed on unsubscribe, keeping starred ones.
func (r *Repo) RemoveFeed(ctx context.Context, userID, feedID uint64) error {
	return r.db.WithContext(ctx).Exec("DELETE ue FROM user_entries AS ue JOIN entries AS e ON e.id = ue.entry_id "+
		"WHERE ue.user_id = ? AND e.feed_id = ? AND ue.starred_at IS NULL", userID, feedID).Error
}

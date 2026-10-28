package subscription

import (
	"context"
	"errors"
	"time"

	"github.com/ushi02/go_feed/backend/internal/fetcher"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repo struct{ db *gorm.DB }

func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// WithTx returns a Repo that runs its queries inside tx.
func (r *Repo) WithTx(tx *gorm.DB) *Repo { return &Repo{db: tx} }

// Transaction runs fn in one database transaction. Other repos join it with WithTx(tx).
func (r *Repo) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

func (r *Repo) ListFolders(ctx context.Context, userID uint64) ([]Folder, error) {
	var folders []Folder
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("name").Find(&folders).Error
	return folders, err
}

func (r *Repo) CreateFolder(ctx context.Context, f *Folder) error {
	return r.db.WithContext(ctx).Create(f).Error
}

func (r *Repo) RenameFolder(ctx context.Context, userID, id uint64, name string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&Folder{}).Where("id = ? AND user_id = ?", id, userID).Update("name", name)
	return res.RowsAffected != 0, res.Error
}

func (r *Repo) DeleteFolder(ctx context.Context, userID, id uint64) (bool, error) {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&Folder{})
	return res.RowsAffected != 0, res.Error
}

func (r *Repo) FolderExists(ctx context.Context, userID, id uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&Folder{}).Where("id = ? AND user_id = ?", id, userID).Count(&n).Error
	return n == 1, err
}

// FolderID returns the id of the user's folder with this name, creating it if needed.
func (r *Repo) FolderID(ctx context.Context, userID uint64, name string) (uint64, error) {
	folder := Folder{UserID: userID, Name: name}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&folder).Error; err != nil {
		return 0, err
	}
	err := r.db.WithContext(ctx).Where("user_id = ? AND name = ?", userID, name).First(&folder).Error
	return folder.ID, err
}

// LockFeed returns the feed for a canonical URL, creating it if needed, and holds
// its row lock until the transaction ends. The Worker takes the same lock while it
// stores a fetch, so subscribing and a first fetch never interleave.
func (r *Repo) LockFeed(ctx context.Context, canonical string) (Feed, error) {
	feed := Feed{URL: canonical, URLHash: fetcher.Hash(canonical), NextFetchAt: time.Now().UTC()}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&feed).Error; err != nil {
		return Feed{}, err
	}
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("url_hash = ?", feed.URLHash).First(&feed).Error
	return feed, err
}

// UpsertPoolFeeds inserts pool feeds, or marks existing ones as in the pool.
// An existing feed keeps its title and fetch state.
func (r *Repo) UpsertPoolFeeds(ctx context.Context, feeds []Feed) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoUpdates: clause.Assignments(map[string]any{"in_pool": true})}).
		CreateInBatches(feeds, 200).Error
}

// ScheduleNow schedules a fetch now. A feed judged dead gets one more chance:
// if this fetch fails too, the Worker disables it again.
func (r *Repo) ScheduleNow(ctx context.Context, feedID uint64) error {
	return r.db.WithContext(ctx).Model(&Feed{}).Where("id = ?", feedID).
		Updates(map[string]any{"next_fetch_at": time.Now().UTC(), "disabled_at": nil}).Error
}

// ScheduleIfIdle is ScheduleNow unless the feed was fetched after notSince.
func (r *Repo) ScheduleIfIdle(ctx context.Context, feedID uint64, notSince time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&Feed{}).Where("id = ? AND (last_fetched_at IS NULL OR last_fetched_at < ?)", feedID, notSince).
		Updates(map[string]any{"next_fetch_at": time.Now().UTC(), "disabled_at": nil})
	return res.RowsAffected != 0, res.Error
}

func (r *Repo) ListSubscriptions(ctx context.Context, userID uint64) ([]Subscription, error) {
	var subs []Subscription
	err := r.db.WithContext(ctx).Preload("Feed").Where("user_id = ?", userID).Order("id DESC").Find(&subs).Error
	return subs, err
}

func (r *Repo) FindSubscription(ctx context.Context, userID, id uint64) (Subscription, error) {
	var sub Subscription
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&sub).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sub, ErrNotFound
	}
	return sub, err
}

func (r *Repo) IsSubscribed(ctx context.Context, userID, feedID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&Subscription{}).Where("user_id = ? AND feed_id = ?", userID, feedID).Count(&n).Error
	return n != 0, err
}

func (r *Repo) CountSubscribers(ctx context.Context, feedID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&Subscription{}).Where("feed_id = ?", feedID).Count(&n).Error
	return n, err
}

// CreateSubscription reports false if the user already subscribes to the feed.
func (r *Repo) CreateSubscription(ctx context.Context, sub *Subscription) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(sub)
	return res.RowsAffected == 1, res.Error
}

func (r *Repo) MoveSubscription(ctx context.Context, userID, id uint64, folderID *uint64) (bool, error) {
	res := r.db.WithContext(ctx).Model(&Subscription{}).Where("id = ? AND user_id = ?", id, userID).Update("folder_id", folderID)
	return res.RowsAffected != 0, res.Error
}

func (r *Repo) DeleteSubscription(ctx context.Context, sub *Subscription) error {
	return r.db.WithContext(ctx).Where("id = ? AND user_id = ?", sub.ID, sub.UserID).Delete(&Subscription{}).Error
}

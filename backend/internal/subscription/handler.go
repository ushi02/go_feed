package subscription

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/fetcher"
	"github.com/ushi02/go_feed/backend/internal/httpx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Handler struct{ DB *gorm.DB }

var errAlreadySubscribed = errors.New("already subscribed")

func (a *Handler) Discover(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	candidates, err := fetcher.Discover(ctx, c.Query("url"))
	if err != nil {
		httpx.Fail(c, 400, err.Error())
		return
	}
	c.JSON(200, gin.H{"candidates": candidates})
}

func (a *Handler) ListFolders(c *gin.Context) {
	var folders []Folder
	if err := a.DB.WithContext(c.Request.Context()).Where("user_id = ?", httpx.UserID(c)).Order("name").Find(&folders).Error; err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.JSON(200, gin.H{"folders": folders})
}

func (a *Handler) CreateFolder(c *gin.Context) {
	var req struct{ Name string }
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Name) == "" || len(req.Name) > 160 {
		httpx.Fail(c, 400, "invalid folder name")
		return
	}
	f := Folder{UserID: httpx.UserID(c), Name: strings.TrimSpace(req.Name)}
	if err := a.DB.WithContext(c.Request.Context()).Create(&f).Error; err != nil {
		httpx.Fail(c, 409, "folder already exists")
		return
	}
	c.JSON(201, f)
}

func (a *Handler) RenameFolder(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	var req struct{ Name string }
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Name) == "" || len(req.Name) > 160 {
		httpx.Fail(c, 400, "invalid folder name")
		return
	}
	r := a.DB.WithContext(c.Request.Context()).Model(&Folder{}).Where("id = ? AND user_id = ?", id, httpx.UserID(c)).Update("name", strings.TrimSpace(req.Name))
	if r.Error != nil {
		httpx.Fail(c, 409, "folder already exists")
		return
	}
	if r.RowsAffected == 0 {
		httpx.Fail(c, 404, "folder not found")
		return
	}
	c.Status(204)
}

func (a *Handler) DeleteFolder(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	r := a.DB.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", id, httpx.UserID(c)).Delete(&Folder{})
	if r.Error != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	if r.RowsAffected == 0 {
		httpx.Fail(c, 404, "folder not found")
		return
	}
	c.Status(204)
}

func (a *Handler) validFolder(ctx context.Context, userID uint64, folderID *uint64) bool {
	if folderID == nil {
		return true
	}
	var n int64
	return a.DB.WithContext(ctx).Model(&Folder{}).Where("id = ? AND user_id = ?", *folderID, userID).Count(&n).Error == nil && n == 1
}

func (a *Handler) ListSubscriptions(c *gin.Context) {
	var subs []Subscription
	if err := a.DB.WithContext(c.Request.Context()).Preload("Feed").Where("user_id = ?", httpx.UserID(c)).Order("id DESC").Find(&subs).Error; err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.JSON(200, gin.H{"subscriptions": subs})
}

func (a *Handler) CreateSubscription(c *gin.Context) {
	var req struct {
		URL      string  `json:"url"`
		FolderID *uint64 `json:"folder_id"`
	}
	if c.ShouldBindJSON(&req) != nil || !a.validFolder(c.Request.Context(), httpx.UserID(c), req.FolderID) {
		httpx.Fail(c, 400, "invalid subscription or folder")
		return
	}
	canonical, err := fetcher.Canonical(req.URL)
	if err != nil {
		httpx.Fail(c, 400, err.Error())
		return
	}
	// Direct Feed URLs are resolved by the Worker. Discovery is a separate UI step.
	var sub Subscription
	err = a.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		feed := Feed{URL: canonical, URLHash: fetcher.Hash(canonical), NextFetchAt: time.Now().UTC()}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&feed).Error; err != nil {
			return err
		}
		if err := tx.Where("url_hash = ?", feed.URLHash).First(&feed).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&Subscription{}).Where("user_id = ? AND feed_id = ?", httpx.UserID(c), feed.ID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errAlreadySubscribed
		}
		var subscriberCount int64
		if err := tx.Model(&Subscription{}).Where("feed_id = ?", feed.ID).Count(&subscriberCount).Error; err != nil {
			return err
		}
		sub = Subscription{UserID: httpx.UserID(c), FeedID: feed.ID, FolderID: req.FolderID, PendingBaseline: !feed.Initialized}
		if feed.Initialized {
			baseline, err := BaselineID(tx, feed.ID)
			if err != nil {
				return err
			}
			sub.ReadThroughEntryID = baseline
		}
		if err := tx.Create(&sub).Error; err != nil {
			return err
		}
		if subscriberCount == 0 {
			return tx.Model(&Feed{}).Where("id = ?", feed.ID).Update("next_fetch_at", time.Now().UTC()).Error
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errAlreadySubscribed) {
			httpx.Fail(c, 409, "already subscribed")
		} else {
			httpx.Fail(c, 500, "database error")
		}
		return
	}
	c.JSON(201, sub)
}

func (a *Handler) MoveSubscription(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	var req struct {
		FolderID *uint64 `json:"folder_id"`
	}
	if c.ShouldBindJSON(&req) != nil || !a.validFolder(c.Request.Context(), httpx.UserID(c), req.FolderID) {
		httpx.Fail(c, 400, "invalid folder")
		return
	}
	r := a.DB.WithContext(c.Request.Context()).Model(&Subscription{}).Where("id = ? AND user_id = ?", id, httpx.UserID(c)).Update("folder_id", req.FolderID)
	if r.Error != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	if r.RowsAffected == 0 {
		httpx.Fail(c, 404, "subscription not found")
		return
	}
	c.Status(204)
}

func (a *Handler) DeleteSubscription(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	err := a.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var sub Subscription
		if err := tx.Where("id = ? AND user_id = ?", id, httpx.UserID(c)).First(&sub).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE st FROM entry_states AS st JOIN entries AS e ON e.id = st.entry_id WHERE st.user_id = ? AND e.feed_id = ?", httpx.UserID(c), sub.FeedID).Error; err != nil {
			return err
		}
		return tx.Delete(&sub).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Fail(c, 404, "subscription not found")
		return
	}
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.Status(204)
}

func (a *Handler) RefreshSubscription(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	var sub Subscription
	if err := a.DB.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", id, httpx.UserID(c)).First(&sub).Error; err != nil {
		httpx.Fail(c, 404, "subscription not found")
		return
	}
	// Global per-source cooldown: one user's click cannot make every subscriber fetch repeatedly.
	r := a.DB.WithContext(c.Request.Context()).Model(&Feed{}).Where("id = ? AND (last_fetched_at IS NULL OR last_fetched_at < ?)", sub.FeedID, time.Now().UTC().Add(-5*time.Minute)).Update("next_fetch_at", time.Now().UTC())
	if r.Error != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	if r.RowsAffected == 0 {
		httpx.Fail(c, 429, "feed was refreshed recently")
		return
	}
	c.Status(202)
}

func validHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https")
}

// BaselineID returns the entry id that leaves the newest 20 entries of a feed unread.
// It uses the table name instead of entry.Entry so this package does not import entry.
func BaselineID(tx *gorm.DB, feedID uint64) (uint64, error) {
	var ids []uint64
	if err := tx.Table("entries").Where("feed_id = ?", feedID).Order("id DESC").Limit(21).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) <= 20 {
		return 0, nil
	}
	return ids[20], nil
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}

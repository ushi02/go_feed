package reader

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EntryView struct {
	ID          uint64     `json:"id"`
	FeedID      uint64     `json:"feed_id"`
	FeedTitle   string     `json:"feed_title"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Content     string     `json:"content"`
	SortAt      time.Time  `json:"sort_at"`
	PublishedAt *time.Time `json:"published_at"`
	Read        bool       `json:"read"`
	StarredAt   *time.Time `json:"starred_at"`
}

func encodeCursor(t time.Time, id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + ":" + strconv.FormatUint(id, 10)))
}

func decodeCursor(raw string) (time.Time, uint64, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, 0, err
	}
	s := string(b)
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return time.Time{}, 0, fmt.Errorf("invalid cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, s[:i])
	if err != nil {
		return time.Time{}, 0, err
	}
	id, err := strconv.ParseUint(s[i+1:], 10, 64)
	return t, id, err
}

func (a *API) listEntries(c *gin.Context) {
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			fail(c, 400, "limit must be 1-50")
			return
		}
		limit = n
	}
	q := a.DB.WithContext(c.Request.Context()).Table("entries AS e").
		Joins("JOIN subscriptions AS s ON s.feed_id = e.feed_id AND s.user_id = ?", uid(c)).
		Joins("JOIN feeds AS f ON f.id = e.feed_id").
		Joins("LEFT JOIN entry_states AS st ON st.entry_id = e.id AND st.user_id = ?", uid(c)).
		Select("e.id, e.feed_id, f.title AS feed_title, e.title, e.url, e.content, e.sort_at, e.published_at, " +
			"CASE WHEN st.read_override IS NOT NULL THEN st.read_override = 1 ELSE e.id <= s.read_through_entry_id END AS `read`, st.starred_at")
	if folder := c.Query("folder_id"); folder != "" {
		id, err := strconv.ParseUint(folder, 10, 64)
		if err != nil {
			fail(c, 400, "invalid folder_id")
			return
		}
		q = q.Where("s.folder_id = ?", id)
	}
	if sub := c.Query("subscription_id"); sub != "" {
		id, err := strconv.ParseUint(sub, 10, 64)
		if err != nil {
			fail(c, 400, "invalid subscription_id")
			return
		}
		q = q.Where("s.id = ?", id)
	}
	switch c.Query("filter") {
	case "", "all":
	case "unread":
		q = q.Where("CASE WHEN st.read_override IS NOT NULL THEN st.read_override = 0 ELSE e.id > s.read_through_entry_id END")
	case "starred":
		q = q.Where("st.starred_at IS NOT NULL")
	default:
		fail(c, 400, "invalid filter")
		return
	}
	if raw := c.Query("cursor"); raw != "" {
		t, id, err := decodeCursor(raw)
		if err != nil {
			fail(c, 400, "invalid cursor")
			return
		}
		q = q.Where("(e.sort_at < ? OR (e.sort_at = ? AND e.id < ?))", t, t, id)
	}
	entries := []EntryView{}
	if err := q.Order("e.sort_at DESC, e.id DESC").Limit(limit + 1).Scan(&entries).Error; err != nil {
		fail(c, 500, "database error")
		return
	}
	more := len(entries) > limit
	if more {
		entries = entries[:limit]
	}
	next := ""
	if more {
		last := entries[len(entries)-1]
		next = encodeCursor(last.SortAt, last.ID)
	}
	c.JSON(200, gin.H{"entries": entries, "next_cursor": next})
}

func (a *API) updateEntryState(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req struct {
		Read    *bool `json:"read"`
		Starred *bool `json:"starred"`
	}
	if c.ShouldBindJSON(&req) != nil || (req.Read == nil && req.Starred == nil) {
		fail(c, 400, "read or starred is required")
		return
	}
	var n int64
	err := a.DB.WithContext(c.Request.Context()).Table("entries AS e").Joins("JOIN subscriptions AS s ON s.feed_id = e.feed_id").
		Where("e.id = ? AND s.user_id = ?", id, uid(c)).Count(&n).Error
	if err != nil {
		fail(c, 500, "database error")
		return
	}
	if n == 0 {
		fail(c, 404, "entry not found in subscriptions")
		return
	}
	var state EntryState
	err = a.DB.WithContext(c.Request.Context()).Where("user_id = ? AND entry_id = ?", uid(c), id).First(&state).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		fail(c, 500, "database error")
		return
	}
	state.UserID, state.EntryID = uid(c), id
	if req.Read != nil {
		state.ReadOverride = req.Read
	}
	if req.Starred != nil {
		if *req.Starred {
			now := time.Now().UTC()
			state.StarredAt = &now
		} else {
			state.StarredAt = nil
		}
	}
	if err := a.DB.WithContext(c.Request.Context()).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "entry_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"read_override", "starred_at"}),
	}).Create(&state).Error; err != nil {
		fail(c, 500, "database error")
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *API) markRead(c *gin.Context) {
	var req struct {
		SubscriptionID *uint64 `json:"subscription_id"`
		FolderID       *uint64 `json:"folder_id"`
	}
	if c.ShouldBindJSON(&req) != nil || (req.SubscriptionID != nil && req.FolderID != nil) {
		fail(c, 400, "choose one scope")
		return
	}
	q := a.DB.WithContext(c.Request.Context()).Model(&Subscription{}).Where("user_id = ?", uid(c))
	if req.SubscriptionID != nil {
		q = q.Where("id = ?", *req.SubscriptionID)
	}
	if req.FolderID != nil {
		q = q.Where("folder_id = ?", *req.FolderID)
	}
	var subs []Subscription
	if err := q.Find(&subs).Error; err != nil {
		fail(c, 500, "database error")
		return
	}
	if req.SubscriptionID != nil && len(subs) == 0 {
		fail(c, 404, "subscription not found")
		return
	}
	err := a.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for _, sub := range subs {
			var maxID uint64
			if err := tx.Model(&Entry{}).Where("feed_id = ?", sub.FeedID).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
				return err
			}
			if err := tx.Model(&Subscription{}).Where("id = ? AND user_id = ?", sub.ID, uid(c)).Update("read_through_entry_id", maxID).Error; err != nil {
				return err
			}
			if err := tx.Exec("UPDATE entry_states st JOIN entries e ON e.id = st.entry_id SET st.read_override = NULL WHERE st.user_id = ? AND e.feed_id = ? AND e.id <= ?", uid(c), sub.FeedID, maxID).Error; err != nil {
				return err
			}
		}
		return tx.Where("user_id = ? AND read_override IS NULL AND starred_at IS NULL", uid(c)).Delete(&EntryState{}).Error
	})
	if err != nil {
		fail(c, 500, "database error")
		return
	}
	c.Status(204)
}

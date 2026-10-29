package chat

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

type Repo struct{ db *gorm.DB }

func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// Latest returns the user's newest conversation, the one the page shows.
func (r *Repo) Latest(ctx context.Context, userID uint64) (Conversation, error) {
	var c Conversation
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("id DESC").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c, errNoConversation
	}
	return c, err
}

func (r *Repo) CreateConversation(ctx context.Context, c *Conversation) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *Repo) SetTitle(ctx context.Context, userID, id uint64, title string) error {
	return r.db.WithContext(ctx).Model(&Conversation{}).Where("id = ? AND user_id = ?", id, userID).Update("title", title).Error
}

// RecentMessages returns the newest limit messages of a conversation, oldest first.
func (r *Repo) RecentMessages(ctx context.Context, userID, conversationID uint64, limit int) ([]Message, error) {
	var ms []Message
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Order("id DESC").Limit(limit).Find(&ms).Error
	for i, j := 0, len(ms)-1; i < j; i, j = i+1, j-1 {
		ms[i], ms[j] = ms[j], ms[i]
	}
	return ms, err
}

// CountSentSince counts the messages the user sent (not Gugu's replies) in
// the last window, using the database clock. Index: (user_id, role, created_at).
func (r *Repo) CountSentSince(ctx context.Context, userID uint64, hours int) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&Message{}).
		Where("user_id = ? AND role = 'user' AND created_at >= NOW(6) - INTERVAL ? HOUR", userID, hours).
		Count(&n).Error
	return n, err
}

func (r *Repo) AddMessages(ctx context.Context, ms []Message) error {
	if len(ms) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&ms).Error
}

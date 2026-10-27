package account

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type Repo struct{ db *gorm.DB }

func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

func (r *Repo) CreateUser(ctx context.Context, u *User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *Repo) FindUserByName(ctx context.Context, username string) (User, error) {
	var u User
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error
	return u, err
}

func (r *Repo) FindUser(ctx context.Context, id uint64) (User, error) {
	var u User
	err := r.db.WithContext(ctx).First(&u, id).Error
	return u, err
}

func (r *Repo) CreateSession(ctx context.Context, s *Session) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *Repo) FindSession(ctx context.Context, tokenHash string, now time.Time) (Session, error) {
	var s Session
	err := r.db.WithContext(ctx).Where("token_hash = ? AND expires_at > ?", tokenHash, now).First(&s).Error
	return s, err
}

func (r *Repo) DeleteSession(ctx context.Context, tokenHash string) error {
	return r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&Session{}).Error
}

package account

import "time"

type User struct {
	ID           uint64    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	TokenHash string
	UserID    uint64
	ExpiresAt time.Time
}

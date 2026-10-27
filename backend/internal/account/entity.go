package account

import (
	"errors"
	"time"
)

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

var (
	ErrInvalidUsername    = errors.New("username may contain only letters, numbers, _ and -")
	ErrInvalidLength      = errors.New("username must be 3-64 characters and password 8-72 characters")
	ErrUsernameTaken      = errors.New("username already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrSessionExpired     = errors.New("session expired")
)

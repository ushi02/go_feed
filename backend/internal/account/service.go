package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const SessionTTL = 30 * 24 * time.Hour

type Service struct{ repo *Repo }

func NewService(repo *Repo) *Service { return &Service{repo: repo} }

// hashToken is what we store: a leaked sessions table does not reveal usable cookies.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Register creates a user and returns the user with a new session token.
func (s *Service) Register(ctx context.Context, username, password string) (User, string, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 || len(password) < 8 || len(password) > 72 {
		return User{}, "", ErrInvalidLength
	}
	for _, r := range username {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return User{}, "", ErrInvalidUsername
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, "", err
	}
	user := User{Username: username, PasswordHash: string(hash)}
	if err := s.repo.CreateUser(ctx, &user); err != nil {
		return User{}, "", ErrUsernameTaken
	}
	token, err := s.newSession(ctx, user.ID)
	return user, token, err
}

func (s *Service) Login(ctx context.Context, username, password string) (User, string, error) {
	user, err := s.repo.FindUserByName(ctx, strings.TrimSpace(username))
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return User{}, "", ErrInvalidCredentials
	}
	token, err := s.newSession(ctx, user.ID)
	return user, token, err
}

func (s *Service) newSession(ctx context.Context, userID uint64) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	err := s.repo.CreateSession(ctx, &Session{TokenHash: hashToken(token), UserID: userID, ExpiresAt: time.Now().UTC().Add(SessionTTL)})
	return token, err
}

// Authenticate returns the user that owns a valid session token.
func (s *Service) Authenticate(ctx context.Context, token string) (uint64, error) {
	session, err := s.repo.FindSession(ctx, hashToken(token), time.Now().UTC())
	if err != nil {
		return 0, ErrSessionExpired
	}
	return session.UserID, nil
}

func (s *Service) Me(ctx context.Context, userID uint64) (User, error) {
	return s.repo.FindUser(ctx, userID)
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.repo.DeleteSession(ctx, hashToken(token))
}

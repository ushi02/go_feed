package account

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/httpx"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Handler struct{ DB *gorm.DB }

const CookieName = "go_feed_session"

func (a *Handler) Register(c *gin.Context) {
	var req struct{ Username, Password string }
	if c.ShouldBindJSON(&req) != nil {
		httpx.Fail(c, 400, "invalid JSON")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Username) > 64 || len(req.Password) < 8 || len(req.Password) > 72 {
		httpx.Fail(c, 400, "username must be 3-64 characters and password 8-72 characters")
		return
	}
	for _, r := range req.Username {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			httpx.Fail(c, 400, "username may contain only letters, numbers, _ and -")
			return
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.Fail(c, 500, "password hashing failed")
		return
	}
	user := User{Username: req.Username, PasswordHash: string(hash)}
	if err := a.DB.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
		httpx.Fail(c, 409, "username already exists")
		return
	}
	if err := a.newSession(c, user.ID); err != nil {
		httpx.Fail(c, 500, "session creation failed")
		return
	}
	c.JSON(201, user)
}

func (a *Handler) Login(c *gin.Context) {
	var req struct{ Username, Password string }
	if c.ShouldBindJSON(&req) != nil {
		httpx.Fail(c, 400, "invalid JSON")
		return
	}
	var user User
	err := a.DB.WithContext(c.Request.Context()).Where("username = ?", strings.TrimSpace(req.Username)).First(&user).Error
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		httpx.Fail(c, 401, "invalid credentials")
		return
	}
	if err := a.newSession(c, user.ID); err != nil {
		httpx.Fail(c, 500, "session creation failed")
		return
	}
	c.JSON(200, user)
}

func (a *Handler) newSession(c *gin.Context, userID uint64) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	token := hex.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	if err := a.DB.WithContext(c.Request.Context()).Create(&Session{TokenHash: hex.EncodeToString(h[:]), UserID: userID, ExpiresAt: expires}).Error; err != nil {
		return err
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, token, 30*24*3600, "/", "", os.Getenv("COOKIE_SECURE") == "1", true)
	return nil
}

func (a *Handler) Me(c *gin.Context) {
	var user User
	if err := a.DB.WithContext(c.Request.Context()).First(&user, httpx.UserID(c)).Error; err != nil {
		httpx.Fail(c, 404, "user not found")
		return
	}
	c.JSON(200, user)
}

func (a *Handler) Logout(c *gin.Context) {
	token, _ := c.Cookie(CookieName)
	h := sha256.Sum256([]byte(token))
	_ = a.DB.WithContext(c.Request.Context()).Where("token_hash = ?", hex.EncodeToString(h[:])).Delete(&Session{}).Error
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, "", -1, "/", "", os.Getenv("COOKIE_SECURE") == "1", true)
	c.Status(204)
}

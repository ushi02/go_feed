package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/account"
	"github.com/ushi02/go_feed/backend/internal/httpx"
	"gorm.io/gorm"
)

func RequireUser(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(account.CookieName)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "login required"})
			return
		}
		h := sha256.Sum256([]byte(token))
		var session account.Session
		err = db.WithContext(c.Request.Context()).Where("token_hash = ? AND expires_at > ?", hex.EncodeToString(h[:]), time.Now().UTC()).First(&session).Error
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "session expired"})
			return
		}
		httpx.SetUserID(c, session.UserID)
		c.Next()
	}
}

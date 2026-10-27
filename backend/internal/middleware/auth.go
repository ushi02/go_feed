package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/account"
	"github.com/ushi02/go_feed/backend/internal/httpx"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (uint64, error)
}

func RequireUser(auth Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(account.CookieName)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "login required"})
			return
		}
		userID, err := auth.Authenticate(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "session expired"})
			return
		}
		httpx.SetUserID(c, userID)
		c.Next()
	}
}

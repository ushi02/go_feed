package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ushi02/go_feed/backend/internal/ratelimit"
)

// Limiter limits the auth endpoints per client IP.
type Limiter struct {
	*ratelimit.Limiter
}

func NewLimiter() *Limiter {
	return &Limiter{ratelimit.New()}
}

func (l *Limiter) Middleware(name string, max int64, period time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.Allow("auth:"+name+":"+c.ClientIP(), max, period) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many attempts; try again later", "code": "too_many_attempts"})
			return
		}
		c.Next()
	}
}

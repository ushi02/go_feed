package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type counter struct {
	n       int
	expires time.Time
}

type Limiter struct {
	mu    sync.Mutex
	local map[string]counter
}

func NewLimiter() *Limiter {
	return &Limiter{local: map[string]counter{}}
}

// Allow counts one request for key and reports whether it is within max per period.
func (l *Limiter) Allow(key string, max int64, period time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.local) > 10000 {
		for k, v := range l.local {
			if now.After(v.expires) {
				delete(l.local, k)
			}
		}
	}
	v := l.local[key]
	if now.After(v.expires) {
		v = counter{expires: now.Add(period)}
	}
	v.n++
	l.local[key] = v
	return int64(v.n) <= max
}

func (l *Limiter) Middleware(name string, max int64, period time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.Allow("auth:"+name+":"+c.ClientIP(), max, period) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many attempts; try again later"})
			return
		}
		c.Next()
	}
}

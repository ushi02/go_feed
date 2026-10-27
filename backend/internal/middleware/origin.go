package middleware

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
)

// OriginCheck blocks cross-site writes (CSRF). The frontend is served from the
// same origin as the API, so a write is allowed only when the Origin's host
// matches the Host the browser sent. This works for localhost, 127.0.0.1 and
// the production domain without configuration. Scheme is not compared because
// behind Caddy the browser uses https while Go receives plain http.
func OriginCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		origin := c.GetHeader("Origin")
		if origin == "" {
			// Non-browser clients send no Origin; with a cookie it may be a
			// browser hiding the Origin, so refuse.
			if c.Request.Header.Get("Cookie") != "" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid Origin"})
				return
			}
			c.Next()
			return
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host != c.Request.Host {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid Origin"})
			return
		}
		c.Next()
	}
}

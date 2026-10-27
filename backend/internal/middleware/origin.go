package middleware

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func OriginCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		origin := c.GetHeader("Origin")
		allowed := os.Getenv("APP_ORIGIN")
		if allowed == "" {
			allowed = "http://localhost:5173"
		}
		if origin != "" && origin != allowed || (origin == "" && c.Request.Header.Get("Cookie") != "") {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid Origin"})
			return
		}
		c.Next()
	}
}

package http

import (
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ServeFrontend serves the built React app (frontend/dist) from dir for every
// path that no API route matched, so the page and the API share one origin.
func ServeFrontend(r *gin.Engine, dir string) {
	files := nethttp.FileServer(nethttp.Dir(dir))
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") ||
			(c.Request.Method != nethttp.MethodGet && c.Request.Method != nethttp.MethodHead) {
			c.JSON(nethttp.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		files.ServeHTTP(c.Writer, c.Request)
	})
}

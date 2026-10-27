package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOriginCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/x", OriginCheck(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name, host, origin, cookie string
		want                       int
	}{
		{"same origin localhost", "localhost:18080", "http://localhost:18080", "s=1", 204},
		{"same origin 127.0.0.1", "127.0.0.1:18080", "http://127.0.0.1:18080", "s=1", 204},
		{"https behind proxy", "feed.example.com", "https://feed.example.com", "s=1", 204},
		{"other site", "localhost:18080", "https://evil.example", "s=1", 403},
		{"same host other port", "localhost:18080", "http://localhost:5173", "s=1", 403},
		{"no origin with cookie", "localhost:18080", "", "s=1", 403},
		{"no origin no cookie", "localhost:18080", "", "", 204},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/x", nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.cookie != "" {
				req.Header.Set("Cookie", tt.cookie)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("got %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

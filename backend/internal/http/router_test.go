package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestServeFrontend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644)
	os.Mkdir(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o644)
	r := gin.New()
	r.GET("/api/ping", func(c *gin.Context) { c.String(200, "pong") })
	ServeFrontend(r, dir)

	tests := []struct {
		method, path string
		want         int
		body         string
	}{
		{"GET", "/", 200, "app"},
		{"GET", "/assets/app.js", 200, "console.log"},
		{"GET", "/api/ping", 200, "pong"},
		{"GET", "/api/missing", 404, "not found"},
		{"POST", "/", 404, "not found"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
		if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.body) {
			t.Errorf("%s %s = %d %q, want %d containing %q", tt.method, tt.path, rec.Code, rec.Body.String(), tt.want, tt.body)
		}
	}
}

// register sends an invalid body: the limiter runs before the handler, and the
// handler rejects it with 400 without touching the database.
func register(r http.Handler, forwardedFor string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader("{"))
	req.Header.Set("X-Forwarded-For", forwardedFor)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

func TestRateLimitBehindTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// httptest requests come from 192.0.2.1, playing the role of Caddy.
	t.Setenv("TRUSTED_PROXIES", "192.0.2.1")
	r := NewRouter(nil, nil)
	for i := 0; i < 5; i++ {
		if code := register(r, "203.0.113.1"); code != 400 {
			t.Fatalf("request %d = %d, want 400", i+1, code)
		}
	}
	if code := register(r, "203.0.113.1"); code != 429 {
		t.Fatalf("6th request from same client = %d, want 429", code)
	}
	if code := register(r, "203.0.113.2"); code != 400 {
		t.Fatalf("another client behind the proxy = %d, want 400 (own bucket)", code)
	}
}

func TestRateLimitIgnoresSpoofedForwardedFor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("TRUSTED_PROXIES", "")
	r := NewRouter(nil, nil)
	for i := 0; i < 5; i++ {
		register(r, "203.0.113."+string(rune('1'+i)))
	}
	if code := register(r, "203.0.113.9"); code != 429 {
		t.Fatalf("faking X-Forwarded-For bypassed the limit: got %d, want 429", code)
	}
}

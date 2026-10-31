package deployhook

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ushi02/go_feed/backend/internal/ratelimit"
)

func TestServer(t *testing.T) {
	key := newKey(t)
	good := sign(t, key, githubClaims())
	// Dir is not a repo, so a started deploy fails at git fetch; these tests
	// only look at what the HTTP layer allows.
	d := &Deployer{Dir: t.TempDir(), Timeout: time.Minute}
	h := (&Server{Verifier: testVerifier(key), Deployer: d, Limiter: ratelimit.New()}).Handler()

	do := func(method, path, auth, ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		req.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	tests := []struct {
		name, method, path, auth string
		want                     int
	}{
		{"no token", "POST", "/deploy", "", 401},
		{"token in wrong scheme", "POST", "/deploy", "Basic " + good, 401},
		{"forged token", "POST", "/deploy", "Bearer " + sign(t, newKey(t), githubClaims()), 401},
		{"status without token", "GET", "/deploy/status", "", 401},
		{"status with token", "GET", "/deploy/status", "Bearer " + good, 200},
		{"GET /deploy", "GET", "/deploy", "Bearer " + good, 405},
		{"other path", "GET", "/", "", 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w := do(tt.method, tt.path, tt.auth, "192.0.2.1"); w.Code != tt.want {
				t.Fatalf("got %d, want %d: %s", w.Code, tt.want, w.Body)
			}
		})
	}

	t.Run("valid token starts a deploy", func(t *testing.T) {
		w := do("POST", "/deploy", "Bearer "+good, "192.0.2.2")
		var s Status
		json.Unmarshal(w.Body.Bytes(), &s)
		if w.Code != 202 || s.SHA != testSHA {
			t.Fatalf("got %d %s", w.Code, w.Body)
		}
		// Let the background deploy fail before the temp dir is removed.
		for d.Status().State == "running" {
			time.Sleep(10 * time.Millisecond)
		}
	})

	t.Run("busy", func(t *testing.T) {
		d.mu.Lock()
		d.status = Status{State: "running", SHA: testSHA}
		d.mu.Unlock()
		if w := do("POST", "/deploy", "Bearer "+good, "192.0.2.3"); w.Code != 409 {
			t.Fatalf("got %d", w.Code)
		}
	})

	t.Run("rate limit per IP", func(t *testing.T) {
		for i := 0; i < perMinute; i++ {
			if w := do("POST", "/deploy", "", "192.0.2.4"); w.Code != 401 {
				t.Fatalf("request %d: got %d", i, w.Code)
			}
		}
		if w := do("POST", "/deploy", "", "192.0.2.4"); w.Code != 429 {
			t.Fatalf("got %d, want 429", w.Code)
		}
		if w := do("POST", "/deploy", "", "192.0.2.5"); w.Code != 401 {
			t.Fatalf("another IP: got %d", w.Code)
		}
	})
}

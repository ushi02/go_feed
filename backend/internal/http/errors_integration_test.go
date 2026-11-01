package http_test

import (
	"fmt"
	"testing"
)

// errCode sends a request that should fail and returns the code the page
// translates into the user's language.
func (c *client) errCode(method, path string, body any, wantStatus int) string {
	c.t.Helper()
	var out struct {
		Code string `json:"code"`
	}
	c.do(method, path, body, wantStatus, &out)
	return out.Code
}

func TestErrorCodes(t *testing.T) {
	skipShort(t)
	alice := newClient(t, "codes-alice")
	feed := newFeedServer(t, item(1))
	sub := alice.subscribe(feed.URL())
	// Refreshing is refused for 5 minutes after a fetch.
	database.Exec("UPDATE feeds SET last_fetched_at = NOW(6) WHERE url = ?", feed.URL())
	alice.do("POST", "/api/folders", map[string]string{"name": "Tech"}, 201, nil)

	anon := &client{t: t, router: alice.router}
	tests := []struct {
		name, method, path string
		body               any
		status             int
		want               string
	}{
		{"username with a space", "POST", "/api/auth/register", map[string]string{"username": "bad name", "password": "password123"}, 400, "invalid_username"},
		{"short password", "POST", "/api/auth/register", map[string]string{"username": "codes-bob", "password": "short"}, 400, "invalid_length"},
		{"username taken", "POST", "/api/auth/register", map[string]string{"username": "codes-alice", "password": "password123"}, 409, "username_taken"},
		{"wrong password", "POST", "/api/auth/login", map[string]string{"username": "codes-alice", "password": "wrong-password"}, 401, "invalid_credentials"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anon.t = t
			if got := anon.errCode(tt.method, tt.path, tt.body, tt.status); got != tt.want {
				t.Fatalf("code = %q, want %q", got, tt.want)
			}
		})
	}

	signedIn := []struct {
		name, method, path string
		body               any
		status             int
		want               string
	}{
		{"already subscribed", "POST", "/api/subscriptions", map[string]string{"url": feed.URL()}, 409, "already_subscribed"},
		{"not a URL", "POST", "/api/subscriptions", map[string]string{"url": "ftp://example.com/feed"}, 400, "invalid_url"},
		{"nothing to discover", "GET", "/api/discover?url=ftp://example.com", nil, 400, "discover_failed"},
		{"folder exists", "POST", "/api/folders", map[string]string{"name": "Tech"}, 409, "folder_exists"},
		{"refreshed recently", "POST", fmt.Sprintf("/api/subscriptions/%d/refresh", sub), nil, 429, "refreshed_recently"},
	}
	for _, tt := range signedIn {
		t.Run(tt.name, func(t *testing.T) {
			alice.t = t
			if got := alice.errCode(tt.method, tt.path, tt.body, tt.status); got != tt.want {
				t.Fatalf("code = %q, want %q", got, tt.want)
			}
		})
	}
}

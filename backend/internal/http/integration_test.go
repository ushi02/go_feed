package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/ushi02/go_feed/backend/internal/db"
	"github.com/ushi02/go_feed/backend/internal/fetcher"
	apihttp "github.com/ushi02/go_feed/backend/internal/http"
	"github.com/ushi02/go_feed/backend/internal/worker"
)

const testOrigin = "http://localhost:5173"

const testRSS = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Test Blog</title><link>https://blog.example.com/</link>
<item><guid>a</guid><title>First</title><link>https://blog.example.com/1</link><pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate></item>
<item><guid>b</guid><title>Second</title><link>https://blog.example.com/2</link><pubDate>Tue, 02 Jan 2024 00:00:00 GMT</pubDate></item>
<item><guid>c</guid><title>Third</title><link>https://blog.example.com/3</link><pubDate>Wed, 03 Jan 2024 00:00:00 GMT</pubDate></item>
</channel></rss>`

// TestReaderFlow covers register → subscribe → fetch → list unread → mark read
// against a real MySQL, using only the HTTP API and the real Worker.
func TestReaderFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Docker")
	}
	gin.SetMode(gin.TestMode)
	t.Setenv("APP_ORIGIN", testOrigin)
	ctx := context.Background()

	ctr, err := tcmysql.Run(ctx, "mysql:8.0",
		tcmysql.WithDatabase("go_feed"), tcmysql.WithUsername("test"), tcmysql.WithPassword("test"))
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start mysql: %v", err)
	}
	t.Setenv("MYSQL_DSN", ctr.MustConnectionString(ctx, "charset=utf8mb4", "parseTime=true", "loc=UTC"))
	database, err := db.NewDB()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(testRSS))
	}))
	defer rss.Close()
	// httptest listens on 127.0.0.1, which the SSRF guard normally blocks.
	orig := fetcher.IPAllowed
	fetcher.IPAllowed = func(netip.Addr) bool { return true }
	t.Cleanup(func() { fetcher.IPAllowed = orig })

	c := &client{t: t, router: apihttp.NewRouter(database)}

	c.do("POST", "/api/auth/register", map[string]string{"username": "alice", "password": "password123"}, 201, nil)

	var sub struct {
		ID uint64 `json:"id"`
	}
	c.do("POST", "/api/subscriptions", map[string]string{"url": rss.URL + "/feed.xml"}, 201, &sub)

	workerCtx, stopWorker := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		worker.NewWorker(database).Run(workerCtx)
		close(done)
	}()
	var unread entryList
	deadline := time.Now().Add(15 * time.Second)
	for {
		c.do("GET", "/api/entries?filter=unread", nil, 200, &unread)
		if len(unread.Entries) == 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	stopWorker()
	<-done

	if len(unread.Entries) != 3 {
		t.Fatalf("unread entries after fetch = %d, want 3", len(unread.Entries))
	}
	if got := unread.Entries[0].Title; got != "Third" {
		t.Errorf("newest entry = %q, want Third", got)
	}
	if got := unread.Entries[0].FeedTitle; got != "Test Blog" {
		t.Errorf("feed title = %q, want Test Blog", got)
	}

	c.do("POST", "/api/entries/mark-read", map[string]uint64{"subscription_id": sub.ID}, 204, nil)

	c.do("GET", "/api/entries?filter=unread", nil, 200, &unread)
	if len(unread.Entries) != 0 {
		t.Fatalf("unread entries after mark-read = %d, want 0", len(unread.Entries))
	}
	var all entryList
	c.do("GET", "/api/entries", nil, 200, &all)
	if len(all.Entries) != 3 || !all.Entries[0].Read {
		t.Fatalf("all entries = %+v, want 3 read entries", all.Entries)
	}
}

type entryList struct {
	Entries []struct {
		Title     string `json:"title"`
		FeedTitle string `json:"feed_title"`
		Read      bool   `json:"read"`
	} `json:"entries"`
}

// client calls the router in-process and keeps the session cookie between requests.
type client struct {
	t      *testing.T
	router http.Handler
	cookie *http.Cookie
}

func (c *client) do(method, path string, body any, wantStatus int, out any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.router.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "go_feed_session" {
			c.cookie = ck
		}
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			c.t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
}

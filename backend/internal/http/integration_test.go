package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/ushi02/go_feed/backend/internal/db"
	"github.com/ushi02/go_feed/backend/internal/fetcher"
	apihttp "github.com/ushi02/go_feed/backend/internal/http"
	"github.com/ushi02/go_feed/backend/internal/worker"
	"gorm.io/gorm"
)

// testOrigin matches httptest.NewRequest's default Host, so writes are same-origin.
const testOrigin = "http://example.com"

// database is one MySQL container shared by every test in this package.
// Tests stay independent by using their own users and RSS servers.
var database *gorm.DB

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	ctr, err := tcmysql.Run(ctx, "mysql:8.0",
		tcmysql.WithDatabase("go_feed"), tcmysql.WithUsername("test"), tcmysql.WithPassword("test"))
	defer func() { _ = testcontainers.TerminateContainer(ctr) }()
	if err != nil {
		log.Printf("start mysql: %v", err)
		return 1
	}
	os.Setenv("MYSQL_DSN", ctr.MustConnectionString(ctx, "charset=utf8mb4", "parseTime=true", "loc=UTC"))
	database, err = db.NewDB()
	if err != nil {
		log.Printf("connect: %v", err)
		return 1
	}
	if err := db.Migrate(database); err != nil {
		log.Printf("migrate: %v", err)
		return 1
	}
	// httptest listens on 127.0.0.1, which the SSRF guard normally blocks.
	fetcher.IPAllowed = func(netip.Addr) bool { return true }
	return m.Run()
}

func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Docker")
	}
}

// TestReaderFlow covers register → subscribe → fetch → list unread → mark read.
func TestReaderFlow(t *testing.T) {
	skipShort(t)
	feed := newFeedServer(t, item(1), item(2), item(3))
	alice := newClient(t, "alice")
	sub := alice.subscribe(feed.URL())

	startWorker(t)
	unread := alice.waitForEntries(t, "unread", 3)
	if got := unread[0].Title; got != "Post 03" {
		t.Errorf("newest entry = %q, want Post 03", got)
	}
	if got := unread[0].FeedTitle; got != "Test Blog" {
		t.Errorf("feed title = %q, want Test Blog", got)
	}

	alice.do("POST", "/api/entries/mark-read", map[string]uint64{"subscription_id": sub}, 204, nil)

	if n := len(alice.entries("unread")); n != 0 {
		t.Fatalf("unread entries after mark-read = %d, want 0", n)
	}
	all := alice.entries("")
	if len(all) != 3 || !all[0].Read {
		t.Fatalf("all entries = %+v, want 3 read entries", all)
	}
}

// A new subscription starts with the 20 most recently *published* entries,
// whatever order the feed lists them in and whether or not the feed was fetched before.
func TestNewSubscriptionGetsLatest20ByPublishedTime(t *testing.T) {
	skipShort(t)
	var items []string
	for i := 0; i < 30; i++ {
		items = append(items, item((i*7)%30+1)) // days 1..30, shuffled
	}
	feed := newFeedServer(t, items...)
	want := make([]string, 0, 20)
	for day := 30; day > 10; day-- {
		want = append(want, fmt.Sprintf("Post %02d", day))
	}

	alice := newClient(t, "alice20")
	alice.subscribe(feed.URL())
	startWorker(t)
	if got := titles(alice.waitForEntries(t, "unread", 20)); !equal(got, want) {
		t.Errorf("first subscriber unread = %v\nwant %v", got, want)
	}

	// The feed is fetched now; a second subscriber is filled in immediately.
	bob := newClient(t, "bob20")
	bob.subscribe(feed.URL())
	if got := titles(bob.entries("unread")); !equal(got, want) {
		t.Errorf("second subscriber unread = %v\nwant %v", got, want)
	}
	if n := len(bob.entries("")); n != 20 {
		t.Errorf("second subscriber sees %d entries, want 20", n)
	}
}

func TestReadStateIsPerUser(t *testing.T) {
	skipShort(t)
	feed := newFeedServer(t, item(1), item(2), item(3))
	alice := newClient(t, "alice_rs")
	bob := newClient(t, "bob_rs")
	aliceSub := alice.subscribe(feed.URL())
	bob.subscribe(feed.URL())

	startWorker(t)
	first := alice.waitForEntries(t, "unread", 3)
	bob.waitForEntries(t, "unread", 3)

	alice.do("PATCH", fmt.Sprintf("/api/entries/%d/state", first[0].ID), map[string]bool{"starred": true}, 204, nil)
	alice.do("POST", "/api/entries/mark-read", map[string]uint64{"subscription_id": aliceSub}, 204, nil)
	if n := len(alice.entries("unread")); n != 0 {
		t.Fatalf("alice unread = %d, want 0", n)
	}
	if n := len(bob.entries("unread")); n != 3 {
		t.Fatalf("bob unread after alice read = %d, want 3", n)
	}
	if n := len(bob.entries("starred")); n != 0 {
		t.Fatalf("bob starred after alice starred = %d, want 0", n)
	}

	// A new post is delivered to every subscriber as unread.
	feed.set(item(1), item(2), item(3), item(4))
	if err := database.Exec("UPDATE feeds SET next_fetch_at = UTC_TIMESTAMP(6) WHERE url = ?", feed.URL()).Error; err != nil {
		t.Fatal(err)
	}
	if got := titles(alice.waitForEntries(t, "unread", 1)); !equal(got, []string{"Post 04"}) {
		t.Errorf("alice unread after new post = %v, want [Post 04]", got)
	}
	if n := len(bob.waitForEntries(t, "unread", 4)); n != 4 {
		t.Errorf("bob unread after new post = %d, want 4", n)
	}
}

// The list stays small: bodies come only from the detail endpoint, and only for your own entries.
func TestEntryListOmitsContentAndDetailHasIt(t *testing.T) {
	skipShort(t)
	feed := newFeedServer(t, `<item><guid>body</guid><title>With body</title><description>&lt;p&gt;Hello body&lt;/p&gt;</description></item>`)
	alice := newClient(t, "alice_detail")
	alice.subscribe(feed.URL())
	startWorker(t)
	list := alice.waitForEntries(t, "", 1)

	var raw struct {
		Entries []map[string]any `json:"entries"`
	}
	alice.do("GET", "/api/entries", nil, 200, &raw)
	if _, ok := raw.Entries[0]["content"]; ok {
		t.Errorf("list entry has content field: %v", raw.Entries[0])
	}

	var detail struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	alice.do("GET", fmt.Sprintf("/api/entries/%d", list[0].ID), nil, 200, &detail)
	if detail.Title != "With body" || !strings.Contains(detail.Content, "Hello body") {
		t.Errorf("detail = %+v, want title and content", detail)
	}

	mallory := newClient(t, "mallory_detail")
	mallory.do("GET", fmt.Sprintf("/api/entries/%d", list[0].ID), nil, 404, nil)
}

func TestUnreadCountsPerSubscription(t *testing.T) {
	skipShort(t)
	a := newFeedServer(t, item(1), item(2), item(3))
	b := newFeedServer(t, item(1))
	alice := newClient(t, "alice_counts")
	subA := alice.subscribe(a.URL())
	subB := alice.subscribe(b.URL())
	startWorker(t)
	alice.waitForEntries(t, "unread", 4)

	// The newest unread entry is Post 03 of feed a, so a drops from 3 to 2.
	unread := alice.entries("unread")
	alice.do("PATCH", fmt.Sprintf("/api/entries/%d/state", unread[0].ID), map[string]bool{"read": true}, 204, nil)

	var got struct {
		Counts map[string]int `json:"counts"`
	}
	alice.do("GET", "/api/entries/unread-counts", nil, 200, &got)
	want := map[string]int{fmt.Sprint(subA): 2, fmt.Sprint(subB): 1}
	if fmt.Sprint(got.Counts) != fmt.Sprint(want) {
		t.Errorf("unread counts = %v, want %v", got.Counts, want)
	}
}

func TestUnsubscribeKeepsStarredEntries(t *testing.T) {
	skipShort(t)
	feed := newFeedServer(t, item(1), item(2))
	alice := newClient(t, "alice_keep")
	sub := alice.subscribe(feed.URL())
	startWorker(t)
	entries := alice.waitForEntries(t, "unread", 2)
	starred := entries[0]
	alice.do("PATCH", fmt.Sprintf("/api/entries/%d/state", starred.ID), map[string]bool{"starred": true}, 204, nil)

	alice.do("DELETE", fmt.Sprintf("/api/subscriptions/%d", sub), nil, 204, nil)

	if got := titles(alice.entries("starred")); !equal(got, []string{starred.Title}) {
		t.Errorf("starred after unsubscribe = %v, want [%s]", got, starred.Title)
	}
	if got := titles(alice.entries("")); !equal(got, []string{starred.Title}) {
		t.Errorf("all after unsubscribe = %v, want only the starred entry", got)
	}
	alice.do("GET", fmt.Sprintf("/api/entries/%d", starred.ID), nil, 200, nil)
}

// --- helpers ---

func item(day int) string {
	pub := time.Date(2024, 1, day, 0, 0, 0, 0, time.UTC).Format(time.RFC1123Z)
	return fmt.Sprintf(`<item><guid>post-%d</guid><title>Post %02d</title><link>https://blog.example.com/%d</link><pubDate>%s</pubDate></item>`, day, day, day, pub)
}

type feedServer struct {
	*httptest.Server
	mu    sync.Mutex
	items []string
}

func newFeedServer(t *testing.T, items ...string) *feedServer {
	f := &feedServer{items: items}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		body := strings.Join(f.items, "\n")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Test Blog</title><link>https://blog.example.com/</link>%s</channel></rss>`, body)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *feedServer) URL() string { return f.Server.URL + "/feed.xml" }

func (f *feedServer) set(items ...string) {
	f.mu.Lock()
	f.items = items
	f.mu.Unlock()
}

// startWorker runs the real Worker until the test ends.
func startWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.NewWorker(database).Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

type entryView struct {
	ID        uint64 `json:"id"`
	Title     string `json:"title"`
	FeedTitle string `json:"feed_title"`
	Read      bool   `json:"read"`
}

func titles(entries []entryView) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Title
	}
	return out
}

func equal(a, b []string) bool {
	return strings.Join(a, ",") == strings.Join(b, ",")
}

// client calls the router in-process and keeps the session cookie between requests.
type client struct {
	t      *testing.T
	router http.Handler
	cookie *http.Cookie
}

func newClient(t *testing.T, username string) *client {
	c := &client{t: t, router: apihttp.NewRouter(database)}
	c.do("POST", "/api/auth/register", map[string]string{"username": username, "password": "password123"}, 201, nil)
	return c
}

func (c *client) subscribe(url string) uint64 {
	var sub struct {
		ID uint64 `json:"id"`
	}
	c.do("POST", "/api/subscriptions", map[string]string{"url": url}, 201, &sub)
	return sub.ID
}

func (c *client) entries(filter string) []entryView {
	var out struct {
		Entries []entryView `json:"entries"`
	}
	c.do("GET", "/api/entries?limit=50&filter="+filter, nil, 200, &out)
	return out.Entries
}

// waitForEntries polls until the list has n entries, giving the Worker time to fetch.
func (c *client) waitForEntries(t *testing.T, filter string, n int) []entryView {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		got := c.entries(filter)
		if len(got) == n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("filter %q has %d entries after waiting, want %d: %v", filter, len(got), n, titles(got))
		}
		time.Sleep(200 * time.Millisecond)
	}
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

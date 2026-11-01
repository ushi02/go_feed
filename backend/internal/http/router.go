package http

import (
	"log"
	nethttp "net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/account"
	"github.com/ushi02/go_feed/backend/internal/chat"
	"github.com/ushi02/go_feed/backend/internal/entry"
	"github.com/ushi02/go_feed/backend/internal/llm"
	"github.com/ushi02/go_feed/backend/internal/middleware"
	"github.com/ushi02/go_feed/backend/internal/search"
	"github.com/ushi02/go_feed/backend/internal/subscription"
	"gorm.io/gorm"
)

// DeepSeek must keep satisfying WebSearcher, or Gugu would silently lose web
// search; this line makes that a compile error instead.
var _ chat.WebSearcher = (*llm.DeepSeek)(nil)

// NewRouter wires the app. llmClient may be nil: the chat then answers that
// it is not configured, and everything else works as usual.
func NewRouter(db *gorm.DB, llmClient llm.Client) *gin.Engine {
	// Wiring: repo (SQL) → service (rules) → handler (HTTP).
	entryRepo := entry.NewRepo(db)
	accSvc := account.NewService(account.NewRepo(db))
	acc := account.NewHandler(accSvc)
	subSvc := subscription.NewService(subscription.NewRepo(db), entryRepo)
	sub := subscription.NewHandler(subSvc)
	ent := entry.NewHandler(entry.NewService(entryRepo))
	srchSvc := search.NewService(search.NewRepo(db))
	srch := search.NewHandler(srchSvc)
	// The DeepSeek client can also search the web; a client that cannot (the
	// fake in tests) gives Gugu no web_search tool.
	web, _ := llmClient.(chat.WebSearcher)
	ch := chat.NewHandler(chat.NewService(chat.NewRepo(db), llmClient, subSvc, srchSvc, web))
	r := gin.Default()
	// ClientIP (used by the rate limiter) reads X-Forwarded-For only from
	// proxies listed in TRUSTED_PROXIES, e.g. Caddy in production. Otherwise
	// it uses the TCP peer address, so clients cannot fake their IP.
	if err := r.SetTrustedProxies(trustedProxies()); err != nil {
		log.Fatalf("TRUSTED_PROXIES: %v", err)
	}
	limit := middleware.NewLimiter()
	r.GET("/healthz", func(c *gin.Context) { c.JSON(nethttp.StatusOK, gin.H{"status": "ok"}) })
	api := r.Group("/api")
	api.Use(middleware.OriginCheck())
	api.POST("/auth/register", limit.Middleware("register", 5, time.Hour), acc.Register)
	api.POST("/auth/login", limit.Middleware("login", 10, time.Minute), acc.Login)
	protected := api.Group("")
	protected.Use(middleware.RequireUser(accSvc))
	protected.GET("/me", acc.Me)
	protected.POST("/auth/logout", acc.Logout)
	protected.GET("/discover", sub.Discover)
	protected.GET("/folders", sub.ListFolders)
	protected.POST("/folders", sub.CreateFolder)
	protected.PATCH("/folders/:id", sub.RenameFolder)
	protected.DELETE("/folders/:id", sub.DeleteFolder)
	protected.GET("/subscriptions", sub.ListSubscriptions)
	protected.POST("/subscriptions", sub.CreateSubscription)
	protected.PATCH("/subscriptions/:id", sub.MoveSubscription)
	protected.DELETE("/subscriptions/:id", sub.DeleteSubscription)
	protected.POST("/subscriptions/:id/refresh", sub.RefreshSubscription)
	protected.GET("/entries", ent.ListEntries)
	protected.GET("/entries/unread-counts", ent.UnreadCounts)
	protected.GET("/entries/:id", ent.GetEntry)
	protected.PATCH("/entries/:id/state", ent.UpdateEntryState)
	protected.POST("/entries/mark-read", ent.MarkRead)
	protected.GET("/search", srch.Search)
	protected.GET("/chat", ch.Items)
	protected.POST("/chat/conversations", ch.NewConversation)
	protected.POST("/chat/messages", ch.Send)
	protected.POST("/opml/import", sub.ImportOPML)
	protected.GET("/opml/export", sub.ExportOPML)
	return r
}

func trustedProxies() []string {
	v := os.Getenv("TRUSTED_PROXIES")
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

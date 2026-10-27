package http

import (
	"log"
	nethttp "net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/account"
	"github.com/ushi02/go_feed/backend/internal/entry"
	"github.com/ushi02/go_feed/backend/internal/middleware"
	"github.com/ushi02/go_feed/backend/internal/subscription"
	"gorm.io/gorm"
)

func NewRouter(db *gorm.DB) *gin.Engine {
	// Wiring: repo (SQL) → service (rules) → handler (HTTP).
	entryRepo := entry.NewRepo(db)
	accSvc := account.NewService(account.NewRepo(db))
	acc := account.NewHandler(accSvc)
	sub := subscription.NewHandler(subscription.NewService(subscription.NewRepo(db), entryRepo))
	ent := entry.NewHandler(entry.NewService(entryRepo))
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

package http

import (
	nethttp "net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/account"
	"github.com/ushi02/go_feed/backend/internal/entry"
	"github.com/ushi02/go_feed/backend/internal/middleware"
	"github.com/ushi02/go_feed/backend/internal/subscription"
	"gorm.io/gorm"
)

func NewRouter(db *gorm.DB) *gin.Engine {
	acc := &account.Handler{DB: db}
	sub := &subscription.Handler{DB: db}
	ent := &entry.Handler{DB: db}
	r := gin.Default()
	_ = r.SetTrustedProxies(nil)
	limit := middleware.NewLimiter()
	r.GET("/healthz", func(c *gin.Context) { c.JSON(nethttp.StatusOK, gin.H{"status": "ok"}) })
	api := r.Group("/api")
	api.Use(middleware.OriginCheck())
	api.POST("/auth/register", limit.Middleware("register", 5, time.Hour), acc.Register)
	api.POST("/auth/login", limit.Middleware("login", 10, time.Minute), acc.Login)
	protected := api.Group("")
	protected.Use(middleware.RequireUser(db))
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
	protected.PATCH("/entries/:id/state", ent.UpdateEntryState)
	protected.POST("/entries/mark-read", ent.MarkRead)
	protected.POST("/opml/import", sub.ImportOPML)
	protected.GET("/opml/export", sub.ExportOPML)
	return r
}

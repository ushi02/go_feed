package reader

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/internal/feedfetch"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type API struct{ DB *gorm.DB }

const cookieName = "go_feed_session"

var errAlreadySubscribed = errors.New("already subscribed")

func NewRouter(db *gorm.DB) *gin.Engine {
	a := &API{DB: db}
	r := gin.Default()
	_ = r.SetTrustedProxies(nil)
	limit := newLimiter()
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	api := r.Group("/api")
	api.Use(originCheck())
	api.POST("/auth/register", limit.middleware("register", 5, time.Hour), a.register)
	api.POST("/auth/login", limit.middleware("login", 10, time.Minute), a.login)
	protected := api.Group("")
	protected.Use(a.requireUser)
	protected.GET("/me", a.me)
	protected.POST("/auth/logout", a.logout)
	protected.GET("/discover", a.discover)
	protected.GET("/folders", a.listFolders)
	protected.POST("/folders", a.createFolder)
	protected.PATCH("/folders/:id", a.renameFolder)
	protected.DELETE("/folders/:id", a.deleteFolder)
	protected.GET("/subscriptions", a.listSubscriptions)
	protected.POST("/subscriptions", a.createSubscription)
	protected.PATCH("/subscriptions/:id", a.moveSubscription)
	protected.DELETE("/subscriptions/:id", a.deleteSubscription)
	protected.POST("/subscriptions/:id/refresh", a.refreshSubscription)
	protected.GET("/entries", a.listEntries)
	protected.PATCH("/entries/:id/state", a.updateEntryState)
	protected.POST("/entries/mark-read", a.markRead)
	protected.POST("/opml/import", a.importOPML)
	protected.GET("/opml/export", a.exportOPML)
	return r
}

func originCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		origin := c.GetHeader("Origin")
		allowed := os.Getenv("APP_ORIGIN")
		if allowed == "" {
			allowed = "http://localhost:5173"
		}
		if origin != "" && origin != allowed || (origin == "" && c.Request.Header.Get("Cookie") != "") {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid Origin"})
			return
		}
		c.Next()
	}
}

func fail(c *gin.Context, status int, msg string) { c.JSON(status, gin.H{"error": msg}) }

func uid(c *gin.Context) uint64 { return c.MustGet("user_id").(uint64) }

func parseID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, 400, "invalid id")
		return 0, false
	}
	return id, true
}

func (a *API) register(c *gin.Context) {
	var req struct{ Username, Password string }
	if c.ShouldBindJSON(&req) != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Username) > 64 || len(req.Password) < 8 || len(req.Password) > 72 {
		fail(c, 400, "username must be 3-64 characters and password 8-72 characters")
		return
	}
	for _, r := range req.Username {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			fail(c, 400, "username may contain only letters, numbers, _ and -")
			return
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(c, 500, "password hashing failed")
		return
	}
	user := User{Username: req.Username, PasswordHash: string(hash)}
	if err := a.DB.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
		fail(c, 409, "username already exists")
		return
	}
	if err := a.newSession(c, user.ID); err != nil {
		fail(c, 500, "session creation failed")
		return
	}
	c.JSON(201, user)
}

func (a *API) login(c *gin.Context) {
	var req struct{ Username, Password string }
	if c.ShouldBindJSON(&req) != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	var user User
	err := a.DB.WithContext(c.Request.Context()).Where("username = ?", strings.TrimSpace(req.Username)).First(&user).Error
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		fail(c, 401, "invalid credentials")
		return
	}
	if err := a.newSession(c, user.ID); err != nil {
		fail(c, 500, "session creation failed")
		return
	}
	c.JSON(200, user)
}

func (a *API) newSession(c *gin.Context, userID uint64) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	token := hex.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	if err := a.DB.WithContext(c.Request.Context()).Create(&Session{TokenHash: hex.EncodeToString(h[:]), UserID: userID, ExpiresAt: expires}).Error; err != nil {
		return err
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cookieName, token, 30*24*3600, "/", "", os.Getenv("COOKIE_SECURE") == "1", true)
	return nil
}

func (a *API) requireUser(c *gin.Context) {
	token, err := c.Cookie(cookieName)
	if err != nil {
		c.AbortWithStatusJSON(401, gin.H{"error": "login required"})
		return
	}
	h := sha256.Sum256([]byte(token))
	var session Session
	err = a.DB.WithContext(c.Request.Context()).Where("token_hash = ? AND expires_at > ?", hex.EncodeToString(h[:]), time.Now().UTC()).First(&session).Error
	if err != nil {
		c.AbortWithStatusJSON(401, gin.H{"error": "session expired"})
		return
	}
	c.Set("user_id", session.UserID)
	c.Next()
}

func (a *API) me(c *gin.Context) {
	var user User
	if err := a.DB.WithContext(c.Request.Context()).First(&user, uid(c)).Error; err != nil {
		fail(c, 404, "user not found")
		return
	}
	c.JSON(200, user)
}

func (a *API) logout(c *gin.Context) {
	token, _ := c.Cookie(cookieName)
	h := sha256.Sum256([]byte(token))
	_ = a.DB.WithContext(c.Request.Context()).Where("token_hash = ?", hex.EncodeToString(h[:])).Delete(&Session{}).Error
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cookieName, "", -1, "/", "", os.Getenv("COOKIE_SECURE") == "1", true)
	c.Status(204)
}

func (a *API) discover(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	candidates, err := feedfetch.Discover(ctx, c.Query("url"))
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	c.JSON(200, gin.H{"candidates": candidates})
}

func (a *API) listFolders(c *gin.Context) {
	var folders []Folder
	if err := a.DB.WithContext(c.Request.Context()).Where("user_id = ?", uid(c)).Order("name").Find(&folders).Error; err != nil {
		fail(c, 500, "database error")
		return
	}
	c.JSON(200, gin.H{"folders": folders})
}

func (a *API) createFolder(c *gin.Context) {
	var req struct{ Name string }
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Name) == "" || len(req.Name) > 160 {
		fail(c, 400, "invalid folder name")
		return
	}
	f := Folder{UserID: uid(c), Name: strings.TrimSpace(req.Name)}
	if err := a.DB.WithContext(c.Request.Context()).Create(&f).Error; err != nil {
		fail(c, 409, "folder already exists")
		return
	}
	c.JSON(201, f)
}

func (a *API) renameFolder(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req struct{ Name string }
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Name) == "" || len(req.Name) > 160 {
		fail(c, 400, "invalid folder name")
		return
	}
	r := a.DB.WithContext(c.Request.Context()).Model(&Folder{}).Where("id = ? AND user_id = ?", id, uid(c)).Update("name", strings.TrimSpace(req.Name))
	if r.Error != nil {
		fail(c, 409, "folder already exists")
		return
	}
	if r.RowsAffected == 0 {
		fail(c, 404, "folder not found")
		return
	}
	c.Status(204)
}

func (a *API) deleteFolder(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	r := a.DB.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", id, uid(c)).Delete(&Folder{})
	if r.Error != nil {
		fail(c, 500, "database error")
		return
	}
	if r.RowsAffected == 0 {
		fail(c, 404, "folder not found")
		return
	}
	c.Status(204)
}

func (a *API) validFolder(ctx context.Context, userID uint64, folderID *uint64) bool {
	if folderID == nil {
		return true
	}
	var n int64
	return a.DB.WithContext(ctx).Model(&Folder{}).Where("id = ? AND user_id = ?", *folderID, userID).Count(&n).Error == nil && n == 1
}

func (a *API) listSubscriptions(c *gin.Context) {
	var subs []Subscription
	if err := a.DB.WithContext(c.Request.Context()).Preload("Feed").Where("user_id = ?", uid(c)).Order("id DESC").Find(&subs).Error; err != nil {
		fail(c, 500, "database error")
		return
	}
	c.JSON(200, gin.H{"subscriptions": subs})
}

func (a *API) createSubscription(c *gin.Context) {
	var req struct {
		URL      string  `json:"url"`
		FolderID *uint64 `json:"folder_id"`
	}
	if c.ShouldBindJSON(&req) != nil || !a.validFolder(c.Request.Context(), uid(c), req.FolderID) {
		fail(c, 400, "invalid subscription or folder")
		return
	}
	canonical, err := feedfetch.Canonical(req.URL)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	// Direct Feed URLs are resolved by the Worker. Discovery is a separate UI step.
	var sub Subscription
	err = a.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		feed := Feed{URL: canonical, URLHash: feedfetch.Hash(canonical), NextFetchAt: time.Now().UTC()}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&feed).Error; err != nil {
			return err
		}
		if err := tx.Where("url_hash = ?", feed.URLHash).First(&feed).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&Subscription{}).Where("user_id = ? AND feed_id = ?", uid(c), feed.ID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errAlreadySubscribed
		}
		var subscriberCount int64
		if err := tx.Model(&Subscription{}).Where("feed_id = ?", feed.ID).Count(&subscriberCount).Error; err != nil {
			return err
		}
		sub = Subscription{UserID: uid(c), FeedID: feed.ID, FolderID: req.FolderID, PendingBaseline: !feed.Initialized}
		if feed.Initialized {
			baseline, err := baselineID(tx, feed.ID)
			if err != nil {
				return err
			}
			sub.ReadThroughEntryID = baseline
		}
		if err := tx.Create(&sub).Error; err != nil {
			return err
		}
		if subscriberCount == 0 {
			return tx.Model(&Feed{}).Where("id = ?", feed.ID).Update("next_fetch_at", time.Now().UTC()).Error
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errAlreadySubscribed) {
			fail(c, 409, "already subscribed")
		} else {
			fail(c, 500, "database error")
		}
		return
	}
	c.JSON(201, sub)
}

func (a *API) moveSubscription(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req struct {
		FolderID *uint64 `json:"folder_id"`
	}
	if c.ShouldBindJSON(&req) != nil || !a.validFolder(c.Request.Context(), uid(c), req.FolderID) {
		fail(c, 400, "invalid folder")
		return
	}
	r := a.DB.WithContext(c.Request.Context()).Model(&Subscription{}).Where("id = ? AND user_id = ?", id, uid(c)).Update("folder_id", req.FolderID)
	if r.Error != nil {
		fail(c, 500, "database error")
		return
	}
	if r.RowsAffected == 0 {
		fail(c, 404, "subscription not found")
		return
	}
	c.Status(204)
}

func (a *API) deleteSubscription(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	err := a.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var sub Subscription
		if err := tx.Where("id = ? AND user_id = ?", id, uid(c)).First(&sub).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE st FROM entry_states AS st JOIN entries AS e ON e.id = st.entry_id WHERE st.user_id = ? AND e.feed_id = ?", uid(c), sub.FeedID).Error; err != nil {
			return err
		}
		return tx.Delete(&sub).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fail(c, 404, "subscription not found")
		return
	}
	if err != nil {
		fail(c, 500, "database error")
		return
	}
	c.Status(204)
}

func (a *API) refreshSubscription(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var sub Subscription
	if err := a.DB.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", id, uid(c)).First(&sub).Error; err != nil {
		fail(c, 404, "subscription not found")
		return
	}
	// Global per-source cooldown: one user's click cannot make every subscriber fetch repeatedly.
	r := a.DB.WithContext(c.Request.Context()).Model(&Feed{}).Where("id = ? AND (last_fetched_at IS NULL OR last_fetched_at < ?)", sub.FeedID, time.Now().UTC().Add(-5*time.Minute)).Update("next_fetch_at", time.Now().UTC())
	if r.Error != nil {
		fail(c, 500, "database error")
		return
	}
	if r.RowsAffected == 0 {
		fail(c, 429, "feed was refreshed recently")
		return
	}
	c.Status(202)
}

func validHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https")
}

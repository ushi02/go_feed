package subscription

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/httpx"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Discover(c *gin.Context) {
	candidates, err := h.svc.Discover(c.Request.Context(), c.Query("url"))
	if err != nil {
		httpx.Fail(c, 400, err.Error())
		return
	}
	c.JSON(200, gin.H{"candidates": candidates})
}

func (h *Handler) ListFolders(c *gin.Context) {
	folders, err := h.svc.ListFolders(c.Request.Context(), httpx.UserID(c))
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.JSON(200, gin.H{"folders": folders})
}

func folderName(c *gin.Context) (string, bool) {
	var req struct{ Name string }
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Name) == "" || len(req.Name) > 160 {
		httpx.Fail(c, 400, "invalid folder name")
		return "", false
	}
	return strings.TrimSpace(req.Name), true
}

func (h *Handler) CreateFolder(c *gin.Context) {
	name, ok := folderName(c)
	if !ok {
		return
	}
	f, err := h.svc.CreateFolder(c.Request.Context(), httpx.UserID(c), name)
	if err != nil {
		httpx.Fail(c, 409, "folder already exists")
		return
	}
	c.JSON(201, f)
}

func (h *Handler) RenameFolder(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	name, ok := folderName(c)
	if !ok {
		return
	}
	err := h.svc.RenameFolder(c.Request.Context(), httpx.UserID(c), id, name)
	if errors.Is(err, ErrNotFound) {
		httpx.Fail(c, 404, "folder not found")
		return
	}
	if err != nil {
		httpx.Fail(c, 409, "folder already exists")
		return
	}
	c.Status(204)
}

func (h *Handler) DeleteFolder(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	err := h.svc.DeleteFolder(c.Request.Context(), httpx.UserID(c), id)
	if errors.Is(err, ErrNotFound) {
		httpx.Fail(c, 404, "folder not found")
		return
	}
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.Status(204)
}

func (h *Handler) ListSubscriptions(c *gin.Context) {
	subs, err := h.svc.ListSubscriptions(c.Request.Context(), httpx.UserID(c))
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.JSON(200, gin.H{"subscriptions": subs})
}

func (h *Handler) CreateSubscription(c *gin.Context) {
	var req struct {
		URL      string  `json:"url"`
		FolderID *uint64 `json:"folder_id"`
	}
	if c.ShouldBindJSON(&req) != nil {
		httpx.Fail(c, 400, "invalid subscription or folder")
		return
	}
	// Direct Feed URLs are resolved by the Worker. Discovery is a separate UI step.
	sub, err := h.svc.Subscribe(c.Request.Context(), httpx.UserID(c), req.URL, req.FolderID)
	var invalidURL InvalidURLError
	switch {
	case err == nil:
		c.JSON(201, sub)
	case errors.Is(err, ErrInvalidFolder):
		httpx.Fail(c, 400, "invalid subscription or folder")
	case errors.As(err, &invalidURL):
		httpx.Fail(c, 400, invalidURL.Error())
	case errors.Is(err, ErrAlreadySubscribed):
		httpx.Fail(c, 409, "already subscribed")
	default:
		httpx.Fail(c, 500, "database error")
	}
}

func (h *Handler) MoveSubscription(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	var req struct {
		FolderID *uint64 `json:"folder_id"`
	}
	if c.ShouldBindJSON(&req) != nil {
		httpx.Fail(c, 400, "invalid folder")
		return
	}
	err := h.svc.Move(c.Request.Context(), httpx.UserID(c), id, req.FolderID)
	switch {
	case err == nil:
		c.Status(204)
	case errors.Is(err, ErrInvalidFolder):
		httpx.Fail(c, 400, "invalid folder")
	case errors.Is(err, ErrNotFound):
		httpx.Fail(c, 404, "subscription not found")
	default:
		httpx.Fail(c, 500, "database error")
	}
}

func (h *Handler) DeleteSubscription(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	err := h.svc.Unsubscribe(c.Request.Context(), httpx.UserID(c), id)
	if errors.Is(err, ErrNotFound) {
		httpx.Fail(c, 404, "subscription not found")
		return
	}
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.Status(204)
}

func (h *Handler) RefreshSubscription(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	err := h.svc.Refresh(c.Request.Context(), httpx.UserID(c), id)
	switch {
	case err == nil:
		c.Status(202)
	case errors.Is(err, ErrNotFound):
		httpx.Fail(c, 404, "subscription not found")
	case errors.Is(err, ErrRefreshedRecently):
		httpx.Fail(c, 429, "feed was refreshed recently")
	default:
		httpx.Fail(c, 500, "database error")
	}
}

package entry

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/httpx"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func encodeCursor(t time.Time, id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + ":" + strconv.FormatUint(id, 10)))
}

func decodeCursor(raw string) (time.Time, uint64, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, 0, err
	}
	s := string(b)
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return time.Time{}, 0, fmt.Errorf("invalid cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, s[:i])
	if err != nil {
		return time.Time{}, 0, err
	}
	id, err := strconv.ParseUint(s[i+1:], 10, 64)
	return t, id, err
}

func (h *Handler) ListEntries(c *gin.Context) {
	q := ListQuery{UserID: httpx.UserID(c), Limit: 20, Filter: c.Query("filter")}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			httpx.Fail(c, 400, "limit must be 1-50")
			return
		}
		q.Limit = n
	}
	if folder := c.Query("folder_id"); folder != "" {
		id, err := strconv.ParseUint(folder, 10, 64)
		if err != nil {
			httpx.Fail(c, 400, "invalid folder_id")
			return
		}
		q.FolderID = &id
	}
	if sub := c.Query("subscription_id"); sub != "" {
		id, err := strconv.ParseUint(sub, 10, 64)
		if err != nil {
			httpx.Fail(c, 400, "invalid subscription_id")
			return
		}
		q.SubscriptionID = &id
	}
	switch q.Filter {
	case "", "all", "unread", "starred":
	default:
		httpx.Fail(c, 400, "invalid filter")
		return
	}
	if raw := c.Query("cursor"); raw != "" {
		t, id, err := decodeCursor(raw)
		if err != nil {
			httpx.Fail(c, 400, "invalid cursor")
			return
		}
		q.CursorTime, q.CursorID = &t, id
	}
	entries, more, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	next := ""
	if more {
		last := entries[len(entries)-1]
		next = encodeCursor(last.SortAt, last.ID)
	}
	c.JSON(200, gin.H{"entries": entries, "next_cursor": next})
}

func (h *Handler) GetEntry(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	entry, err := h.svc.Get(c.Request.Context(), httpx.UserID(c), id)
	if errors.Is(err, ErrNotFound) {
		httpx.Fail(c, 404, "entry not found")
		return
	}
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.JSON(200, entry)
}

func (h *Handler) UnreadCounts(c *gin.Context) {
	counts, err := h.svc.UnreadCounts(c.Request.Context(), httpx.UserID(c))
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.JSON(200, gin.H{"counts": counts})
}

func (h *Handler) UpdateEntryState(c *gin.Context) {
	id, ok := httpx.ParseID(c)
	if !ok {
		return
	}
	var req struct {
		Read    *bool `json:"read"`
		Starred *bool `json:"starred"`
	}
	if c.ShouldBindJSON(&req) != nil || (req.Read == nil && req.Starred == nil) {
		httpx.Fail(c, 400, "read or starred is required")
		return
	}
	err := h.svc.UpdateState(c.Request.Context(), httpx.UserID(c), id, req.Read, req.Starred)
	if errors.Is(err, ErrNotFound) {
		httpx.Fail(c, 404, "entry not found in subscriptions")
		return
	}
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) MarkRead(c *gin.Context) {
	var req struct {
		SubscriptionID *uint64 `json:"subscription_id"`
		FolderID       *uint64 `json:"folder_id"`
	}
	if c.ShouldBindJSON(&req) != nil || (req.SubscriptionID != nil && req.FolderID != nil) {
		httpx.Fail(c, 400, "choose one scope")
		return
	}
	err := h.svc.MarkRead(c.Request.Context(), httpx.UserID(c), req.SubscriptionID, req.FolderID)
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

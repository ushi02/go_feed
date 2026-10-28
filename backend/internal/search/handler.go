package search

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/httpx"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Search(c *gin.Context) {
	results, err := h.svc.Search(c.Request.Context(), httpx.UserID(c), c.Query("q"))
	switch {
	case err == nil:
		c.JSON(200, gin.H{"results": results})
	case errors.Is(err, ErrEmptyQuery):
		httpx.Fail(c, 400, "search query is empty")
	default:
		httpx.Fail(c, 500, "database error")
	}
}

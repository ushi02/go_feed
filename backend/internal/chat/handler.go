package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/httpx"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Items(c *gin.Context) {
	items, err := h.svc.Items(c.Request.Context(), httpx.UserID(c))
	if err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.JSON(200, gin.H{"items": items})
}

func (h *Handler) NewConversation(c *gin.Context) {
	if err := h.svc.NewConversation(c.Request.Context(), httpx.UserID(c)); err != nil {
		httpx.Fail(c, 500, "database error")
		return
	}
	c.Status(204)
}

// Send answers with server-sent events: "text" pieces, a "break" after each
// part of the answer, and "card"s while the reply is made, then "done", or
// "error" if it broke off. An error found
// before anything was sent is a normal JSON error response instead.
func (h *Handler) Send(c *gin.Context) {
	var req struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, 400, "invalid request")
		return
	}
	started := false
	send := func(event string, data any) {
		if !started {
			started = true
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("X-Accel-Buffering", "no") // ask proxies not to hold the stream back
			c.Status(200)
		}
		b, _ := json.Marshal(data)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, b)
		c.Writer.Flush()
	}
	err := h.svc.Send(c.Request.Context(), httpx.UserID(c), req.Text, Events{
		Text:  func(piece string) { send("text", gin.H{"text": piece}) },
		Break: func() { send("break", gin.H{}) },
		Card:  func(card Card) { send("card", card) },
	})
	switch {
	case err == nil:
		send("done", gin.H{})
	case c.Request.Context().Err() != nil:
		// The page was closed; there is nobody to tell.
	case errors.Is(err, ErrInvalidMessage):
		httpx.FailCode(c, 400, "message_invalid", "message must be 1-1000 characters")
	case errors.Is(err, ErrDailyLimit):
		httpx.FailCode(c, 429, "chat_quota", "at most 50 messages per 24 hours")
	case errors.Is(err, ErrNotConfigured):
		httpx.FailCode(c, 503, "chat_unavailable", "chat is not configured (DEEPSEEK_API_KEY is missing)")
	default:
		log.Printf("chat: user %d: %v", httpx.UserID(c), err)
		if started {
			send("error", gin.H{"error": "the reply broke off", "code": "chat_interrupted"})
		} else {
			httpx.FailCode(c, 502, "chat_upstream", "the model could not be reached")
		}
	}
}

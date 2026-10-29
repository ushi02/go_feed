package account

import (
	"errors"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/ushi02/go_feed/backend/internal/httpx"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

const CookieName = "go_feed_session"

func setSessionCookie(c *gin.Context, token string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, token, maxAge, "/", "", os.Getenv("COOKIE_SECURE") == "1", true)
}

func (h *Handler) Register(c *gin.Context) {
	var req struct{ Username, Password string }
	if c.ShouldBindJSON(&req) != nil {
		httpx.Fail(c, 400, "invalid JSON")
		return
	}
	user, token, err := h.svc.Register(c.Request.Context(), req.Username, req.Password)
	switch {
	case errors.Is(err, ErrInvalidLength), errors.Is(err, ErrInvalidUsername):
		httpx.Fail(c, 400, err.Error())
	case errors.Is(err, ErrUsernameTaken):
		httpx.Fail(c, 409, err.Error())
	case err != nil:
		httpx.Fail(c, 500, "registration failed")
	default:
		setSessionCookie(c, token, int(SessionTTL.Seconds()))
		c.JSON(201, user)
	}
}

func (h *Handler) Login(c *gin.Context) {
	var req struct{ Username, Password string }
	if c.ShouldBindJSON(&req) != nil {
		httpx.Fail(c, 400, "invalid JSON")
		return
	}
	user, token, err := h.svc.Login(c.Request.Context(), req.Username, req.Password)
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		httpx.Fail(c, 401, err.Error())
	case err != nil:
		httpx.Fail(c, 500, "session creation failed")
	default:
		setSessionCookie(c, token, int(SessionTTL.Seconds()))
		c.JSON(200, user)
	}
}

func (h *Handler) Me(c *gin.Context) {
	user, err := h.svc.Me(c.Request.Context(), httpx.UserID(c))
	if err != nil {
		httpx.Fail(c, 404, "user not found")
		return
	}
	c.JSON(200, user)
}

func (h *Handler) Logout(c *gin.Context) {
	token, _ := c.Cookie(CookieName)
	_ = h.svc.Logout(c.Request.Context(), token)
	setSessionCookie(c, "", -1)
	c.Status(204)
}

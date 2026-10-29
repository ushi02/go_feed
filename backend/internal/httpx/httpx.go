// Package httpx holds small helpers shared by every handler package.
package httpx

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

const userIDKey = "user_id"

func Fail(c *gin.Context, status int, msg string) { c.JSON(status, gin.H{"error": msg}) }

func SetUserID(c *gin.Context, id uint64) { c.Set(userIDKey, id) }

func UserID(c *gin.Context) uint64 { return c.MustGet(userIDKey).(uint64) }

func ParseID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		Fail(c, 400, "invalid id")
		return 0, false
	}
	return id, true
}

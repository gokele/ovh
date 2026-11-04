package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// LimitRequestBody also covers public API routes such as device pairing.
func LimitRequestBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") && c.Request.Body != nil {
			if c.Request.ContentLength > maxBytes {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "error": "请求体过大,请减少提交内容", "code": "REQUEST_TOO_LARGE"})
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
)

// Version 当前二进制版本。build.ps1 用 -ldflags "-X github.com/ovh-buy/server/internal/handlers.Version=x.y.z" 注入。
// 默认 "dev" 给 go run / 未注入的 build。
var Version = "dev"

// GetVersion GET /api/version  无需鉴权,前端启动时拿来显示
func GetVersion(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"version": Version,
		})
	}
}

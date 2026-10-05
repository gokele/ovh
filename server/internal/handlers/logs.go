package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
)

// GetLogs GET /api/logs
func GetLogs(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.Logger.Flush()
		c.JSON(http.StatusOK, state.Logger.Snapshot())
	}
}

// FlushLogs POST /api/logs/flush
func FlushLogs(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.Logger.Flush()
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "日志已刷新", "code": "ECEA0FB83"})
	}
}

// ClearLogs DELETE /api/logs
func ClearLogs(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := state.Logger.Clear(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		state.Logger.Info("Logs cleared", "system")
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}

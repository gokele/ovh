package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxServiceNameLen OVH 的服务名最长也就几十个字符
// (ns3001234.ip-1-2-3.eu / vps-abc12345.vps.ovh.net),给足余量。
const maxServiceNameLen = 255

// ValidateServiceName 挡住会把 OVH 请求打歪的服务名。
//
// 这些 handler 一律是 "/dedicated/server/" + svc + "/reboot" 这样拼路径的
// (两百多处,不可能逐个改成转义)。而 svc 里只要有 # 或 ?,
// Go 的 url.Parse 会把它后面的部分当成片段/查询串截掉 —— 实测:
//
//	svc = "a#b"  → 实际请求 POST /1.0/dedicated/server/a   (片段 "b/reboot")
//	svc = "a?b"  → 实际请求 POST /1.0/dedicated/server/a   (查询 "b/reboot")
//
// 也就是说**请求被静默发到了另一个端点**,而不是报错。正常走界面选服务器
// 不会产生这种值(名字都来自 OVH 自己的列表),但这一层是只此一处就能堵死的,
// 而漏掉的代价是一次打到非预期端点的写操作。
//
// 放在路由组上,组里每个 :service_name 都自动受保护,不用改任何 handler。
func ValidateServiceName() gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if svc == "" { // 组里也有 /list、/aliases 这类不带参数的路由
			c.Next()
			return
		}
		if len(svc) > maxServiceNameLen {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "服务名过长,不像是一个有效的 OVH 服务名",
			})
			return
		}
		for _, r := range svc {
			ok := r == '.' || r == '-' || r == '_' ||
				(r >= '0' && r <= '9') ||
				(r >= 'a' && r <= 'z') ||
				(r >= 'A' && r <= 'Z')
			if !ok {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error": "服务名里有非法字符。OVH 的服务名只会包含字母、数字、点、连字符和下划线" +
						"(比如 ns3001234.ip-1-2-3.eu);请从列表里重新选一台机器",
				})
				return
			}
		}
		c.Next()
	}
}

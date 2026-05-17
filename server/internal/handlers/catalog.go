package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
)

// catalogTTL OVH 公开 catalog 缓存时长，与前端 useOvhCatalog 的 staleTime 对齐
const catalogTTL = 2 * time.Hour

// catalogBaseURLForSubsidiary 把 subsidiary 映射成对应站点的 base URL。
// 同一个 catalog 只能从对应站点查，跨站点 404。
func catalogBaseURLForSubsidiary(sub string) string {
	switch strings.ToUpper(sub) {
	case "US":
		return "https://api.us.ovhcloud.com"
	case "CA", "QC", "ASIA", "SG", "AU", "IN":
		return "https://ca.api.ovh.com"
	default:
		return "https://eu.api.ovh.com"
	}
}

// GetCatalog GET /api/catalog?subsidiary=IE[&forceRefresh=true]
// 返回 OVH 公开 eco catalog 的原始 JSON。优先走 SQLite 缓存（2 小时 TTL），
// 缓存过期或带 forceRefresh=true 时才直连 OVH。
func GetCatalog(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		sub := strings.ToUpper(strings.TrimSpace(c.Query("subsidiary")))
		if sub == "" {
			sub = state.Config.Get().Zone
			if sub == "" {
				sub = "IE"
			}
		}
		force := strings.EqualFold(c.Query("forceRefresh"), "true")

		// 1. 命中 SQLite 缓存且未过期 → 直接返回
		if !force {
			raw, ts, ok, err := state.DB.GetCatalog(sub)
			if err == nil && ok {
				age := time.Since(time.UnixMilli(ts))
				if age < catalogTTL {
					c.Header("X-Cache-Age-Seconds", strconv.FormatInt(int64(age.Seconds()), 10))
					c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(raw))
					return
				}
			}
		}

		// 2. 直连 OVH 拉新数据
		baseURL := catalogBaseURLForSubsidiary(sub)
		url := fmt.Sprintf("%s/v1/order/catalog/public/eco?ovhSubsidiary=%s", baseURL, sub)
		client := &http.Client{Timeout: 30 * time.Second}
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			// 拉失败时回退到 stale 缓存（如果有），比直接给 500 强
			if raw, _, ok, _ := state.DB.GetCatalog(sub); ok {
				c.Header("X-Cache-Warning", "stale (upstream fetch failed)")
				c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(raw))
				return
			}
			state.Logger.Error("catalog 拉取失败 "+sub+": "+err.Error(), "catalog")
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			state.Logger.Error("catalog 读取响应失败 "+sub+": "+err.Error(), "catalog")
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		if resp.StatusCode != http.StatusOK {
			state.Logger.Error(fmt.Sprintf("catalog 上游 HTTP %d: %s", resp.StatusCode, sub), "catalog")
			c.JSON(resp.StatusCode, gin.H{"error": fmt.Sprintf("upstream returned %d", resp.StatusCode)})
			return
		}

		// 3. 落 SQLite + 回写响应
		if err := state.DB.UpsertCatalog(sub, string(body)); err != nil {
			state.Logger.Warn("catalog 写库失败 "+sub+": "+err.Error(), "catalog")
		} else {
			state.Logger.Info(fmt.Sprintf("catalog %s 已缓存 (%d KB)", sub, len(body)/1024), "catalog")
		}
		c.Header("X-Cache-Age-Seconds", "0")
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
	}
}

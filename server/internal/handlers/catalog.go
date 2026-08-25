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
	"github.com/ovh-buy/server/internal/ovh"
)

// catalogTTL OVH 公开 catalog 缓存时长，与前端 useOvhCatalog 的 staleTime 对齐
const catalogTTL = 2 * time.Hour

// catalogBaseURLForSubsidiary 把 subsidiary 映射成对应站点的 base URL。
// 实现收敛在 ovh 包一份(catalog 包解析 region 时也要用同一张表),
// 两份各自演化过一次就会出现"某个子公司只在一处被支持"的诡异 bug。
func catalogBaseURLForSubsidiary(sub string) string {
	return ovh.CatalogBaseURLForSubsidiary(sub)
}

// GetCatalog GET /api/catalog?subsidiary=IE[&forceRefresh=true]
// 返回 OVH 公开 eco catalog 的原始 JSON。优先走 SQLite 缓存（2 小时 TTL），
// 缓存过期或带 forceRefresh=true 时才直连 OVH。
func GetCatalog(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		sub := strings.ToUpper(strings.TrimSpace(c.Query("subsidiary")))
		if sub == "" {
			// 多账户:落到默认账户的 zone,不读 state.Config
			acc, _ := state.FindAccount("")
			sub = strings.ToUpper(strings.TrimSpace(acc.Zone))
			if sub == "" {
				sub = ovh.DefaultSubsidiaryForEndpoint(acc.Endpoint)
			}
		}
		// 子公司必须是三区归属表里的取值:不认识的值会被 CatalogBaseURLForSubsidiary
		// 兜底打到 EU 站点,OVH 回一句 400 invalid ovhSubsidiary,前端只能看到
		// "upstream returned 400" —— 完全看不出是子公司写错了。
		if !ovh.KnownSubsidiary(sub) {
			state.Logger.Warn("catalog 请求了未知子公司: "+sub, "catalog")
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "未知的 ovhSubsidiary: " + sub,
				"message": "OVH 的 EU / US / CA 是三套独立站点，各自只认自己的子公司：" +
					"EU=CZ DE ES EU FI FR GB IE IT LT MA NL PL PT SN TN；CA=ASIA AU CA IN QC SG WE WS；US=US",
			})
			return
		}
		region := ovh.SubsidiaryRegion(sub)
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

		// 2. 直连 OVH 拉新数据。站点由子公司决定(三区目录互不相通,查错站点是 400 而不是空目录)
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
			state.Logger.Error(fmt.Sprintf("catalog 上游 HTTP %d: 子公司 %s / %s 站点(%s)", resp.StatusCode, sub, region, baseURL), "catalog")
			// 上游报错时同样先退回 stale 缓存:目录一天也变不了几次,
			// 给一份两小时前的目录也比让整个浏览页空掉强。
			if raw, _, ok, _ := state.DB.GetCatalog(sub); ok {
				c.Header("X-Cache-Warning", "stale (upstream returned "+strconv.Itoa(resp.StatusCode)+")")
				c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(raw))
				return
			}
			c.JSON(http.StatusBadGateway, gin.H{
				"error": fmt.Sprintf("upstream returned %d", resp.StatusCode),
				"message": fmt.Sprintf("%s 站点没有返回子公司 %s 的目录（HTTP %d）：%s",
					region, sub, resp.StatusCode, strings.TrimSpace(string(body))),
			})
			return
		}

		// 3. 落 SQLite + 回写响应
		if err := state.DB.UpsertCatalog(sub, string(body)); err != nil {
			state.Logger.Warn("catalog 写库失败 "+sub+": "+err.Error(), "catalog")
		} else {
			state.Logger.Info(fmt.Sprintf("catalog %s(%s 站点) 已缓存 (%d KB)", sub, region, len(body)/1024), "catalog")
		}
		c.Header("X-Cache-Age-Seconds", "0")
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
	}
}

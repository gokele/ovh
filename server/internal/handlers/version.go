package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// updateRepo GitHub 上游仓库。换源时改这里就行。
const (
	updateRepo      = "gokele/ovh"
	updateUserAgent = "OVH-Console-UpdateChecker"
)

// githubRelease 只挑要用的字段反序列化
type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
}

// parseSemver 把 "v0.0.2" / "0.1.10" 切成 [0 0 2] / [0 1 10],
// 解析失败的段当 0,简单稳定的字典序比较够用
func parseSemver(s string) [3]int {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.SplitN(s, ".", 3)
	var v [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		v[i] = n
	}
	return v
}

// semverGreater latest > current ?
func semverGreater(latest, current string) bool {
	l := parseSemver(latest)
	c := parseSemver(current)
	for i := 0; i < 3; i++ {
		if l[i] > c[i] {
			return true
		}
		if l[i] < c[i] {
			return false
		}
	}
	return false
}

// CheckUpdate GET /api/version/check-update
// 收到请求 → 直连 GitHub 拉 gokele/ovh 最新 release,跟本地 Version 比一下。
// 后端不缓存、不定时跑、不存任何状态,纯粹被动响应:每次访问触发一次拉取。
// 频率控制全交给前端 React Query 的 staleTime(同一会话 1h 内不重复请求)。
// dev 版本(未注入 ldflags)也返回 latest 信息,但 hasUpdate 始终 false,避免开发时刷屏。
func CheckUpdate(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		url := "https://api.github.com/repos/" + updateRepo + "/releases/latest"
		client := &http.Client{Timeout: 15 * time.Second}
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", updateUserAgent)

		resp, err := client.Do(req)
		if err != nil {
			state.Logger.Warn("update check 拉取失败: "+err.Error(), "version")
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "current": Version})
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "current": Version})
			return
		}
		if resp.StatusCode != http.StatusOK {
			c.JSON(resp.StatusCode, gin.H{
				"error":   "upstream returned " + strconv.Itoa(resp.StatusCode),
				"detail":  strings.TrimSpace(string(body)),
				"current": Version,
			})
			return
		}

		var rel githubRelease
		if err := json.Unmarshal(body, &rel); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "current": Version})
			return
		}

		latest := strings.TrimPrefix(rel.TagName, "v")
		hasUpdate := Version != "dev" && semverGreater(latest, Version)

		c.JSON(http.StatusOK, gin.H{
			"current":     Version,
			"latest":      latest,
			"tag":         rel.TagName,
			"name":        rel.Name,
			"hasUpdate":   hasUpdate,
			"url":         rel.HTMLURL,
			"publishedAt": rel.PublishedAt,
			"body":        rel.Body,
			"prerelease":  rel.Prerelease,
			"checkedAt":   time.Now().UTC().Format(time.RFC3339),
		})
	}
}

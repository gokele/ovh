package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/updater"
)

// 更新是全局单例操作:同一时刻只允许一个在跑。
// 进度放内存就够 —— 更新成功的终点就是进程被换掉,没有"跨重启保留进度"这回事。
var (
	updateMu       sync.Mutex
	updateProgress = updater.Progress{Phase: "idle"}
)

func setProgress(p updater.Progress) {
	updateMu.Lock()
	updateProgress = p
	updateMu.Unlock()
}

func getProgress() updater.Progress {
	updateMu.Lock()
	defer updateMu.Unlock()
	return updateProgress
}

// updateRunning 是否有更新正在进行
func updateRunning() bool {
	switch getProgress().Phase {
	case "downloading", "verifying", "installing", "restarting":
		return true
	}
	return false
}

// GetUpdateStatus GET /api/version/update/status
// 前端点了更新之后轮询这个看进度。重启阶段这个接口会连不上 —— 那正是重启成功的信号。
func GetUpdateStatus(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, getProgress())
	}
}

// SelfUpdate POST /api/version/update
//
// 全自动:下载 → 校验 SHA256 → 替换正在运行的二进制 → 优雅关服 → 用新版本重启。
// 立刻返回,真正的活在后台 goroutine 里干,前端轮询 /api/version/update/status 看进度。
//
// restart 由 main 注入:它负责关掉 HTTP 监听和 SQLite,再调 updater.Restart。
// 这两件事必须在 exec 之前做完 —— 否则新进程会撞上"端口已被占用",
// 而 SQLite 不干净关闭会留下 -wal 文件。
func SelfUpdate(state *app.State, restart func(exePath string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if Version == "dev" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "当前是开发构建(version=dev),没有可比较的版本号,不支持自更新。请用 Release 里的二进制",
			})
			return
		}
		if updateRunning() {
			c.JSON(http.StatusConflict, gin.H{"success": false, "error": "已有更新正在进行", "progress": getProgress()})
			return
		}

		rel, err := updater.FetchLatest()
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err.Error()})
			return
		}
		latest := trimV(rel.TagName)
		if !semverGreater(latest, Version) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"error":   "当前已是最新版本 " + Version,
				"latest":  latest,
			})
			return
		}

		setProgress(updater.Progress{Phase: "downloading", Message: "正在下载 v" + latest, Version: latest})
		state.Logger.Info("[更新] 开始自更新: "+Version+" → "+latest, "version")

		go func() {
			tmp, exe, err := updater.Prepare(rel, func(pct int) {
				setProgress(updater.Progress{
					Phase: "downloading", Percent: pct, Version: latest,
					Message: "正在下载 v" + latest,
				})
			})
			if err != nil {
				state.Logger.Error("[更新] 失败: "+err.Error(), "version")
				setProgress(updater.Progress{Phase: "failed", Version: latest, Error: err.Error(), Message: "更新失败"})
				return
			}
			// 下载函数内部已经逐块算过 SHA256 并比对,走到这里就是校验通过
			setProgress(updater.Progress{Phase: "installing", Percent: 100, Version: latest, Message: "校验通过,正在替换程序"})

			if err := updater.Install(tmp, exe); err != nil {
				state.Logger.Error("[更新] 替换失败: "+err.Error(), "version")
				setProgress(updater.Progress{Phase: "failed", Version: latest, Error: err.Error(), Message: "替换程序失败"})
				return
			}
			// 写下"待验证"标记:新进程活到对外服务那一刻会调 MarkHealthy 删掉它;
			// 没删掉说明新版本没起来,下次启动时 RollbackIfStale 会换回旧版本
			updater.MarkPending()
			state.Logger.Info("[更新] 已替换为 v"+latest+",正在重启", "version")
			setProgress(updater.Progress{Phase: "restarting", Percent: 100, Version: latest, Message: "已更新到 v" + latest + ",正在重启"})

			// 给前端一点时间把 restarting 这个状态轮询走,不然它只会看到连接中断
			time.Sleep(1500 * time.Millisecond)
			restart(exe)
		}()

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "已开始更新到 v" + latest + ",完成后会自动重启",
			"current": Version,
			"latest":  latest,
		})
	}
}

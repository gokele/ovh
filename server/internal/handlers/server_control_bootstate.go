package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
)

// 当前启动状态(只读):救援模式还是正常(硬盘)启动。
//
// OVH 没有"这台机器此刻实际跑的是哪个系统"的接口。能拿到的是:
//   - GET /dedicated/server/{sn} 的 bootId:**下次启动**用的启动项;
//   - GET /dedicated/server/{sn}/boot/{bootId} 的 bootType:harddisk / rescue / network / ...;
//   - GET /dedicated/server/{sn}/task?function=hardReboot:重启任务及其完成时间。
// 本应用改启动项时记下时间(kv boot-change:{账户}:{服务器});之后有一次重启完成,才能确认设置已生效。
// 在别处(OVH 控制台)改的启动项没有记录:正常启动按已生效处理(默认状态),救援启动标为未确认。

type bootChangeRecord struct {
	BootID int64  `json:"bootId"`
	At     string `json:"at"`
}

func bootChangeKey(accountID, svc string) string { return "boot-change:" + accountID + ":" + svc }

// recordBootChange 在成功 PUT bootId 之后调用;记录失败只影响"是否已生效"的判断,不影响操作本身。
func recordBootChange(state *app.State, c *gin.Context, svc string, bootID int64) {
	acc, ok := ovhAccountFor(state, c)
	if !ok || state.DB == nil {
		return
	}
	if err := state.DB.SetKV(bootChangeKey(acc.ID, svc), bootChangeRecord{BootID: bootID, At: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		state.Logger.Warn("记录启动项变更时间失败: "+err.Error(), "server_control")
	}
}

// GetBootState GET /api/server-control/:service_name/boot-state
func GetBootState(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		acc, ok := ovhAccountFor(state, c)
		if !ok {
			noOVHResp(c)
			return
		}
		client, err := state.OVH.IsolatedClientForAccount(acc)
		if err != nil {
			noOVHResp(c)
			return
		}
		ctx := c.Request.Context()
		var server struct {
			BootID     int64  `json:"bootId"`
			PowerState string `json:"powerState"`
			State      string `json:"state"`
		}
		if err := client.GetWithContext(ctx, "/dedicated/server/"+svc, &server); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": ovh.Explain(err)})
			return
		}
		var boot struct {
			BootType    string `json:"bootType"`
			Description string `json:"description"`
		}
		if server.BootID > 0 {
			if err := client.GetWithContext(ctx, fmt.Sprintf("/dedicated/server/%s/boot/%d", svc, server.BootID), &boot); err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": ovh.Explain(err)})
				return
			}
		}

		// 最近一次重启任务(id 最大的那个)
		var reboot map[string]interface{}
		var taskIDs []int64
		if err := client.GetWithContext(ctx, "/dedicated/server/"+svc+"/task?function=hardReboot", &taskIDs); err == nil && len(taskIDs) > 0 {
			latest := taskIDs[0]
			for _, id := range taskIDs {
				if id > latest {
					latest = id
				}
			}
			var task map[string]interface{}
			if client.GetWithContext(ctx, fmt.Sprintf("/dedicated/server/%s/task/%d", svc, latest), &task) == nil {
				reboot = task
			}
		}
		var record *bootChangeRecord
		if state.DB != nil {
			var r bootChangeRecord
			if found, _ := state.DB.GetKV(bootChangeKey(acc.ID, svc), &r); found {
				record = &r
			}
		}
		effect := bootEffect(boot.BootType, server.BootID, reboot, record)
		resp := gin.H{
			"success": true, "bootId": server.BootID, "bootType": boot.BootType, "description": boot.Description,
			"powerState": server.PowerState, "state": server.State, "effect": effect,
		}
		if reboot != nil {
			resp["reboot"] = gin.H{"status": reboot["status"], "startDate": reboot["startDate"], "doneDate": reboot["doneDate"]}
		}
		if record != nil && record.BootID == server.BootID {
			resp["changedAt"] = record.At
		}
		c.JSON(http.StatusOK, resp)
	}
}

// bootEffect 判断启动设置是否已在运行中生效:
// rebooting 正在重启 / active 已生效 / pending 已改但还没重启 / unknown 无法确认。
func bootEffect(bootType string, bootID int64, reboot map[string]interface{}, record *bootChangeRecord) string {
	status := ""
	if reboot != nil {
		status, _ = reboot["status"].(string)
	}
	switch status {
	case "todo", "init", "doing":
		return "rebooting"
	}
	if record != nil && record.BootID == bootID {
		changed, err := time.Parse(time.RFC3339, record.At)
		if err == nil {
			if status == "done" {
				if done, ok := parseTaskTime(reboot["doneDate"]); ok && done.After(changed) {
					return "active"
				}
			}
			return "pending"
		}
	}
	if strings.EqualFold(bootType, "harddisk") {
		return "active"
	}
	return "unknown"
}

func parseTaskTime(v interface{}) (time.Time, bool) {
	s, _ := v.(string)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05-07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}


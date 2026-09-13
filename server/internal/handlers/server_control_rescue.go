package handlers

import (
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/ovh"
)

// 一键救援。
//
// 手动做这件事要在 OVH 后台点四步:进服务器 → 改 netboot 为救援 → 填收密码的邮箱
// → 回去重启服务器。漏掉最后一步是最常见的错误 —— netboot 改了但没重启,
// 机器还在正常系统里跑,用户以为进了救援模式,然后对着 SSH 连不上发懵。
//
// 官方流程(rescue mode 指南 + dedicated.server schema):
//   1. GET  /dedicated/server/{sn}/boot?bootType=rescue   拿救援启动项
//   2. PUT  /dedicated/server/{sn}                        设 bootId(+rescueMail/rescueSshKey)
//   3. POST /dedicated/server/{sn}/reboot                 必须重启才生效
// 退出救援就是把 bootType 换成 harddisk 再走一遍 2、3。
// https://docs.ovhcloud.com/en/guides/bare-metal-cloud/dedicated-servers/rescue_mode/

// rescueBoots 取某种 bootType 的启动项详情。
func rescueBoots(client *ovhsdk.Client, svc, bootType string) ([]map[string]interface{}, error) {
	var ids []int64
	if err := client.Get("/dedicated/server/"+svc+"/boot?bootType="+bootType, &ids); err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for _, id := range ids {
		var d map[string]interface{}
		if err := client.Get(fmt.Sprintf("/dedicated/server/%s/boot/%d", svc, id), &d); err != nil {
			// 单条详情拉不到不该让整个功能失败:id 本身已经够用来切换了
			d = map[string]interface{}{"bootId": id, "bootType": bootType}
		}
		out = append(out, d)
	}
	return out, nil
}

// pickRescueBoot 从救援启动项里挑一个。
//
// 一台机器通常有好几个救援项(不同内核版本、pro/customer)。挑法:
// 优先 kernel 里带 "customer" 的 —— 那是 OVH 现在的标准救援系统,
// 支持把密码发到指定邮箱;都没有就用第一个,并把完整列表交给前端让用户自己选。
func pickRescueBoot(boots []map[string]interface{}) (int64, string, bool) {
	if len(boots) == 0 {
		return 0, "", false
	}
	best := boots[0]
	for _, b := range boots {
		k := strings.ToLower(fmt.Sprint(b["kernel"]))
		if strings.Contains(k, "customer") {
			best = b
			break
		}
	}
	id, ok := numconv.ToInt64(best["bootId"])
	if !ok {
		return 0, "", false
	}
	return id, fmt.Sprint(best["kernel"]), true
}

// GetRescueStatus GET /api/server-control/:service_name/rescue
//
// 回答三件事:这台机现在是不是救援启动、有哪些救援项可选、上次填的收信邮箱是什么。
func GetRescueStatus(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var info map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc, &info); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": ovh.Explain(err)})
			return
		}
		currentBoot, _ := numconv.ToInt64(info["bootId"])
		boots, err := rescueBoots(client, svc, "rescue")
		if err != nil {
			// 这里不能吞:拉不到会让前端画成"这台机器不支持救援",
			// 而实际多半只是限流或权限不够
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": ovh.Explain(err)})
			return
		}
		inRescue := false
		for _, b := range boots {
			if id, ok := numconv.ToInt64(b["bootId"]); ok && id == currentBoot {
				inRescue = true
				break
			}
		}
		rescueMail, _ := info["rescueMail"].(string)
		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"inRescue":    inRescue,
			"currentBoot": currentBoot,
			"rescueMail":  rescueMail,
			"boots":       boots,
		})
	}
}

// EnterRescue POST /api/server-control/:service_name/rescue
//
// body: { email?: string, sshKey?: string, bootId?: number, confirm: true }
func EnterRescue(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		var body struct {
			Email   string `json:"email"`
			SSHKey  string `json:"sshKey"`
			BootID  int64  `json:"bootId"`
			Confirm bool   `json:"confirm"`
		}
		_ = c.ShouldBindJSON(&body)
		// 会重启机器 = 业务中断,和重装、终止同一档,必须显式确认
		if !body.Confirm {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "进入救援模式会立刻重启服务器,请在请求里带 confirm:true 确认",
			})
			return
		}
		body.Email = strings.TrimSpace(body.Email)
		if body.Email != "" {
			if _, err := mail.ParseAddress(body.Email); err != nil {
				// 邮箱写错的后果是密码发不到你手里,而机器已经重启进救援了 —— 先挡下来
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   "邮箱格式不对:" + body.Email + "。救援系统的登录密码会发到这个地址,填错就收不到",
				})
				return
			}
		}

		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}

		bootID := body.BootID
		if bootID <= 0 {
			boots, err := rescueBoots(client, svc, "rescue")
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": ovh.Explain(err)})
				return
			}
			id, _, ok := pickRescueBoot(boots)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   "这台服务器没有可用的救援启动项(OVH 返回的 bootType=rescue 列表是空的)",
				})
				return
			}
			bootID = id
		}

		// 只发要改的字段。PUT /dedicated/server/{sn} 的语义见 SetBootConfig 里的长注释。
		payload := map[string]interface{}{"bootId": bootID}
		if body.Email != "" {
			payload["rescueMail"] = body.Email
		}
		if k := strings.TrimSpace(body.SSHKey); k != "" {
			payload["rescueSshKey"] = k
		}
		if err := client.Put("/dedicated/server/"+svc, payload, nil); err != nil {
			state.Logger.Error("服务器 "+svc+" 切救援启动失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": ovh.Explain(err)})
			return
		}

		// 必须重启才生效。手动操作时漏掉这步是最常见的错误 ——
		// 改完 netboot 以为就进救援了,其实机器还在正常系统里。
		var task map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/reboot", nil, &task); err != nil {
			state.Logger.Error("服务器 "+svc+" 重启失败(救援启动已设置): "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error": "救援启动项已经设置好了,但重启没发出去(" + ovh.Explain(err) + ")。" +
					"手动重启一次就会进救援模式;或者到启动模式那里把它改回硬盘启动取消本次操作",
			})
			return
		}
		state.Logger.Warn(fmt.Sprintf("服务器 %s 已切换到救援模式并重启(bootId=%d)", svc, bootID), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"bootId":  bootID,
			"task":    task,
			"message": "已切到救援模式并重启。约 3~5 分钟后可用 root 登录;" +
				"密码会发到" + rescueMailHint(body.Email) + "。修好之后记得点「退出救援模式」",
		})
	}
}

func rescueMailHint(email string) string {
	if email != "" {
		return " " + email
	}
	return "你 OVH 账户的联系邮箱"
}

// ExitRescue POST /api/server-control/:service_name/rescue/exit
//
// 改回硬盘启动并重启。
func ExitRescue(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		var body struct {
			Confirm bool `json:"confirm"`
		}
		_ = c.ShouldBindJSON(&body)
		if !body.Confirm {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "退出救援模式会立刻重启服务器,请在请求里带 confirm:true 确认",
			})
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		boots, err := rescueBoots(client, svc, "harddisk")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": ovh.Explain(err)})
			return
		}
		if len(boots) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "OVH 没有返回硬盘启动项(bootType=harddisk),无法自动切回,请到启动模式页手动选择",
			})
			return
		}
		bootID, ok := numconv.ToInt64(boots[0]["bootId"])
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "硬盘启动项的 bootId 解析失败"})
			return
		}
		if err := client.Put("/dedicated/server/"+svc, map[string]interface{}{"bootId": bootID}, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": ovh.Explain(err)})
			return
		}
		var task map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/reboot", nil, &task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "已改回硬盘启动,但重启没发出去(" + ovh.Explain(err) + ")。手动重启一次即可",
			})
			return
		}
		state.Logger.Info(fmt.Sprintf("服务器 %s 已退出救援模式并重启(bootId=%d)", svc, bootID), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success": true, "bootId": bootID, "task": task,
			"message": "已切回硬盘启动并重启,约 3~5 分钟后恢复正常系统",
		})
	}
}

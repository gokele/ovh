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
			"success": true,
			// bootId 是下次启动配置,不是当前实际运行的操作系统。
			"inRescue":         inRescue, // 兼容旧客户端,含义同 rescueConfigured。
			"rescueConfigured": inRescue,
			"currentBoot":      currentBoot,
			"rescueMail":       rescueMail,
			"boots":            boots,
		})
	}
}

// EnterRescue POST /api/server-control/:service_name/rescue
//
// body: { email?: string, useDefaultEmail?: bool, expectedBoot?: number, sshKey?: string, bootId?: number, confirm: true }
func EnterRescue(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		var body struct {
			Email           string `json:"email"`
			SSHKey          string `json:"sshKey"`
			BootID          int64  `json:"bootId"`
			Confirm         bool   `json:"confirm"`
			UseDefaultEmail bool   `json:"useDefaultEmail"`
			ExpectedBoot    *int64 `json:"expectedBoot"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "救援请求格式不正确,未执行操作", "code": "RESCUE_INVALID_REQUEST"})
			return
		}
		// 会重启机器 = 业务中断,和重装、终止同一档,必须显式确认
		if !body.Confirm {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "进入救援模式会立刻重启服务器,请在请求里带 confirm:true 确认", "code": "E0DA171AC",
			})
			return
		}
		body.Email = strings.TrimSpace(body.Email)
		if body.Email == "" && !body.UseDefaultEmail && strings.TrimSpace(body.SSHKey) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请填写完整邮箱,或明确选择使用账户联系邮箱", "code": "RESCUE_EMAIL_REQUIRED"})
			return
		}
		if body.Email != "" {
			if !validRescueEmail(body.Email) {
				// 邮箱写错的后果是密码发不到你手里,而机器已经重启进救援了 —— 先挡下来
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   "请填写完整、有效的收信邮箱,未执行救援操作", "code": "RESCUE_EMAIL_REQUIRED",
				})
				return
			}
		}

		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		if !checkRescueBoot(c, client, svc, body.ExpectedBoot) {
			return
		}
		if body.UseDefaultEmail {
			var contact struct {
				Email string `json:"email"`
			}
			if err := client.Get("/me", &contact); err != nil || !validRescueEmail(strings.TrimSpace(contact.Email)) {
				c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "无法取得有效的账户联系邮箱,未执行操作", "code": "RESCUE_EMAIL_REQUIRED"})
				return
			}
			// 省略 rescueMail 会保留旧地址,必须明确写入本账户的联系邮箱。
			body.Email = strings.TrimSpace(contact.Email)
		}

		bootID := body.BootID
		boots, err := rescueBoots(client, svc, "rescue")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": ovh.Explain(err)})
			return
		}
		if bootID <= 0 {
			id, _, ok := pickRescueBoot(boots)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   "这台服务器没有可用的救援启动项(OVH 返回的 bootType=rescue 列表是空的)", "code": "EF8C25932",
				})
				return
			}
			bootID = id
		} else {
			valid := false
			for _, b := range boots {
				if id, ok := numconv.ToInt64(b["bootId"]); ok && id == bootID {
					valid = true
					break
				}
			}
			if !valid {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "所选启动项不属于此服务器的救援启动列表", "code": "RESCUE_INVALID_REQUEST"})
				return
			}
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
			// 重启防抖(已有重启在进行):启动项已切好,机器这次重启就会进救援 —— 流程等价于成功
			if ovh.IsRebootAlreadyRequested(err) {
				state.Logger.Warn("服务器 "+svc+" 救援启动已设置,且已有重启在进行中(无需再发)", "server_control")
				c.JSON(http.StatusOK, gin.H{
					"success": true,
					"bootId":  bootID,
					"message": "救援启动项已设置,并且服务器已有一个重启在进行中 —— 这次重启就会进入救援模式。" +
						"约 3~5 分钟后可用 root 登录,密码发到" + rescueMailHint(body.Email) + ";修好之后记得点「退出救援模式」",
				})
				return
			}
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
			"message": "已设置救援启动并提交重启请求，请等待 OVH 任务完成；" +
				"密码会发到" + rescueMailHint(body.Email) + "。修好之后记得点「退出救援模式」",
		})
	}
}

// validRescueEmail 与表单一致,拒绝未写完的域名、显示名称和多行地址。
func validRescueEmail(value string) bool {
	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Address != value || strings.ContainsAny(value, "\r\n") {
		return false
	}
	at := strings.LastIndex(value, "@")
	return at > 0 && strings.Contains(value[at+1:], ".") && !strings.HasSuffix(value, ".")
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
			Confirm      bool   `json:"confirm"`
			ExpectedBoot *int64 `json:"expectedBoot"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "救援请求格式不正确,未执行操作", "code": "RESCUE_INVALID_REQUEST"})
			return
		}
		if !body.Confirm {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "退出救援模式会立刻重启服务器,请在请求里带 confirm:true 确认", "code": "E4F860E38",
			})
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		if !checkRescueBoot(c, client, svc, body.ExpectedBoot) {
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
				"error":   "OVH 没有返回硬盘启动项(bootType=harddisk),无法自动切回,请到启动模式页手动选择", "code": "E46BFD00A",
			})
			return
		}
		bootID, ok := numconv.ToInt64(boots[0]["bootId"])
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "硬盘启动项的 bootId 解析失败", "code": "EC659066B"})
			return
		}
		if err := client.Put("/dedicated/server/"+svc, map[string]interface{}{"bootId": bootID}, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": ovh.Explain(err)})
			return
		}
		var task map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/reboot", nil, &task); err != nil {
			if ovh.IsRebootAlreadyRequested(err) {
				// 已有重启在进行 + 启动项已切回硬盘 → 那次重启就会回到正常系统
				state.Logger.Warn("服务器 "+svc+" 已切回硬盘启动,且已有重启在进行中(无需再发)", "server_control")
				c.JSON(http.StatusOK, gin.H{
					"success": true, "bootId": bootID,
					"message": "已切回硬盘启动,并且服务器已有一个重启在进行中 —— 这次重启就会回到正常系统,约 3~5 分钟",
					"code":    "EA0CDDD4E",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "已改回硬盘启动,但重启没发出去(" + ovh.Explain(err) + ")。手动重启一次即可",
			})
			return
		}
		state.Logger.Info(fmt.Sprintf("服务器 %s 已退出救援模式并重启(bootId=%d)", svc, bootID), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success": true, "bootId": bootID, "task": task,
			"message": "已切回硬盘启动并重启,约 3~5 分钟后恢复正常系统", "code": "E9F35BA62",
		})
	}
}

// checkRescueBoot 发现确认期间启动配置发生变化时停止,避免按过期页面重启。
func checkRescueBoot(c *gin.Context, client *ovhsdk.Client, svc string, expected *int64) bool {
	if expected == nil { // 兼容已有 API 客户端;新界面始终携带已确认的 bootId。
		return true
	}
	var info map[string]interface{}
	if err := client.Get("/dedicated/server/"+svc, &info); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "无法重新核对启动配置,未执行操作", "code": "RESCUE_BOOT_CHANGED"})
		return false
	}
	current, ok := numconv.ToInt64(info["bootId"])
	if !ok || current != *expected {
		c.JSON(http.StatusConflict, gin.H{"success": false, "error": "启动配置已变化,请重新打开窗口核对后再操作", "code": "RESCUE_BOOT_CHANGED"})
		return false
	}
	return true
}

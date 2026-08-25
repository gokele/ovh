// Package notify 把通知发到所有已配置的通道。
//
// 为什么要有第二条通道:补货监控的全部价值就是"有货的那一刻你能收到消息"。
// 而在此之前,Telegram 是唯一通道,并且监控在 Telegram 校验失败时会**自动停止**——
// 也就是说 TG bot 被封、token 失效、机器连不上 api.telegram.org 这几种情况下,
// 用户不是"少收一条通知",而是整个监控停摆,且只有翻日志才知道。
//
// 现在只要还有一条通道可用,监控就继续跑;全部通道都不可用才停。
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/telegram"
)

// Channel 一条通知通道的状态
type Channel struct {
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	OK         bool   `json:"ok"`
	Detail     string `json:"detail,omitempty"`
}

// webhookPayload 发给自定义 webhook 的结构。
// 字段名同时兼容几种常见接收端:钉钉/飞书的 text.content、以及大多数自建服务读的 text/message。
type webhookPayload struct {
	Text    string `json:"text"`
	Message string `json:"message"`
	Title   string `json:"title"`
	MsgType string `json:"msgtype"`
	Content struct {
		Text string `json:"text"`
	} `json:"text_content"`
}

// Broadcast 把一条消息发到所有已配置的通道。
// 返回成功送达的通道数 —— 调用方据此判断"这条通知到底有没有发出去"。
//
// 注意 replyMarkup 只有 Telegram 支持(一键下单按钮),webhook 那边只能收到纯文本。
// 这是有意的:让按钮在 TG 上继续可用,同时保证 TG 挂掉时消息本身还能到达。
func Broadcast(state *app.State, message string, replyMarkup map[string]interface{}) int {
	delivered := 0
	cfg := state.Config.Get()

	if strings.TrimSpace(cfg.TgToken) != "" && strings.TrimSpace(cfg.TgChatID) != "" {
		if telegram.SendMessage(state, message, replyMarkup) {
			delivered++
		}
	}
	if url := strings.TrimSpace(cfg.NotifyWebhookURL); url != "" {
		if err := sendWebhook(url, message); err != nil {
			state.Logger.Warn("Webhook 通知发送失败: "+err.Error(), "notify")
		} else {
			delivered++
		}
	}
	if delivered == 0 {
		state.Logger.Error("这条通知一个通道都没送达,请检查 Telegram / Webhook 配置", "notify")
	}
	return delivered
}

// sendWebhook POST 一条 JSON 到用户自定义地址。
// 不猜对方的协议:同一条文本塞进几个最常见的字段名,钉钉/飞书/Bark/自建都能取到其中一个。
func sendWebhook(url, message string) error {
	var p webhookPayload
	p.Text = message
	p.Message = message
	p.Title = "OVH 控制台"
	p.MsgType = "text"
	p.Content.Text = message

	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// Status 逐个通道体检。verifyRemote=true 时会真的去调远端(Telegram getMe / getChat),
// 只想知道"配没配"就传 false —— 监控循环里每轮都调远端会白白打 API。
func Status(state *app.State, verifyRemote bool) []Channel {
	cfg := state.Config.Get()
	out := make([]Channel, 0, 2)

	tg := Channel{Name: "Telegram"}
	tg.Configured = strings.TrimSpace(cfg.TgToken) != "" && strings.TrimSpace(cfg.TgChatID) != ""
	if tg.Configured {
		if verifyRemote {
			ok, reason := telegram.VerifyConfig(state)
			tg.OK, tg.Detail = ok, reason
		} else {
			tg.OK = true
		}
	} else {
		tg.Detail = "未配置 Bot Token 或 Chat ID"
	}
	out = append(out, tg)

	wh := Channel{Name: "Webhook"}
	url := strings.TrimSpace(cfg.NotifyWebhookURL)
	wh.Configured = url != ""
	if wh.Configured {
		if verifyRemote {
			if err := sendWebhook(url, "OVH 控制台:通知通道连通性测试"); err != nil {
				wh.Detail = err.Error()
			} else {
				wh.OK = true
			}
		} else {
			wh.OK = true
		}
	} else {
		wh.Detail = "未配置"
	}
	out = append(out, wh)

	return out
}

// AnyAvailable 是否至少还有一条通道可用。
// 监控据此决定要不要自停 —— 只要还有人能收到消息,监控就该继续跑。
func AnyAvailable(state *app.State, verifyRemote bool) (bool, string) {
	chans := Status(state, verifyRemote)
	reasons := make([]string, 0, len(chans))
	for _, c := range chans {
		if c.Configured && c.OK {
			return true, ""
		}
		if c.Configured {
			reasons = append(reasons, c.Name+": "+c.Detail)
		}
	}
	if len(reasons) == 0 {
		return false, "没有配置任何通知通道(Telegram / Webhook)"
	}
	return false, strings.Join(reasons, "; ")
}

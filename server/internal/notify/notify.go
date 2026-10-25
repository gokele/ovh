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
	"net/url"
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

// webhookPayload 保留自建服务原有字段,同时支持飞书的 msg_type/content。
type webhookPayload struct {
	Text          interface{} `json:"text"`
	Message       string      `json:"message"`
	Title         string      `json:"title"`
	MsgType       string      `json:"msgtype"`
	FeishuMsgType string      `json:"msg_type"`
	FeishuContent struct {
		Text string `json:"text"`
	} `json:"content"`
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
	delivered, _ := BroadcastWithStatus(state, message, replyMarkup)
	return delivered
}

// BroadcastWithStatus 返回本次实际发送结果,避免测试通知拿配置状态冒充送达状态。
func BroadcastWithStatus(state *app.State, message string, replyMarkup map[string]interface{}) (int, []Channel) {
	delivered := 0
	cfg := state.Config.Get()
	tg := Channel{Name: "Telegram", Configured: strings.TrimSpace(cfg.TgToken) != "" && strings.TrimSpace(cfg.TgChatID) != ""}
	wh := Channel{Name: "Webhook", Configured: strings.TrimSpace(cfg.NotifyWebhookURL) != ""}
	if tg.Configured {
		if telegram.SendMessage(state, message, replyMarkup) {
			delivered++
			tg.OK = true
		} else {
			tg.Detail = "发送失败,请检查 Telegram 配置及网络"
		}
	}
	if wh.Configured {
		if err := sendWebhook(strings.TrimSpace(cfg.NotifyWebhookURL), message); err != nil {
			wh.Detail = err.Error()
			state.Logger.Warn("Webhook 通知发送失败: "+err.Error(), "notify")
		} else {
			delivered++
			wh.OK = true
		}
	}
	if delivered == 0 {
		state.Logger.Error("这条通知一个通道都没送达,请检查 Telegram / Webhook 配置", "notify")
	}
	return delivered, []Channel{tg, wh}
}

// sendWebhook 发文本消息,同时检查 HTTP 状态和常见的业务状态码。
func sendWebhook(webhookURL, message string) error {
	target, err := url.Parse(webhookURL)
	if err != nil {
		return err
	}
	host := strings.ToLower(target.Hostname())
	feishu := host == "open.feishu.cn" || host == "open.larksuite.com"
	dingtalk := host == "oapi.dingtalk.com"
	var p webhookPayload
	p.Text = message
	p.Message = message
	p.Title = "OVH 控制台"
	p.MsgType = "text"
	p.Content.Text = message
	p.FeishuMsgType = "text"
	p.FeishuContent.Text = message
	// 钉钉的 text 必须是对象;自建服务保持原来的字符串字段。
	if dingtalk {
		p.Text = map[string]string{"content": message}
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	const maxResponse = 64 << 10
	response, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return fmt.Errorf("读取 Webhook 响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := response
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if len(response) > maxResponse {
		return fmt.Errorf("Webhook 响应过大,无法确认发送结果")
	}
	var result struct {
		Code          *json.Number `json:"code"`
		ErrCode       *json.Number `json:"errcode"`
		StatusCode    *json.Number `json:"StatusCode"`
		Msg           string       `json:"msg"`
		ErrMsg        string       `json:"errmsg"`
		StatusMessage string       `json:"StatusMessage"`
		Message       string       `json:"message"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		if feishu || dingtalk {
			return fmt.Errorf("Webhook 返回无效的业务响应: %w", err)
		}
		return nil // 自建服务允许空响应或纯文本。
	}
	if (feishu && result.Code == nil && result.StatusCode == nil) || (dingtalk && result.ErrCode == nil) {
		return fmt.Errorf("Webhook 响应缺少业务状态码,无法确认发送结果")
	}
	for _, status := range []struct {
		name    string
		code    *json.Number
		message string
	}{
		{"code", result.Code, result.Msg}, {"errcode", result.ErrCode, result.ErrMsg}, {"StatusCode", result.StatusCode, result.StatusMessage},
	} {
		if status.code == nil {
			continue
		}
		code, err := status.code.Int64()
		if err != nil {
			return fmt.Errorf("Webhook 返回无效的 %s: %s", status.name, status.code.String())
		}
		// Bark 和部分自建服务用 code=200 表示成功;飞书、钉钉用 0。
		if code != 0 && !(status.name == "code" && code == 200 && !feishu && !dingtalk) {
			detail := status.message
			if detail == "" {
				detail = result.Message
			}
			return fmt.Errorf("Webhook 返回错误 (%s %d): %s", status.name, code, detail)
		}
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

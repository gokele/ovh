package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"bytes"
)

// ─────────────────────────────────────────────────────────────────────────────
// Long polling（getUpdates）—— 不需要公网地址的那条路
//
// Telegram 收取 update 只有两种方式,而且**互斥**:
//
//	webhook  Telegram 主动 POST 给你 → 必须有公网 HTTPS 域名 + 受信证书,
//	         端口还只能是 443/80/88/8443。家宽、NAT 后面、没域名的小鸡全都用不了。
//	polling  你主动去 getUpdates 拉 → 只需要能出站访问 api.telegram.org。
//
// 官方文档原话:getUpdates "will not work if an outgoing webhook is set up",
// 所以切到 polling 之前必须先 deleteWebhook,切回去要重新 setWebhook。
//
// 对这个项目来说 polling 还顺带解决了 webhook 那条路最难受的一件事:
// webhook 端点必须放在鉴权白名单里(Telegram 不可能带 X-API-Key),
// 于是只能靠 secret_token 证明来源,还留了个"老部署没注册 secret"的兼容模式。
// polling 没有入站端点,伪造来源这个问题根本不存在。
// ─────────────────────────────────────────────────────────────────────────────

const (
	// PollTimeoutSeconds 单次 getUpdates 的长轮询挂起时长。
	// 文档没规定上限,30 秒是通用安全值:够长(空闲时几乎不产生请求),
	// 又远低于各种中间代理的默认空闲断连阈值。
	PollTimeoutSeconds = 30

	// PollLimit 单次最多取几条。文档允许 1-100。
	PollLimit = 100

	// kvPollOffset 已确认到的 update_id。必须落库 ——
	// offset 只在**下一次** getUpdates 时才确认,进程重启(尤其是自更新)
	// 如果从头开始,24 小时内的积压会被重放一遍,等于重复下单。
	kvPollOffset = "telegram_poll_offset"
)

// pollBackoff 连续失败时的退避梯度。最长 60 秒:
// 网络抖动几秒就好,而 409/token 失效这类问题等再久也没用,
// 退到 60 秒是为了别把日志和 Telegram 的限流刷爆。
var pollBackoff = []time.Duration{
	1 * time.Second, 2 * time.Second, 5 * time.Second,
	10 * time.Second, 30 * time.Second, 60 * time.Second,
}

// UpdateHandler 处理一条 update。由 handlers 包注入,
// 这样 poller 不用反向依赖 handlers(会循环 import)。
type UpdateHandler func(data map[string]interface{})

// Poller 长轮询收取器。
type Poller struct {
	state   *app.State
	handle  UpdateHandler
	mu      sync.Mutex
	running bool

	// generation 代际号,每次 Start 递增。
	//
	// 只翻 running 布尔是不够的:"停止后立刻启动"会让旧循环活下来
	// (它下一个检查点看到 running 又是 true),两个循环并存。
	// 而两个 getUpdates 并发打同一个 token,Telegram 会让它们互相踢掉,
	// 表现就是按钮时灵时不灵。monitor / vps 那两个循环踩过同样的坑。
	generation int64

	lastErr   atomic.Value // string
	lastPoll  atomic.Value // time.Time
	offsetVal atomic.Int64
}

// NewPoller 构造。handle 在每条 update 上被调用(串行)。
func NewPoller(state *app.State, handle UpdateHandler) *Poller {
	p := &Poller{state: state, handle: handle}
	p.lastErr.Store("")
	p.lastPoll.Store(time.Time{})
	return p
}

// Running 当前是否在跑。
func (p *Poller) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// Status 给设置页用的状态快照。
func (p *Poller) Status() map[string]interface{} {
	last, _ := p.lastPoll.Load().(time.Time)
	errStr, _ := p.lastErr.Load().(string)
	out := map[string]interface{}{
		"running":   p.Running(),
		"offset":    p.offsetVal.Load(),
		"lastError": errStr,
	}
	if !last.IsZero() {
		out["lastPollAt"] = last.Format(time.RFC3339)
	}
	return out
}

// Start 启动长轮询。会先 deleteWebhook —— 两者互斥,不删的话 getUpdates 直接报错。
func (p *Poller) Start() error {
	cfg := p.state.Config.Get()
	if strings.TrimSpace(cfg.TgToken) == "" {
		return fmt.Errorf("未配置 Telegram Bot Token")
	}

	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return nil // 已经在跑,幂等
	}
	p.generation++
	gen := p.generation
	p.running = true
	p.mu.Unlock()

	// webhook 和 polling 互斥:不先摘掉 webhook,getUpdates 会一直失败。
	// drop_pending_updates 不传 —— 默认保留积压。
	// 理由:自更新重启只花几秒,那几秒里按下的按钮不该丢。
	// 积压里的老按钮有自己的 TTL 和一次性 claim 兜底,重复下单由 update_id 幂等表挡住。
	if ok, msg := deleteWebhook(p.state); !ok {
		p.state.Logger.Warn("切换到长轮询前删除 webhook 失败(仍继续尝试): "+msg, "telegram")
	} else {
		p.state.Logger.Info("已删除 webhook,切换到长轮询模式", "telegram")
	}

	// 恢复上次确认到的位置
	var saved int64
	if p.state.DB != nil {
		if ok, _ := p.state.DB.GetKV(kvPollOffset, &saved); ok && saved > 0 {
			p.offsetVal.Store(saved)
			p.state.Logger.Info(fmt.Sprintf("长轮询从上次的 offset=%d 继续", saved), "telegram")
		}
	}

	go p.loop(gen)
	p.state.Logger.Info("Telegram 长轮询已启动(不需要公网地址)", "telegram")
	return nil
}

// Stop 停止长轮询。正在挂起的那次 getUpdates 最多再等 PollTimeoutSeconds 秒退出。
func (p *Poller) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	p.generation++ // 让旧循环的 stillMine 立刻失效
	p.mu.Unlock()
	p.state.Logger.Info("Telegram 长轮询已停止", "telegram")
}

// stillMine 这个代际是不是还是当前那个。
func (p *Poller) stillMine(gen int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running && p.generation == gen
}

func (p *Poller) loop(gen int64) {
	defer func() {
		if r := recover(); r != nil {
			p.state.Logger.Error(fmt.Sprintf("长轮询循环异常退出: %v", r), "telegram")
			p.mu.Lock()
			if p.generation == gen {
				p.running = false
			}
			p.mu.Unlock()
		}
	}()

	fails := 0
	// 长轮询要挂起 PollTimeoutSeconds 秒才回,客户端超时必须留出富余
	client := httpClient((PollTimeoutSeconds + 15) * time.Second)

	for p.stillMine(gen) {
		updates, err := p.fetch(client)
		if err != nil {
			if !p.stillMine(gen) {
				return
			}
			fails++
			p.lastErr.Store(err.Error())
			d := pollBackoff[min(fails-1, len(pollBackoff)-1)]
			// 409 = 另一个进程在用同一个 token 拉 update。
			// 两个实例互相踢,表现是按钮时灵时不灵,值得单独喊一声。
			if strings.Contains(err.Error(), "Conflict") || strings.Contains(err.Error(), "409") {
				p.state.Logger.Error("长轮询冲突:同一个 Bot Token 有另一个进程在收取 update("+
					"是不是开了两份程序,或者别处还挂着 webhook?)。"+d.String()+" 后重试", "telegram")
			} else {
				p.state.Logger.Warn("长轮询失败("+d.String()+" 后重试): "+err.Error(), "telegram")
			}
			p.sleepInterruptible(gen, d)
			continue
		}
		if fails > 0 {
			p.state.Logger.Info("长轮询已恢复", "telegram")
			fails = 0
			p.lastErr.Store("")
		}
		p.lastPoll.Store(time.Now())

		for _, up := range updates {
			if !p.stillMine(gen) {
				return
			}
			id := parseUpdateIDAny(up["update_id"])
			// 先处理再推进 offset。反过来的话,处理途中崩掉这条就永远丢了 ——
			// 而"丢一次下单"比"重复处理一次"严重得多,后者有 update_id 幂等表兜底。
			p.handle(up)
			if id > 0 {
				p.advance(id + 1)
			}
		}
	}
}

// fetch 拉一批 update。
func (p *Poller) fetch(client *http.Client) ([]map[string]interface{}, error) {
	cfg := p.state.Config.Get()
	token := strings.TrimSpace(cfg.TgToken)
	if token == "" {
		return nil, fmt.Errorf("未配置 Telegram Bot Token")
	}

	q := url.Values{}
	if off := p.offsetVal.Load(); off > 0 {
		q.Set("offset", strconv.FormatInt(off, 10))
	}
	q.Set("limit", strconv.Itoa(PollLimit))
	q.Set("timeout", strconv.Itoa(PollTimeoutSeconds))
	// 只要这两类。不声明的话默认会收到一堆用不上的类型(成员变动、频道帖子…),
	// 白白占 update_id 和幂等表。注意这个设置在 Telegram 侧是**记住**的,
	// 所以每次都显式传,避免沿用上一次的。
	q.Set("allowed_updates", `["message","callback_query"]`)

	resp, err := client.Get("https://api.telegram.org/bot" + token + "/getUpdates?" + q.Encode())
	if err != nil {
		return nil, fmt.Errorf("%s", scrub(err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, PollLimit*MaxTelegramBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %s", scrub(err.Error()))
	}

	var result struct {
		OK          bool                     `json:"ok"`
		Description string                   `json:"description"`
		ErrorCode   int                      `json:"error_code"`
		Result      []map[string]interface{} `json:"result"`
	}
	// UseNumber:id 字段(TG 的 callback/message id ~5.8e18)解成 float64 会
	// 丢精度 + 科学计数法,answerCallbackQuery 拿到的 id 和 TG 发的对不上,
	// 按钮点击的 toast 反馈永远失败。json.Number 保住精确整数
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("响应不是合法 JSON(HTTP %d)", resp.StatusCode)
	}
	if !result.OK {
		return nil, fmt.Errorf("Telegram 拒绝(%d): %s", result.ErrorCode, result.Description)
	}
	return result.Result, nil
}

// advance 推进并落库 offset。
func (p *Poller) advance(next int64) {
	if next <= p.offsetVal.Load() {
		return
	}
	p.offsetVal.Store(next)
	if p.state.DB != nil {
		if err := p.state.DB.SetKV(kvPollOffset, next); err != nil {
			// 落库失败不致命:内存里的 offset 仍然对,只是重启后可能重放,
			// 而重放有 update_id 幂等表挡着。
			p.state.Logger.Warn("保存长轮询 offset 失败: "+err.Error(), "telegram")
		}
	}
}

// sleepInterruptible 可被 Stop 打断的等待。
func (p *Poller) sleepInterruptible(gen int64, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !p.stillMine(gen) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// deleteWebhook 摘掉 webhook,让 getUpdates 可用。
func deleteWebhook(state *app.State) (bool, string) {
	cfg := state.Config.Get()
	token := strings.TrimSpace(cfg.TgToken)
	if token == "" {
		return false, "未配置 Telegram Bot Token"
	}
	client := httpClient(10 * time.Second)
	resp, err := client.Get("https://api.telegram.org/bot" + token + "/deleteWebhook")
	if err != nil {
		return false, scrub(err.Error())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, MaxTelegramBodyBytes))
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(body, &r)
	if r.OK {
		return true, ""
	}
	return false, r.Description
}

// parseUpdateIDAny JSON 数字解出来是 float64。
func parseUpdateIDAny(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

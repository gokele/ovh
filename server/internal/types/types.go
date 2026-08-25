package types

import "time"

type Config struct {
	AppKey      string `json:"appKey"`
	AppSecret   string `json:"appSecret"`
	ConsumerKey string `json:"consumerKey"`
	Endpoint    string `json:"endpoint"`
	TgToken     string `json:"tgToken"`
	TgChatID    string `json:"tgChatId"`
	IAM         string `json:"iam"`
	Zone        string `json:"zone"`

	// TgWebhookSecret Telegram setWebhook 的 secret_token。Telegram 会在每次回调里带
	// X-Telegram-Bot-Api-Secret-Token 头，用它证明请求真的来自 Telegram。
	// 首次需要时自动生成并落库；GetSettings 不会把它回给前端。
	TgWebhookSecret string `json:"tgWebhookSecret,omitempty"`
	// NotifyWebhookURL 第二条通知通道:一个接收 JSON POST 的地址(钉钉/飞书/Bark/自建都行)。
	// 补货监控的全部价值就是"有货那一刻你能收到消息",单通道意味着 Telegram 一挂就全盲。
	NotifyWebhookURL string `json:"notifyWebhookUrl,omitempty"`
	// TgWebhookSecretRegistered secret 是否已经推给 Telegram（setWebhook 成功过）。
	// false 时 webhook 处于兼容模式：不强制校验 secret，避免升级后老用户的按钮直接全挂。
	TgWebhookSecretRegistered bool `json:"tgWebhookSecretRegistered,omitempty"`
}

// DefaultConfig 默认配置
func DefaultConfig() Config {
	return Config{
		Endpoint: "ovh-eu",
		IAM:      "go-ovh-ie",
		Zone:     "IE",
	}
}

// LogEntry 日志条目（字段名与前端 JSON 结构一致）
type LogEntry struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Source    string `json:"source"`
}

// Stats 对应 /api/stats 响应
type Stats struct {
	ActiveQueues          int  `json:"activeQueues"`
	TotalServers          int  `json:"totalServers"`
	AvailableServers      int  `json:"availableServers"`
	PurchaseSuccess       int  `json:"purchaseSuccess"`
	PurchaseFailed        int  `json:"purchaseFailed"`
	QueueProcessorRunning bool `json:"queueProcessorRunning"`
	MonitorRunning        bool `json:"monitorRunning"`
}

// OVHAccount OVH 账户凭据。多账户场景下每条记录代表一个 OVH 账户。
type OVHAccount struct {
	ID          string `json:"id"`       // UUID
	Name        string `json:"name"`     // 用户起的名字（"主号" / "小号 A"）
	Endpoint    string `json:"endpoint"` // ovh-eu / ovh-us / ovh-ca
	Zone        string `json:"zone"`     // IE/FR/DE/US/CA/...
	AppKey      string `json:"appKey"`
	AppSecret   string `json:"appSecret"`
	ConsumerKey string `json:"consumerKey"`
	IAM         string `json:"iam"`       // go-ovh-<zone-lower>
	IsDefault   bool   `json:"isDefault"` // 默认账户（未指定时 fallback 用它）
	CreatedAt   string `json:"createdAt"`
}

// QueueItem 抢购队列项
type QueueItem struct {
	ID            string   `json:"id"`
	AccountID     string   `json:"accountId"` // 该任务下单时用的 OVH 账户
	PlanCode      string   `json:"planCode"`
	Datacenter    string   `json:"datacenter"`
	Options       []string `json:"options"`
	Status        string   `json:"status"` // running / pending / paused / completed
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
	RetryInterval int      `json:"retryInterval"`
	RetryCount    int      `json:"retryCount"`
	// FailureCount 只统计"真的向 OVH 提交过并失败"的次数;无货的空轮不算。
	// MaxRetries 封顶用它而不是 RetryCount —— 抢购的常态就是绝大多数轮次都无货,
	// 拿轮次封顶会让任务在还没真正试过几次时就被判死。
	FailureCount       int     `json:"failureCount,omitempty"`
	MaxRetries         int     `json:"maxRetries,omitempty"`
	LastCheckTime      float64 `json:"lastCheckTime"`
	QuickOrder         bool    `json:"quickOrder,omitempty"`
	Priority           int     `json:"priority,omitempty"`
	FromTelegram       bool    `json:"fromTelegram,omitempty"`
	ConfigSniperTaskID string  `json:"configSniperTaskId,omitempty"`
}

// PriceInfo 价格信息
type PriceInfo struct {
	WithTax      *float64 `json:"withTax"`
	WithoutTax   *float64 `json:"withoutTax"`
	Tax          *float64 `json:"tax"`
	CurrencyCode string   `json:"currencyCode"`
}

// PurchaseHistoryEntry 抢购历史
type PurchaseHistoryEntry struct {
	ID             string   `json:"id"`
	AccountID      string   `json:"accountId"` // 哪个账户买的
	TaskID         string   `json:"taskId"`
	PlanCode       string   `json:"planCode"`
	Datacenter     string   `json:"datacenter"`
	Options        []string `json:"options"`
	Status         string   `json:"status"` // success / failed
	OrderID        string   `json:"orderId"`
	OrderURL       string   `json:"orderUrl"`
	ErrorMessage   *string  `json:"errorMessage"`
	PurchaseTime   string   `json:"purchaseTime"`
	AttemptCount   int      `json:"attemptCount"`
	ExpirationTime string   `json:"expirationTime,omitempty"`
	// RetractionTime 订单的撤销权截止时间（billing.Order.retractionDate）。
	// 单独开一个字段而不是塞进 ExpirationTime：retractionDate 是"多久内可无理由撤单"，
	// expirationDate 是"订单未付款何时作废"，语义不同，混用会让用户把撤销期当成付款截止期。
	RetractionTime string     `json:"retractionTime,omitempty"`
	Price          *PriceInfo `json:"price,omitempty"`
	// Timing 这一单每个阶段花了多久。抢购输了之后唯一有用的信息就是"慢在哪一步" ——
	// 是 OVH 的库存接口慢、还是自己这台机器建购物车慢、还是最后 checkout 排队了。
	Timing  []PhaseTiming `json:"timing,omitempty"`
	TotalMs int64         `json:"totalMs,omitempty"`
}

// PhaseTiming 抢购链路上一个阶段的墙钟耗时
type PhaseTiming struct {
	Name string `json:"name"`
	Ms   int64  `json:"ms"`
}

// Datacenter 服务器目录中单个机房可用性
type Datacenter struct {
	Datacenter   string `json:"datacenter"`
	Availability string `json:"availability"`
	DCName       string `json:"dcName,omitempty"`
	Region       string `json:"region,omitempty"`
}

// ServerOption 选项标签
type ServerOption struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	Family    string `json:"family,omitempty"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

// ServerPlan 服务器目录项
type ServerPlan struct {
	PlanCode         string         `json:"planCode"`
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	CPU              string         `json:"cpu"`
	Memory           string         `json:"memory"`
	Storage          string         `json:"storage"`
	Bandwidth        string         `json:"bandwidth"`
	VrackBandwidth   string         `json:"vrackBandwidth"`
	Datacenters      []Datacenter   `json:"datacenters"`
	DefaultOptions   []ServerOption `json:"defaultOptions"`
	AvailableOptions []ServerOption `json:"availableOptions"`
}

// SubscriptionHistoryEntry 监控订阅的历史记录条目
type SubscriptionHistoryEntry struct {
	Timestamp  string                 `json:"timestamp"`
	Datacenter string                 `json:"datacenter"`
	Status     string                 `json:"status"`
	ChangeType string                 `json:"changeType"`
	OldStatus  interface{}            `json:"oldStatus"`
	Config     map[string]interface{} `json:"config,omitempty"`
}

// Subscription 监控订阅（跨账户共享列表;auto-order 触发时按 AutoOrderAccountID 下单）
type Subscription struct {
	PlanCode           string                     `json:"planCode"`
	Datacenters        []string                   `json:"datacenters"`
	NotifyAvailable    bool                       `json:"notifyAvailable"`
	NotifyUnavailable  bool                       `json:"notifyUnavailable"`
	LastStatus         map[string]string          `json:"lastStatus"`
	CreatedAt          string                     `json:"createdAt"`
	History            []SubscriptionHistoryEntry `json:"history"`
	ServerName         string                     `json:"serverName,omitempty"`
	AutoOrder          bool                       `json:"autoOrder,omitempty"`
	Quantity           int                        `json:"quantity,omitempty"`
	AutoOrderAccountID string                     `json:"autoOrderAccountId,omitempty"` // 空 = 触发时只通知不下单
}

// VPSSubscription VPS 监控订阅
type VPSSubscription struct {
	ID                 string                   `json:"id"`
	PlanCode           string                   `json:"planCode"`
	OvhSubsidiary      string                   `json:"ovhSubsidiary"`
	Datacenters        []string                 `json:"datacenters"`
	MonitorLinux       bool                     `json:"monitorLinux"`
	MonitorWindows     bool                     `json:"monitorWindows"`
	NotifyAvailable    bool                     `json:"notifyAvailable"`
	NotifyUnavailable  bool                     `json:"notifyUnavailable"`
	LastStatus         map[string]string        `json:"lastStatus"`
	History            []map[string]interface{} `json:"history"`
	CreatedAt          string                   `json:"createdAt"`
	AutoOrderAccountID string                   `json:"autoOrderAccountId,omitempty"` // 空 = 触发时只通知不下单
}

// CacheInfo 服务器列表缓存信息
type CacheInfo struct {
	Cached             bool     `json:"cached"`
	UsingExpiredCache  bool     `json:"usingExpiredCache"`
	CacheAgeMinutes    int      `json:"cacheAgeMinutes"`
	Timestamp          *float64 `json:"timestamp"`
	CacheAge           *int     `json:"cacheAge"`
	CacheDuration      int      `json:"cacheDuration"`
	NextAutoRefresh    *float64 `json:"nextAutoRefresh"`
	AutoRefreshEnabled bool     `json:"autoRefreshEnabled"`
}

// NowISO 返回 ISO8601 时间（与 datetime.now().isoformat() 一致）
func NowISO() string {
	return time.Now().Format("2006-01-02T15:04:05.000000")
}

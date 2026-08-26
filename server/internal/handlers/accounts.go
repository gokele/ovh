package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// ── 输入 / 输出 DTO ────────────────────────────────────────────────────────

// accountInput POST/PUT body
type accountInput struct {
	Name        string `json:"name"`
	Endpoint    string `json:"endpoint"` // 可空,会按 zone 推断
	Zone        string `json:"zone"`
	AppKey      string `json:"appKey"`
	AppSecret   string `json:"appSecret"`
	ConsumerKey string `json:"consumerKey"`
	IAM         string `json:"iam"` // 可空,会自动生成 go-ovh-<zone>
	SetDefault  bool   `json:"setDefault"`
}

// endpointForZone 根据 zone 推 endpoint。
// 归属表在 ovh 包(唯一权威来源):以前这里漏了 WE / WS 两个子公司,
// 它们属于加区站点却被落到 ovh-eu,建出来的账户从第一次调用起就打错站点。
func endpointForZone(zone string) string {
	return ovh.EndpointForSubsidiary(zone)
}

// endpointRegion 见 ovh.EndpointRegion(同大区的品牌别名视为等价)
func endpointRegion(endpoint string) string { return ovh.EndpointRegion(endpoint) }

// fillDerived 补全 Endpoint / IAM
func (in *accountInput) normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Zone = strings.ToUpper(strings.TrimSpace(in.Zone))
	in.AppKey = strings.TrimSpace(in.AppKey)
	in.AppSecret = strings.TrimSpace(in.AppSecret)
	in.ConsumerKey = strings.TrimSpace(in.ConsumerKey)
	in.IAM = strings.TrimSpace(in.IAM)
	if in.Zone == "" {
		// 没填 zone 时按 endpoint 推同大区的默认子公司,而不是一律回落 "IE"。
		// 回落 IE 对「只填了 endpoint=ovh-us / ovh-ca」的请求是致命的:
		// zone=IE 属于 EU 区,下面的同大区校验会直接把这条合法请求打回,
		// 报的还是"子公司 IE 与 endpoint ovh-us 不在同一大区"这种用户根本没输入过的内容。
		in.Zone = ovh.DefaultSubsidiaryForEndpoint(in.Endpoint)
	}
	if in.Endpoint == "" {
		in.Endpoint = endpointForZone(in.Zone)
	}
	if in.IAM == "" {
		in.IAM = "go-ovh-" + strings.ToLower(in.Zone)
	}
}

// validateZoneEndpoint 校验 (子公司, endpoint) 这一对本身是否自洽。
// 单独抽出来是因为 PUT 的部分更新会把「请求体里的一对」和「落库后的一对」拆开:
// 只传 endpoint 不传 zone 时,请求体那一对是自洽的,落库后的那一对却可能跨了大区。
func validateZoneEndpoint(zone, endpoint string) string {
	// endpoint 必须是 go-ovh 认识的名字。以前不校验,写错的话 go-ovh 会静默当成 ovh-eu,
	// 于是美区账户的请求全打到欧洲站点,表现为"目录里没有这个机型 / 下单一直失败"
	if !IsKnownEndpoint(endpoint) {
		return "不支持的 endpoint: " + endpoint + "(可用: ovh-eu / ovh-us / ovh-ca / kimsufi-* / soyoustart-*)"
	}
	// 子公司必须在归属表里。表外的值(拼错、或 OVH 没这个子公司)会被 SubsidiaryRegion
	// 兜底成 EU,于是拿一个不存在的 ovhSubsidiary 去拉目录 —— OVH 直接 400,
	// 但错误发生在下单链路深处,看到的只是"取目录失败"。
	if !ovh.KnownSubsidiary(zone) {
		return "未知的子公司(zone): " + zone + "(EU 区: CZ DE ES EU FI FR GB IE IT LT MA NL PL PT SN TN;CA 区: ASIA AU CA IN QC SG WE WS;US 区: US)"
	}
	// zone 与 endpoint 必须同属一个大区:目录、价格、库存、购物车在 EU/US/CA 三个站点之间
	// 完全独立,zone=US 配 endpoint=ovh-eu 这种组合会一路走到下单才报错,且报错看不出根因。
	// 只比大区不比字符串,kimsufi-eu / soyoustart-eu 这类同大区的别名品牌照常可用。
	if endpointRegion(endpoint) != endpointRegion(endpointForZone(zone)) {
		return "子公司 " + zone + "(" + ovh.SubsidiaryRegion(zone) + " 区)与 endpoint " + endpoint +
			"(" + endpointRegion(endpoint) + " 区)不在同一大区(应使用 " + endpointForZone(zone) + " 或同大区的 kimsufi/soyoustart endpoint)"
	}
	return ""
}

func (in *accountInput) validate() string {
	if in.Name == "" {
		return "缺少 name"
	}
	if in.AppKey == "" || in.AppSecret == "" || in.ConsumerKey == "" {
		return "缺少 OVH 凭据 (appKey / appSecret / consumerKey)"
	}
	return validateZoneEndpoint(in.Zone, in.Endpoint)
}

// ── handlers ───────────────────────────────────────────────────────────────

// ListAccounts GET /api/accounts
func ListAccounts(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		accs, err := state.DB.ListAccounts()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if accs == nil {
			accs = []types.OVHAccount{}
		}
		c.JSON(http.StatusOK, gin.H{"accounts": accs, "total": len(accs)})
	}
}

// GetAccountByID GET /api/accounts/:id
func GetAccountByID(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		acc, ok, err := state.DB.GetAccount(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "账户不存在"})
			return
		}
		c.JSON(http.StatusOK, acc)
	}
}

// CreateAccount POST /api/accounts
// 创建后立即用新凭据调 OVH /me 验证,valid 一并返回。
// 注意:验证失败的账户仍然入库(只是 valid=false)——凭据填错时用户往往只想改一个字段,
// 直接删掉会让人重填一遍;前端据 valid 提示即可。
func CreateAccount(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in accountInput
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		in.normalize()
		if msg := in.validate(); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}

		// 没账户时第一个自动设默认。
		// COUNT 失败(比如 SQLITE_BUSY)时绝不能当成 0:UpsertAccount 见 is_default=1 会把
		// 其它账户的 is_default 清 0,等于让一次数据库抖动悄悄夺走默认账户身份,
		// 之后所有不带 ?account= 的调用(队列里空 AccountID 的下单)都会打到新账户上。
		// 这种情况退回内存里的账户表判断,内存也是空的才认为这是第一个账户。
		isDefault := in.SetDefault
		if count, err := state.DB.CountAccounts(); err != nil {
			state.Logger.Warn("统计账户数量失败,改用内存账户表判断是否设为默认: "+err.Error(), "accounts")
			isDefault = isDefault || !state.HasAnyAccount()
		} else if count == 0 {
			isDefault = true
		}

		acc := types.OVHAccount{
			ID:          uuid.NewString(),
			Name:        in.Name,
			Endpoint:    in.Endpoint,
			Zone:        in.Zone,
			AppKey:      in.AppKey,
			AppSecret:   in.AppSecret,
			ConsumerKey: in.ConsumerKey,
			IAM:         in.IAM,
			IsDefault:   isDefault,
			CreatedAt:   types.NowISO(),
		}
		if err := state.DB.UpsertAccount(acc); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		_ = state.ReloadAccounts()

		// 用新凭据验证
		valid, subsidiaryWarning := verifyAccountCreds(state, acc.ID)
		state.Logger.Info("创建账户: "+acc.Name+" ("+acc.Zone+"/"+ovh.SubsidiaryRegion(acc.Zone)+" 区) valid="+boolStr(valid), "accounts")

		// subsidiaryWarning 非空 = 凭据能用,但这个账户在 OVH 那边属于另一个子公司,
		// 目录/价格/下单 region 都会按填错的那个走。给前端原样提示,别让它在下单时才炸。
		c.JSON(http.StatusOK, gin.H{"account": acc, "valid": valid, "subsidiaryWarning": subsidiaryWarning})
	}
}

// UpdateAccount PUT /api/accounts/:id
func UpdateAccount(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		existing, ok, err := state.DB.GetAccount(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "账户不存在"})
			return
		}
		var in accountInput
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// normalize() 会给空 zone 兜底成 "IE" 并据此推出 endpoint/iam,
		// 所以"客户端到底传没传"必须在 normalize 之前记下来。否则一个省略 zone 的 PUT
		// (前端 hooks 的类型就是 Partial<AccountInput>)会把 ovh-us / ovh-ca 账户
		// 静默改成 IE / ovh-eu,之后该账户所有请求都拿 US 的 key 去打 EU,表现为"账户突然失效"。
		zoneProvided := strings.TrimSpace(in.Zone) != ""
		endpointProvided := strings.TrimSpace(in.Endpoint) != ""
		iamProvided := strings.TrimSpace(in.IAM) != ""
		in.normalize()
		// 允许部分更新:空字段保留原值
		acc := existing
		if in.Name != "" {
			acc.Name = in.Name
		}
		if zoneProvided {
			acc.Zone = in.Zone
			acc.Endpoint = in.Endpoint // 显式传了就用传的,否则 normalize 已按 zone 推好
			acc.IAM = in.IAM
		} else {
			// 没传 zone 时只更新客户端确实传了的那几个字段
			if endpointProvided {
				acc.Endpoint = in.Endpoint
			}
			if iamProvided {
				acc.IAM = in.IAM
			}
		}
		if in.AppKey != "" {
			acc.AppKey = in.AppKey
		}
		if in.AppSecret != "" {
			acc.AppSecret = in.AppSecret
		}
		if in.ConsumerKey != "" {
			acc.ConsumerKey = in.ConsumerKey
		}
		acc.IsDefault = acc.IsDefault || in.SetDefault

		// 校验的是「合并之后」的那一对,不是请求体里的那一对。
		// PUT {"endpoint":"ovh-us"}(不带 zone)在请求体里完全自洽,合并后却变成
		// zone=FR + endpoint=ovh-us —— 拿欧洲子公司的目录去打美区站点,账户从此每一次调用都错,
		// 而以前 UpdateAccount 根本不做任何校验,这种账户会一直躺在库里。
		if msg := validateZoneEndpoint(acc.Zone, acc.Endpoint); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}

		if err := state.DB.UpsertAccount(acc); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		state.OVH.Invalidate(acc.ID)
		invalidateOrderMappingCache(acc.ID) // 换了凭据/endpoint,旧缓存里的订单可能已经不属于这个账户了
		_ = state.ReloadAccounts()

		valid, subsidiaryWarning := verifyAccountCreds(state, acc.ID)
		c.JSON(http.StatusOK, gin.H{"account": acc, "valid": valid, "subsidiaryWarning": subsidiaryWarning})
	}
}

// DeleteAccountByID DELETE /api/accounts/:id  级联删除
func DeleteAccountByID(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if err := state.DB.DeleteAccount(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		state.OVH.Invalidate(id)
		invalidateOrderMappingCache(id) // 订单映射按账户缓存,账户没了缓存也得走
		_ = state.ReloadAccounts()
		// 关联的内存数据也得清掉(queue / history / sniper_tasks)
		reloadAfterAccountDelete(state, id)
		state.Logger.Info("删除账户 + 级联清理: "+id, "accounts")
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}

// SetDefaultAccountByID POST /api/accounts/:id/set-default
func SetDefaultAccountByID(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if err := state.DB.SetDefaultAccount(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		_ = state.ReloadAccounts()
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}

// VerifyAccount POST /api/accounts/:id/verify
func VerifyAccount(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		valid, subsidiaryWarning := verifyAccountCreds(state, id)
		c.JSON(http.StatusOK, gin.H{"valid": valid, "subsidiaryWarning": subsidiaryWarning})
	}
}

// ── 内部工具 ───────────────────────────────────────────────────────────────

// verifyAccountCreds 用账户凭据调 OVH /me 验证有效。
// 第二个返回值是子公司错配说明(空 = 没问题):/me 的 ovhSubsidiary 才是 OVH 认的归属,
// 而账户里存的 zone 决定了目录站点和下单 region —— 两者不一致时凭据本身有效(valid=true),
// 但这个账户的目录、价格、库存全是另一个子公司的。详见 SubsidiaryMismatchNote。
func verifyAccountCreds(state *app.State, accountID string) (bool, string) {
	cli, err := state.OVH.ClientFor(accountID)
	if err != nil {
		return false, ""
	}
	var me map[string]interface{}
	if err := cli.Get("/me", &me); err != nil {
		state.Logger.Warn("verify account "+accountID+": "+err.Error(), "accounts")
		return false, ""
	}
	acc, ok := state.FindAccount(accountID)
	if !ok {
		return true, ""
	}
	note := SubsidiaryMismatchNote(acc, me)
	if note != "" {
		state.Logger.Warn("账户 "+acc.Name+" 子公司配置与 OVH 实际归属不一致:"+note, "accounts")
	}
	return true, note
}

// reloadAfterAccountDelete 删账户后,把内存里关联的 queue/history/sniper_tasks
// 重新从 SQLite 加载(级联删除已经把这些行删掉了)
// monitorRef 由 main 注入。handlers 包里大多数函数按参数拿 *monitor.Monitor,
// 但删账户的级联清理埋在 helper 里,加参数要改一串签名,注入一次更省事。
var monitorRef *monitor.Monitor

// SetMonitorRef main 启动时调用一次
func SetMonitorRef(m *monitor.Monitor) { monitorRef = m }

func reloadAfterAccountDelete(state *app.State, accountID string) {
	if items, err := state.DB.ListQueue(); err == nil {
		state.QueueMu.Lock()
		state.Queue = items
		if state.Queue == nil {
			state.Queue = []types.QueueItem{}
		}
		state.QueueMu.Unlock()
	}
	if items, err := state.DB.ListHistory(); err == nil {
		state.HistoryMu.Lock()
		state.History = items
		if state.History == nil {
			state.History = []types.PurchaseHistoryEntry{}
		}
		state.HistoryMu.Unlock()
	}
	// 监控订阅的 auto_order_account_id 已经被 SQL UPDATE 清空,内存也必须同步清:
	// SaveToDB 是拿内存整表 Replace 回写的,不清内存的话,下一次任何订阅增删改
	// 都会把已删账户 ID 复活回数据库 —— 之前这里写着"由 monitor 包自己 LoadFromDB"
	// 但那个重载从未发生,级联清理等于白做
	if mon := monitorRef; mon != nil {
		if n := mon.ClearAccountRefs(accountID); n > 0 {
			state.Logger.Info(fmt.Sprintf("删账户 %s:已解除 %d 条监控订阅的自动下单绑定", accountID, n), "accounts")
		}
	}
	if subs, err := state.DB.ListVPSSubscriptions(); err == nil {
		state.VPSSubsMu.Lock()
		state.VPSSubscriptions = subs
		if state.VPSSubscriptions == nil {
			state.VPSSubscriptions = []types.VPSSubscription{}
		}
		state.VPSSubsMu.Unlock()
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

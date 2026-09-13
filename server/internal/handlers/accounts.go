package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/netfp"
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

	// ProxyURL / Fingerprint 用指针,为了把"没传"和"传了空串"分开。
	//
	// 别的字段用"空 = 保留原值"的约定,但那样代理就**永远清不掉** ——
	// 用户想从"走代理"改回"直连",发空串会被当成没传。
	// nil = 不改，"" = 清掉（改回直连），非空 = 换成这个。
	ProxyURL    *string `json:"proxyUrl"`
	Fingerprint *string `json:"fingerprint"`
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
	if in.ProxyURL != nil {
		if err := netfp.ValidateProxyURL(*in.ProxyURL); err != nil {
			// 在保存这一刻挡下来:放过去的话,用户要等到真有货那一刻才发现
			// 出口不对,而那正是唯一不能出错的时刻。
			return "代理地址不合法: " + err.Error()
		}
	}
	if in.Fingerprint != nil {
		if _, warn := netfp.LookupProfile(*in.Fingerprint); warn != "" {
			return warn
		}
	}
	if in.AppKey == "" || in.AppSecret == "" || in.ConsumerKey == "" {
		return "缺少 OVH 凭据 (appKey / appSecret / consumerKey)"
	}
	return validateZoneEndpoint(in.Zone, in.Endpoint)
}

// ── handlers ───────────────────────────────────────────────────────────────

// ListAccounts GET /api/accounts
// maskCred 把凭据打成掩码。只保留首尾各 3 位,够用户认出"这是哪一把",
// 又不足以拿去用。
func maskCred(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 8 {
		return "••••••••"
	}
	return v[:3] + "••••••" + v[len(v)-3:]
}

// sanitizeAccount 去掉明文凭据,换成掩码。
//
// 为什么必须这么做:GET /api/accounts 以前直接下发解密后的 AppKey / AppSecret /
// ConsumerKey 明文。配上「API_SECRET_KEY 未设时默认 123456」和当时的
// CORS AllowAllOrigins,构成一条完整的窃取链 —— 用户开着控制台时访问任意网页,
// 那个页面只要 fetch('http://127.0.0.1:19998/api/accounts',
// {headers:{'X-API-Key':'123456'}}) 就能读走全部 OVH 凭据,拿去下单/重装/删机器。
// 「只监听本地」挡不住这条路,浏览器本身就是攻击载体。
//
// 前端要明文只是为了编辑时回填输入框 —— 那个需求用「留空 = 保持原值」满足即可
// (UpdateAccount 本来就是这个语义),凭据没有任何理由离开后端。
func sanitizeAccount(a types.OVHAccount) types.OVHAccount {
	a.AppKey = maskCred(a.AppKey)
	a.AppSecret = maskCred(a.AppSecret)
	a.ConsumerKey = maskCred(a.ConsumerKey)
	// 代理串里常带 user:pass,和三个密钥同等对待 —— 只回显打过码的,
	// 但保留主机和端口,否则用户没法确认自己配的是哪个出口。
	a.ProxyURL = netfp.ScrubProxyURL(a.ProxyURL)
	return a
}

func ListAccounts(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		accs, err := state.DB.ListAccounts()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out := make([]types.OVHAccount, 0, len(accs))
		for _, a := range accs {
			out = append(out, sanitizeAccount(a))
		}
		c.JSON(http.StatusOK, gin.H{"accounts": out, "total": len(out)})
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
		c.JSON(http.StatusOK, sanitizeAccount(acc))
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
		if in.ProxyURL != nil {
			acc.ProxyURL = strings.TrimSpace(*in.ProxyURL)
		}
		if in.Fingerprint != nil {
			acc.Fingerprint = strings.TrimSpace(*in.Fingerprint)
		}
		if err := state.DB.UpsertAccount(acc); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		_ = state.ReloadAccounts()
		RefreshSharedProxy(state)

		// 用新凭据验证
		valid, subsidiaryWarning := verifyAccountCreds(state, acc.ID)
		state.Logger.Info("创建账户: "+acc.Name+" ("+acc.Zone+"/"+ovh.SubsidiaryRegion(acc.Zone)+" 区) valid="+boolStr(valid), "accounts")

		// subsidiaryWarning 非空 = 凭据能用,但这个账户在 OVH 那边属于另一个子公司,
		// 目录/价格/下单 region 都会按填错的那个走。给前端原样提示,别让它在下单时才炸。
		c.JSON(http.StatusOK, gin.H{"account": sanitizeAccount(acc), "valid": valid, "subsidiaryWarning": subsidiaryWarning})
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
		// 代理 / 指纹用指针判"传没传":nil = 不改，"" = 改回直连。
		// 改完下面会 Invalidate + ReloadAccounts,已缓存的 client 会带着
		// 新的出站配置重建 —— 中途换代理、或者从直连改成走代理,都能立刻生效。
		if in.ProxyURL != nil {
			acc.ProxyURL = strings.TrimSpace(*in.ProxyURL)
		}
		if in.Fingerprint != nil {
			acc.Fingerprint = strings.TrimSpace(*in.Fingerprint)
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

		// 解不开的密文绝不能被空串覆盖回去。
		//
		// rowToAccount 在解密失败时返回空串(为了让 ClientFor 明确报"缺少凭据",
		// 而不是把乱码发给 OVH 换回一句 Invalid signature)。但那个空串一路带到这里,
		// 用户只改个名字、三个凭据都留空 → acc 里是空串 → Encrypt("") 返回 "" →
		// UPDATE app_key='' —— 密文被永久毁掉,后来找回正确密钥也救不回来。
		//
		// 触发场景都不罕见:换机器只拷了 sniper.db、轮换过 OVH_DB_KEY、
		// 恢复了旧 .env 配新 db。而 main.go 那道"新生成密钥+库里有密文就拒绝启动"
		// 的保护在这里不生效 —— 密钥是存在的,只是不对。
		if acc.AppKey == "" || acc.AppSecret == "" || acc.ConsumerKey == "" {
			if raw, ok, _ := state.DB.GetAccountRaw(id); ok && raw.HasCiphertext() {
				c.JSON(http.StatusConflict, gin.H{
					"error": "这个账户的凭据当前解不开(密钥不匹配),为避免把密文覆盖掉,已拒绝保存。" +
						"请先恢复正确的 OVH_DB_KEY;确实找不回就在这里重新填写完整的三个凭据",
				})
				return
			}
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
		RefreshSharedProxy(state)

		valid, subsidiaryWarning := verifyAccountCreds(state, acc.ID)
		c.JSON(http.StatusOK, gin.H{"account": sanitizeAccount(acc), "valid": valid, "subsidiaryWarning": subsidiaryWarning})
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
		RefreshSharedProxy(state)
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
		RefreshSharedProxy(state)
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
		// 被级联删掉的任务里,可能有正跑在 PurchaseServer 中段的。
		// 光用新列表覆盖内存,那些协程收不到任何信号:
		// 队列处理器每轮是拿 state.Queue 的**快照**去复核"还在不在队列里"的,
		// 而它们已经不在快照里了 —— 那条复核永远轮不到它们。
		// 结果是协程拿着已删账户的凭据把整条建车链路跑完,一路 401/403。
		// 删单、清空队列、TG /cancel 三个入口都调了 MarkTaskDeleted,
		// 只有这里漏了。MarkTaskDeleted 会 cancel 它们的 ctx,
		// 正在进行的 OVH 调用当场中断。
		alive := make(map[string]struct{}, len(items))
		for _, it := range items {
			alive[it.ID] = struct{}{}
		}
		gone := []string{}
		for _, it := range state.Queue {
			if _, ok := alive[it.ID]; !ok {
				gone = append(gone, it.ID)
			}
		}
		state.Queue = items
		if state.Queue == nil {
			state.Queue = []types.QueueItem{}
		}
		state.QueueMu.Unlock()
		// 出锁再标记:MarkTaskDeleted 会同步调 cancel,不该占着 QueueMu
		for _, id := range gone {
			state.MarkTaskDeleted(id)
		}
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

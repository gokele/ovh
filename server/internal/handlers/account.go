package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

func noOVHRespAccount(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "未配置OVH API"})
}

// ── 带失败计数的并发详情拉取 ──────────────────────────────────────────────
//
// util.go 的 parallelGetDetails / parallelGetStringKeys 把失败位留成 nil,调用方一过滤
// 记录就凭空消失,接口照样 200,日志里的"成功 N 条"也统计不出少了几条。
// util.go 是公共文件不在本次改动范围,所以这里放一份带计数的版本:
// 拿不到的详情等于整条记录丢失,必须让日志和响应能看见。

const ovhDetailRetryDelay = 300 * time.Millisecond

// ovhErrIsTransient 429(OVH 对 /me 有速率限制,这里按 10 并发拉详情很容易撞上)和 5xx
// 属于重试有意义的瞬时错误;403/404 是权限或资源本身的问题,重试只是白白再烧一次配额。
// 非 APIError(连接超时/重置)同样按瞬时处理。
func ovhErrIsTransient(err error) bool {
	var apiErr *ovhsdk.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == http.StatusTooManyRequests || apiErr.Code >= 500
	}
	return true
}

// ovhGetWithRetry 对瞬时错误退避重试一次。宁可多花 300ms 也不要让一条记录悄悄消失。
func ovhGetWithRetry(client *ovhsdk.Client, path string, out interface{}) error {
	err := client.Get(path, out)
	if err == nil || !ovhErrIsTransient(err) {
		return err
	}
	time.Sleep(ovhDetailRetryDelay)
	return client.Get(path, out)
}

// parallelGetPaths 并发 GET 一批路径,结果按索引对齐(失败位 nil),
// 额外返回失败条数和第一个错误,供调用方上报。
func parallelGetPaths(client *ovhsdk.Client, paths []string, concurrency int) ([]map[string]interface{}, int, error) {
	if concurrency <= 0 {
		concurrency = 10
	}
	results := make([]map[string]interface{}, len(paths))
	errs := make([]error, len(paths))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			var d map[string]interface{}
			if err := ovhGetWithRetry(client, path, &d); err != nil {
				errs[idx] = err
				return
			}
			results[idx] = d
		}(i, p)
	}
	wg.Wait()
	failed := 0
	var firstErr error
	for _, e := range errs {
		if e != nil {
			failed++
			if firstErr == nil {
				firstErr = e
			}
		}
	}
	return results, failed, firstErr
}

// parallelGetDetailsCounted parallelGetDetails 的带计数版本
func parallelGetDetailsCounted(client *ovhsdk.Client, keys []interface{}, pathFn func(interface{}) string, concurrency int) ([]map[string]interface{}, int, error) {
	paths := make([]string, len(keys))
	for i, k := range keys {
		paths[i] = pathFn(k)
	}
	return parallelGetPaths(client, paths, concurrency)
}

// parallelGetStringsCounted parallelGetStringKeys 的带计数版本
func parallelGetStringsCounted(client *ovhsdk.Client, keys []string, pathFn func(string) string, concurrency int) ([]map[string]interface{}, int, error) {
	paths := make([]string, len(keys))
	for i, k := range keys {
		paths[i] = pathFn(k)
	}
	return parallelGetPaths(client, paths, concurrency)
}

// collectDetails 过滤掉失败位,并在有失败时写日志 + 带回响应头,
// 让"列表少了几条"这件事至少能被看见(refunds / email-history 前端要的是裸数组,
// 不能往 body 里塞字段,只能走 header)。
func collectDetails(state *app.State, c *gin.Context, details []map[string]interface{}, failed int, firstErr error, what, category string) []map[string]interface{} {
	list := []map[string]interface{}{}
	for _, d := range details {
		if d != nil {
			list = append(list, d)
		}
	}
	if failed > 0 {
		msg := ""
		if firstErr != nil {
			msg = firstErr.Error()
		}
		state.Logger.Warn(fmt.Sprintf("%s详情有 %d/%d 条拉取失败,返回列表不完整: %s", what, failed, len(details), msg), category)
		c.Header("X-Partial-Failures", strconv.Itoa(failed))
	}
	return list
}

// ── 账单 / 退款列表 ────────────────────────────────────────────────────────
//
// /me/bill 与 /me/refund 都只返回 id 数组(schema: string[]),对返回顺序没有任何承诺,
// 所以"最近 20 条"不能靠截断 id 列表得到,必须用 schema 提供的 date.from 过滤 +
// 详情里的 date 字段排序(billing.Bill.date / billing.Refund.date 都是必填 datetime)。
// date.from 在 EU / US / CA 三区的这两个端点上都是合法 query 参数(已逐一核对 schema)。

const (
	accountBillingListSize   = 20  // 最终返回给前端的条数
	accountBillingMaxDetails = 100 // 单次最多拉多少条详情,避免老账户把一次请求拖成分钟级
)

// billingListWindows date.from 的候选窗口(月),从窄到宽
var billingListWindows = []int{1, 3, 12, 36}

// fetchRecentBillingIDs 逐步放宽 date.from,第一个能凑够 want 条的窗口即采用;
// 所有窗口都不够(新账户)才退回不带过滤的全量列表。
// 先窄后宽是为了在账单密集的账户上少拉详情,同时保证拿到的是最近的而不是随机的一批。
//
// 第二个返回值是降级说明:窗口查询失败时不再直接报错,而是退回改动前的裸路径全量列表,
// 让调用方能记日志/提示,但页面照常能开。
// 原因:US 的 /me/refund、/me/bill 的 date.from 在 schema 里是 string 而不是 EU 的 datetime,
// OVH US 认不认 "2026-08-25T00:00:00+00:00" 这种写法无法离线确认;
// 而这两个端点是「退款记录」「账单」两个 tab 的唯一数据源,一旦被拒就是整页 500,
// 代价远大于"少一层服务端过滤、多拉几条详情"。
func fetchRecentBillingIDs(client *ovhsdk.Client, basePath string, want int) ([]string, string, error) {
	var ids []string
	warn := ""
	for _, months := range billingListWindows {
		from := time.Now().UTC().AddDate(0, -months, 0).Format("2006-01-02T15:04:05+00:00")
		var raw []interface{}
		if err := client.Get(basePath+"?date.from="+url.QueryEscape(from), &raw); err != nil {
			warn = fmt.Sprintf("date.from 窗口查询(%s)被拒绝,已降级为全量列表 + 本地排序: %s", from, err.Error())
			ids = nil
			break
		}
		ids = idsToStrings(raw)
		if len(ids) >= want {
			return ids, "", nil
		}
	}
	var raw []interface{}
	if err := client.Get(basePath, &raw); err != nil {
		return nil, warn, err
	}
	if all := idsToStrings(raw); len(all) > len(ids) {
		ids = all
	}
	// 全量列表 OVH 不承诺顺序,而下游只会取前 accountBillingMaxDetails 条拉详情,
	// 取错一批就等于"最近 20 条"里没有真正最近的。id 在这两个端点上都是随时间递增的,
	// 所以先按 id 倒序做个尽力而为的近似(拿到详情后仍会按 date 重新排,这里只影响取样)。
	sortBillingIDsDesc(ids)
	return ids, warn, nil
}

// sortBillingIDsDesc 账单/退款 id 倒序:能当整数比就按数值比(避免 "999" > "1000"),
// 否则退回"长的在前、同长按字典序倒序",对 OVH 常见的定长前缀 id 同样成立。
func sortBillingIDsDesc(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := ids[i], ids[j]
		na, ea := strconv.ParseInt(a, 10, 64)
		nb, eb := strconv.ParseInt(b, 10, 64)
		if ea == nil && eb == nil {
			return na > nb
		}
		if len(a) != len(b) {
			return len(a) > len(b)
		}
		return a > b
	})
}

// idsToStrings OVH 这几个列表端点 schema 写的是 string[],但历史上也出现过数字 id,
// 沿用 idToString 的宽松处理。
func idsToStrings(raw []interface{}) []string {
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s := idToString(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// sortBillingByDateDesc 按详情的 date 倒序。OVH 的 datetime 带时区偏移,
// 直接字符串比较会把不同偏移的同一时刻排错,所以先按时间解析,解析不出来才退回字符串比较。
func sortBillingByDateDesc(list []map[string]interface{}) {
	sort.SliceStable(list, func(i, j int) bool {
		a, _ := list[i]["date"].(string)
		b, _ := list[j]["date"].(string)
		ta, ea := parseFlexible(a)
		tb, eb := parseFlexible(b)
		if ea == nil && eb == nil {
			return ta.After(tb)
		}
		if ea == nil {
			return true
		}
		if eb == nil {
			return false
		}
		return a > b
	})
}

// SubsidiaryMismatchNote 比对「账户配置里的 zone」和「OVH 实际认的 ovhSubsidiary」。
//
// 为什么必须查:zone 是全项目区域推导的唯一输入 —— 它决定目录站点
// (ovh.CatalogBaseURLForSubsidiary)、eco 目录的 ovhSubsidiary 参数、
// 下单的 region 取值。而 nichandle.Nichandle.ovhSubsidiary(EU/US/CA 三区 schema 里都是必填)
// 才是 OVH 自己认的那一个。两者不一致有两档后果:
//   - 同大区不一致(配 FR、实际 IE):目录/价格是另一个国家的,下单价与页面价对不上
//   - 跨大区不一致(配 FR、实际 US):endpoint 也跟着推错,所有调用打到别的站点,
//     表现为"目录里没有这个机型 / 账户突然失效",根因完全看不出来
//
// 只提示不改写:改 zone 是用户对凭据归属的决定,后台偷偷改会让下一次下单换个国家的价格。
// 返回空字符串表示没问题(取不到 ovhSubsidiary 时也当没问题,不制造假警报)。
func SubsidiaryMismatchNote(acc types.OVHAccount, me map[string]interface{}) string {
	actual, _ := me["ovhSubsidiary"].(string)
	actual = strings.ToUpper(strings.TrimSpace(actual))
	if actual == "" {
		return ""
	}
	configured := strings.ToUpper(strings.TrimSpace(acc.Zone))
	if configured == "" {
		configured = ovh.DefaultSubsidiaryForEndpoint(acc.Endpoint)
	}
	if configured == actual {
		return ""
	}
	cfgRegion, actRegion := ovh.SubsidiaryRegion(configured), ovh.SubsidiaryRegion(actual)
	if cfgRegion != actRegion {
		return fmt.Sprintf("账户配置的子公司是 %s(%s 区),但 OVH 返回的 ovhSubsidiary 是 %s(%s 区)——"+
			"这两个区的目录、价格、库存、购物车完全独立,当前配置会把请求打到错误的站点,请把 zone 改成 %s 并使用 endpoint %s",
			configured, cfgRegion, actual, actRegion, actual, ovh.EndpointForSubsidiary(actual))
	}
	return fmt.Sprintf("账户配置的子公司是 %s,但 OVH 返回的 ovhSubsidiary 是 %s(同属 %s 区)——"+
		"站点没打错,但目录和价格会按 %s 取,与实际结算币种/价格可能不一致,建议把 zone 改成 %s",
		configured, actual, cfgRegion, configured, actual)
}

// GetAccountInfo GET /api/ovh/account/info
func GetAccountInfo(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		var info map[string]interface{}
		if err := client.Get("/me", &info); err != nil {
			state.Logger.Error("获取账户信息失败: "+err.Error(), "account_management")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取账户信息失败: " + err.Error()})
			return
		}
		// 这个接口的响应体是 OVH 的 nichandle 原样透传(前端按 OVH 字段渲染),
		// 不能往里塞自定义字段,所以子公司错配只能走 header + 日志。
		if acc, ok := ovhAccountFor(state, c); ok {
			if note := SubsidiaryMismatchNote(acc, info); note != "" {
				state.Logger.Warn("账户 "+acc.Name+" 子公司配置与 OVH 实际归属不一致:"+note, "account_management")
				c.Header("X-Subsidiary-Mismatch", "1")
			}
		}
		state.Logger.Info("成功获取账户信息", "account_management")
		c.JSON(http.StatusOK, info)
	}
}

// GetAccountRefunds GET /api/ovh/account/refunds
func GetAccountRefunds(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		ids, warn, err := fetchRecentBillingIDs(client, "/me/refund", accountBillingListSize)
		if err != nil {
			state.Logger.Error("获取退款列表失败: "+err.Error(), "account_management")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取退款列表失败: " + err.Error()})
			return
		}
		if warn != "" {
			state.Logger.Warn("退款列表"+warn, "account_management")
		}
		scan := len(ids)
		if scan > accountBillingMaxDetails {
			scan = accountBillingMaxDetails
			state.Logger.Warn(fmt.Sprintf("退款记录共 %d 条,只取前 %d 条排序", len(ids), scan), "account_management")
		}
		// 并发拉详情：10 并发,原 20 * 200ms = 4 秒 -> 2 * 200ms = 0.4 秒
		details, failed, firstErr := parallelGetStringsCounted(client, ids[:scan], func(s string) string {
			return "/me/refund/" + s
		}, 10)
		list := collectDetails(state, c, details, failed, firstErr, "退款", "account_management")
		sortBillingByDateDesc(list)
		if len(list) > accountBillingListSize {
			list = list[:accountBillingListSize]
		}
		state.Logger.Info(fmt.Sprintf("成功获取 %d 条退款记录(窗口内 %d 条,拉取失败 %d 条)", len(list), len(ids), failed), "account_management")
		c.JSON(http.StatusOK, list)
	}
}

// GetCreditBalance GET /api/ovh/account/credit-balance
func GetCreditBalance(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		var names []string
		if err := client.Get("/me/credit/balance", &names); err != nil {
			state.Logger.Error("获取信用余额失败: "+err.Error(), "account_management")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "获取信用余额失败: " + err.Error()})
			return
		}
		// 并发拉详情
		details, failed, firstErr := parallelGetStringsCounted(client, names, func(n string) string {
			return "/me/credit/balance/" + n
		}, 10)
		balances := collectDetails(state, c, details, failed, firstErr, "信用余额", "account_management")
		state.Logger.Info(fmt.Sprintf("成功获取 %d 个信用余额(共 %d 个,失败 %d 个)", len(balances), len(names), failed), "account_management")
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": balances, "total": len(names), "failed": failed})
	}
}

// GetEmailHistory GET /api/ovh/account/email-history
func GetEmailHistory(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		var ids []interface{}
		if err := client.Get("/me/notification/email/history", &ids); err != nil {
			state.Logger.Error("获取邮件历史失败: "+err.Error(), "account_management")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取邮件历史失败: " + err.Error()})
			return
		}
		// 倒序
		for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
			ids[i], ids[j] = ids[j], ids[i]
		}
		max := 50
		if len(ids) < max {
			max = len(ids)
		}
		// 并发拉 50 封邮件详情：原 50 * 200ms = 10 秒 -> 5 * 200ms = 1 秒
		details, failed, firstErr := parallelGetDetailsCounted(client, ids[:max], func(k interface{}) string {
			return "/me/notification/email/history/" + idToString(k)
		}, 10)
		list := collectDetails(state, c, details, failed, firstErr, "邮件", "account_management")
		// 口径:请求了 max 封、失败 failed 封。len(ids) 是账户的全量历史(可能上千),
		// 写成"总共"会让人以为差额是丢了,其实是我们主动只取最近 max 封。
		state.Logger.Info(fmt.Sprintf("成功获取 %d 封邮件(本次请求最近 %d 封,失败 %d 封;账户历史共 %d 封)",
			len(list), max, failed, len(ids)), "account_management")
		c.JSON(http.StatusOK, list)
	}
}

// contactChangeUnsupported US 区没有 /me/task/contactChange 系列端点:
// EU / CA schema 有全部 5 条(contactChange 本体 + accept/refuse/resendEmail),
// US 的 /me 只有 emailChange —— 本地 schema 与 live api.us.ovhcloud.com/1.0/me.json 都确认。
// 直接告诉调用方"这个区没有这个能力",不要把 OVH 的 404 包成 500 内部错误让人以为后端坏了。
//
// 判大区走 ovh.EndpointRegion 而不是比 "ovh-us" 字符串:endpoint 是用户可填字段,
// 大小写、以及将来可能出现的同大区别名都该按同一份归属表判断。
func contactChangeUnsupported(state *app.State, c *gin.Context) bool {
	acc, ok := ovhAccountFor(state, c)
	if !ok || ovh.EndpointRegion(acc.Endpoint) != "US" {
		return false
	}
	state.Logger.Warn("账户 "+acc.Name+" 属于 US 区,OVH US API 不提供联系人变更(contactChange)能力", "server_control")
	c.JSON(http.StatusNotImplemented, gin.H{
		"status":  "unsupported",
		"message": "OVH US 区不支持联系人变更请求,该功能仅在 EU / CA 区可用",
	})
	return true
}

// parseContactTaskID schema 里 id 是必填 long,0 / 负数 / 非数字都不是合法任务 id。
// 早点回 400,别拿 0 去打 OVH 换回一个 404 再包成 500(accept/refuse 还会顺带把用户的 token 发出去)。
func parseContactTaskID(c *gin.Context) (int64, bool) {
	taskID, err := strconv.ParseInt(c.Param("task_id"), 10, 64)
	if err != nil || taskID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "任务 ID 无效:必须是正整数"})
		return 0, false
	}
	return taskID, true
}

// GetContactChangeRequests GET /api/ovh/contact-change-requests
func GetContactChangeRequests(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contactChangeUnsupported(state, c) {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		var ids []interface{}
		if err := client.Get("/me/task/contactChange", &ids); err != nil {
			state.Logger.Error("获取联系人变更请求列表失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "获取联系人变更请求列表失败: " + err.Error()})
			return
		}
		// 并发拉详情
		details, failed, firstErr := parallelGetDetailsCounted(client, ids, func(k interface{}) string {
			return "/me/task/contactChange/" + idToString(k)
		}, 10)
		list := collectDetails(state, c, details, failed, firstErr, "联系人变更请求", "server_control")
		sort.SliceStable(list, func(i, j int) bool {
			a, _ := list[i]["dateRequest"].(string)
			b, _ := list[j]["dateRequest"].(string)
			return a > b
		})
		state.Logger.Info(fmt.Sprintf("成功获取 %d 个联系人变更请求(共 %d 个,失败 %d 个)", len(list), len(ids), failed), "server_control")
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": list, "total": len(ids), "failed": failed})
	}
}

// GetContactChangeRequestDetail GET /api/ovh/contact-change-requests/:task_id
func GetContactChangeRequestDetail(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contactChangeUnsupported(state, c) {
			return
		}
		taskID, ok := parseContactTaskID(c)
		if !ok {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		var d map[string]interface{}
		if err := client.Get(fmt.Sprintf("/me/task/contactChange/%d", taskID), &d); err != nil {
			state.Logger.Error(fmt.Sprintf("获取联系人变更请求 %d 详情失败: %s", taskID, err.Error()), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "获取联系人变更请求详情失败: " + err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("成功获取联系人变更请求 %d 详情", taskID), "server_control")
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": d})
	}
}

// AcceptContactChangeRequest POST /api/ovh/contact-change-requests/:task_id/accept
func AcceptContactChangeRequest(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contactChangeUnsupported(state, c) {
			return
		}
		taskID, ok := parseContactTaskID(c)
		if !ok {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		var body struct {
			Token string `json:"token"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Token == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "缺少必需的 token 参数。请从邮件中获取 token 并输入。"})
			return
		}
		if err := client.Post(fmt.Sprintf("/me/task/contactChange/%d/accept", taskID), map[string]interface{}{
			"token": body.Token,
		}, nil); err != nil {
			state.Logger.Error(fmt.Sprintf("接受联系人变更请求 %d 失败: %s", taskID, err.Error()), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "接受联系人变更请求失败: " + err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("成功接受联系人变更请求 %d", taskID), "server_control")
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "联系人变更请求已接受"})
	}
}

// RefuseContactChangeRequest POST /api/ovh/contact-change-requests/:task_id/refuse
func RefuseContactChangeRequest(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contactChangeUnsupported(state, c) {
			return
		}
		taskID, ok := parseContactTaskID(c)
		if !ok {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		var body struct {
			Token string `json:"token"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Token == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "缺少必需的 token 参数。请从邮件中获取 token 并输入。"})
			return
		}
		if err := client.Post(fmt.Sprintf("/me/task/contactChange/%d/refuse", taskID), map[string]interface{}{
			"token": body.Token,
		}, nil); err != nil {
			state.Logger.Error(fmt.Sprintf("拒绝联系人变更请求 %d 失败: %s", taskID, err.Error()), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "拒绝联系人变更请求失败: " + err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("成功拒绝联系人变更请求 %d", taskID), "server_control")
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "联系人变更请求已拒绝"})
	}
}

// ResendContactChangeEmail POST /api/ovh/contact-change-requests/:task_id/resend-email
func ResendContactChangeEmail(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contactChangeUnsupported(state, c) {
			return
		}
		taskID, ok := parseContactTaskID(c)
		if !ok {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		if err := client.Post(fmt.Sprintf("/me/task/contactChange/%d/resendEmail", taskID), map[string]interface{}{}, nil); err != nil {
			state.Logger.Error(fmt.Sprintf("重发联系人变更请求 %d 邮件失败: %s", taskID, err.Error()), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "重发邮件失败: " + err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("成功重发联系人变更请求 %d 的邮件", taskID), "server_control")
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "确认邮件已重新发送"})
	}
}

// GetSubAccounts GET /api/ovh/account/sub-accounts
func GetSubAccounts(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		var ids []interface{}
		if err := client.Get("/me/subAccount", &ids); err != nil {
			state.Logger.Error("获取子账户列表失败: "+err.Error(), "account_management")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "获取子账户列表失败: " + err.Error()})
			return
		}
		// 并发拉详情
		details, failed, firstErr := parallelGetDetailsCounted(client, ids, func(k interface{}) string {
			return "/me/subAccount/" + idToString(k)
		}, 10)
		list := collectDetails(state, c, details, failed, firstErr, "子账户", "account_management")
		state.Logger.Info(fmt.Sprintf("成功获取 %d 个子账户(共 %d 个,失败 %d 个)", len(list), len(ids), failed), "account_management")
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": list, "total": len(ids), "failed": failed})
	}
}

// GetAccountBills GET /api/ovh/account/bills
func GetAccountBills(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		ids, warn, err := fetchRecentBillingIDs(client, "/me/bill", accountBillingListSize)
		if err != nil {
			state.Logger.Error("获取账单列表失败: "+err.Error(), "account_management")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "获取账单列表失败: " + err.Error()})
			return
		}
		if warn != "" {
			state.Logger.Warn("账单列表"+warn, "account_management")
		}
		scan := len(ids)
		truncated := false
		if scan > accountBillingMaxDetails {
			scan = accountBillingMaxDetails
			truncated = true
			state.Logger.Warn(fmt.Sprintf("账单共 %d 条,只取前 %d 条排序", len(ids), scan), "account_management")
		}
		// 并发拉账单详情
		details, failed, firstErr := parallelGetStringsCounted(client, ids[:scan], func(s string) string {
			return "/me/bill/" + s
		}, 10)
		list := collectDetails(state, c, details, failed, firstErr, "账单", "account_management")
		sortBillingByDateDesc(list)
		if len(list) > accountBillingListSize {
			list = list[:accountBillingListSize]
		}
		state.Logger.Info(fmt.Sprintf("成功获取 %d 条账单记录(窗口内 %d 条,拉取失败 %d 条)", len(list), len(ids), failed), "account_management")
		c.JSON(http.StatusOK, gin.H{
			"status":    "success",
			"data":      list,
			"total":     len(ids),
			"failed":    failed,
			"truncated": truncated,
			// warning 为空字符串表示走的是正常窗口查询;非空说明是降级路径,前端可原样提示
			"warning": warn,
		})
	}
}

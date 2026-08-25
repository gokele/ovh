package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

// ovhClientFor 从请求 ?account=xxx 取账户 ID 拿对应 OVH client;
// 空时(没传 ?account)走默认账户;凭据缺失返回 error,调用方按原 Client() 错误流程处理。
// 大部分 handler 都是 `state.OVH.Client()` 模式,这个 helper 是 1:1 替换,
// 把单账户改成多账户路由,语义最小化变化。
func ovhClientFor(state *app.State, c *gin.Context) (*ovhsdk.Client, error) {
	return state.OVH.ClientFor(c.Query("account"))
}

// ovhAccountFor 从请求 ?account=xxx 取账户实体(给需要原始凭据/endpoint 的 raw HTTP 调用用)。
// 空 → 默认账户;不存在 → ok=false。
func ovhAccountFor(state *app.State, c *gin.Context) (types.OVHAccount, bool) {
	return state.FindAccount(c.Query("account"))
}

// knownEndpoints go-ovh 支持的 endpoint 名 → REST API base URL。
// 账户的 endpoint 是用户可填的自由字符串,不在这张表里的值一律视为非法 ——
// 以前的 default 分支会把 US/CA 账户的手写 HTTP 请求悄悄打到 eu.api.ovh.com,
// 拿回来的是"另一个大区的空数据",比直接报错更难排查。
var knownEndpoints = map[string]string{
	"ovh-eu":        "https://eu.api.ovh.com",
	"ovh-us":        "https://api.us.ovhcloud.com",
	"ovh-ca":        "https://ca.api.ovh.com",
	"kimsufi-eu":    "https://eu.api.kimsufi.com",
	"kimsufi-ca":    "https://ca.api.kimsufi.com",
	"soyoustart-eu": "https://eu.api.soyoustart.com",
	"soyoustart-ca": "https://ca.api.soyoustart.com",
}

// IsKnownEndpoint 账户创建/更新时校验 endpoint 合法性用
func IsKnownEndpoint(endpoint string) bool {
	_, ok := knownEndpoints[endpoint]
	return ok
}

// ovhAPIBaseURL 把 endpoint 映射成 OVH REST API base URL。
// 未知 endpoint 返回 ok=false,调用方必须报错而不是猜一个大区。
func ovhAPIBaseURLChecked(endpoint string) (string, bool) {
	u, ok := knownEndpoints[endpoint]
	return u, ok
}

// ovhAPIBaseURL 兼容旧调用点:未知 endpoint 仍回落 EU,但会被 ovhAPIBaseURLChecked
// 的调用方拦在前面。新代码请用 ovhAPIBaseURLChecked。
func ovhAPIBaseURL(endpoint string) string {
	if u, ok := knownEndpoints[endpoint]; ok {
		return u
	}
	return "https://eu.api.ovh.com"
}

// parallelGetDetails 通用并发 GET helper。对 keys[i] 用 pathFn(keys[i]) 拼出路径，
// 并发拉到 detail。最多 concurrency 个并发。结果按索引对齐，失败位 nil。
// 这是 1:1 串行 for 循环的并发替代版本，仅把网络 IO 并发化。
func parallelGetDetails(client *ovhsdk.Client, keys []interface{}, pathFn func(interface{}) string, concurrency int) []map[string]interface{} {
	if concurrency <= 0 {
		concurrency = 10
	}
	results := make([]map[string]interface{}, len(keys))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, key interface{}) {
			defer wg.Done()
			defer func() { <-sem }()
			var d map[string]interface{}
			if err := client.Get(pathFn(key), &d); err == nil {
				results[idx] = d
			}
		}(i, k)
	}
	wg.Wait()
	return results
}

// parallelGetStrings string 版本简化调用
func parallelGetStringKeys(client *ovhsdk.Client, keys []string, pathFn func(string) string, concurrency int) []map[string]interface{} {
	if concurrency <= 0 {
		concurrency = 10
	}
	results := make([]map[string]interface{}, len(keys))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, key string) {
			defer wg.Done()
			defer func() { <-sem }()
			var d map[string]interface{}
			if err := client.Get(pathFn(key), &d); err == nil {
				results[idx] = d
			}
		}(i, k)
	}
	wg.Wait()
	return results
}

// parallelGetDetailsWithErrs 与 parallelGetDetails 同构,但把每个 key 的错误一并带回。
// 语义保证:results[i] == nil ⟺ errs[i] != nil —— OVH 返回 200 但 body 是 null 时
// 也记成错误,调用方才能区分"这条没有数据"和"这条没拉到"。
//
// 老的 parallelGetDetails 把失败静默置 nil,调用方只能 continue 丢条目,
// 于是 403/429/5xx 会以"列表变短 + success:true"的形式呈现给用户。新代码一律用这个版本。
func parallelGetDetailsWithErrs(client *ovhsdk.Client, keys []interface{}, pathFn func(interface{}) string, concurrency int) ([]map[string]interface{}, []error) {
	if concurrency <= 0 {
		concurrency = 10
	}
	results := make([]map[string]interface{}, len(keys))
	errs := make([]error, len(keys))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, key interface{}) {
			defer wg.Done()
			defer func() { <-sem }()
			var d map[string]interface{}
			if err := client.Get(pathFn(key), &d); err != nil {
				errs[idx] = err
				return
			}
			if d == nil {
				errs[idx] = fmt.Errorf("OVH 返回空响应体")
				return
			}
			results[idx] = d
		}(i, k)
	}
	wg.Wait()
	return results, errs
}

// parallelGetStringKeysWithErrs string key 版本,语义同上
func parallelGetStringKeysWithErrs(client *ovhsdk.Client, keys []string, pathFn func(string) string, concurrency int) ([]map[string]interface{}, []error) {
	iface := make([]interface{}, len(keys))
	for i, k := range keys {
		iface[i] = k
	}
	return parallelGetDetailsWithErrs(client, iface, func(v interface{}) string {
		s, _ := v.(string)
		return pathFn(s)
	}, concurrency)
}

// countErrs 统计失败条数并返回第一个错误,给 handler 汇报 partial 用
func countErrs(errs []error) (failed int, first error) {
	for _, e := range errs {
		if e != nil {
			failed++
			if first == nil {
				first = e
			}
		}
	}
	return
}

// ovhAPICode 取 OVH SDK 错误里的 HTTP 状态码;不是 OVH API 错误时返回 0。
// 错误降级必须判状态码而不是匹配错误文案 —— 文案会随 OVH 改版失效,
// 一旦匹配不上就会把 401/500 当成"该功能未开通"骗过用户。
func ovhAPICode(err error) int {
	var apiErr *ovhsdk.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return 0
}

// ovhIsNotFound OVH 是否明确回了 404
func ovhIsNotFound(err error) bool { return ovhAPICode(err) == http.StatusNotFound }

// defaultZero 数字字段缺失时返回 0 而不是 null（OVH 偶尔不返回某些字段）
func defaultZero(v interface{}) interface{} {
	if v == nil {
		return 0
	}
	return v
}

// defaultObj 字典字段缺失时返回 {} 而不是 null
func defaultObj(v interface{}) interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}

// defaultArr 数组字段缺失时返回 [] 而不是 null
func defaultArr(v interface{}) interface{} {
	if v == nil {
		return []interface{}{}
	}
	return v
}

// isNonEmptyStorage 对应 Python "if data.get('storageConfig'):" 的 falsy 语义
// 空数组 / 空字典 / nil / false / 0 都视为"未提供自定义 storage"
func isNonEmptyStorage(v interface{}) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case []interface{}:
		return len(x) > 0
	case map[string]interface{}:
		return len(x) > 0
	case string:
		return x != ""
	}
	return true
}

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// idToString 把 OVH 返回的 ID（可能是 string 或数字）转成字符串
// 用于 /me/refund、/me/bill、/me/task/* 等返回数组里既可能是 ["xx","yy"] 也可能是 [1,2] 的端点
func idToString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	default:
		return fmt.Sprintf("%v", x)
	}
}

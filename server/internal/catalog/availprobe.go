package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
)

// 判断一个 planCode 属于哪个大区,必须以 /dedicated/server/datacenter/availabilities 为准,
// **不能**用 eco 公开目录的"有没有这个 planCode"来判。
//
// 实测(2026-08):EU 站点的 availabilities 出现过 244 个 planCode,而 /order/catalog/public/eco
// 只有 99 个 —— 差集里整条 Scale / HCI / SDS / High-Grade 产品线都在,其中一百多个当前就有库存。
// 拿 eco 目录当判据会把这些机型判成"不属于任何已配置大区",监控直接不再打 OVH,
// 从"能用"退化成"永久静默"—— 比它要修的原始 bug 更糟。
//
// eco 目录只适合回答"这个机型能不能用 /order/cart/{id}/eco 下单",不适合回答"它属于哪个区"。

const (
	availProbeTTL = 10 * time.Minute
	// availProbeErrTTL 探测失败后的静默期。失败结论不进正缓存(见下方注释),
	// 但也不能每 5 秒一轮地重试:OVH 侧一抖动,N 个订阅 × 候选大区数的请求会同时压上去,
	// 把一次抖动放大成持续 429。15 秒 ≈ 3 个监控轮次,恢复得够快,又能压住风暴。
	availProbeErrTTL = 15 * time.Second
)

type availProbeEntry struct {
	has bool // 该大区的可用性接口有没有这个机型的记录
	at  time.Time
}

// availProbeCall 自己实现的 singleflight:同一个 (planCode, 大区) 同时只探一次。
// 一个 planCode 常被多个订阅同时监控(还有 maxWorkers 并发),没有这层去重时
// 每轮都会对同一个机型重复打三个站点的可用性接口。
type availProbeCall struct {
	done chan struct{}
	has  bool
	err  error
}

// availProbeFailure 失败负缓存条目
type availProbeFailure struct {
	err error
	at  time.Time
}

var (
	availProbeMu    sync.Mutex
	availProbeCache = map[string]availProbeEntry{}   // planCode+大区 → 该区有无记录
	availProbeFail  = map[string]availProbeFailure{} // 探测键 → 上次失败
	availProbeCalls = map[string]*availProbeCall{}   // 探测键 → 在途探测
)

// probeRegionHasPlan 用公开的可用性接口探一个 planCode 在某大区站点有没有记录。
// 该接口无需凭据(实测三站点 curl 直接可取),所以探测不消耗任何账户配额。
// 写成变量是为了让测试能换成假实现,用调用次数验证"同一机型不会被并发重复探"。
var probeRegionHasPlan = func(region, planCode string) (bool, error) {
	q := url.Values{}
	q.Set("planCode", planCode)
	reqURL := ovh.APIBaseURLForRegion(region) + "/v1/dedicated/server/datacenter/availabilities?" + q.Encode()

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(reqURL)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var arr []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		return false, err
	}
	return len(arr) > 0, nil
}

// RegionOfPlan 探测 planCode 属于哪个大区(只在传入的候选大区里找)。
// 返回:
//   - region 非空:该大区的可用性接口有这个机型的记录
//   - region 为空 + err == nil:候选大区都查不到(机型下架 / planCode 拼错 / 属于没配账户的大区)
//   - err != nil:探测过程本身失败,调用方**不要**据此下结论
//
// 结果缓存 10 分钟:机型归属几乎不变,而监控是 5 秒一轮,不能每轮都探。
func RegionOfPlan(state *app.State, planCode string, candidateRegions []string) (string, error) {
	regions := dedupRegions(candidateRegions)
	var lastErr error
	for _, r := range regions {
		has, err := regionHasPlan(state, planCode, r)
		if err != nil {
			lastErr = err
			continue
		}
		if has {
			return r, nil
		}
	}
	if lastErr != nil {
		// 有站点没探成 —— 不能说"哪个区都没有",调用方必须按"未知"处理
		return "", lastErr
	}
	return "", nil
}

// regionHasPlan 某个大区的可用性接口有没有这个机型的记录。
//
// 缓存粒度是 (机型, 大区) 而不是 (机型, 候选集):
// 结论"US 站点有没有 X"本身与调用方问了哪几个区无关。早先按 planCode 单键缓存
// 导致过一个真实事故 —— 只配欧区账户的用户,候选集是 [EU],探不到就把"空"写进
// planCode 单键;随后候选集为 [EU US CA] 的调用直接命中那个"空",于是一个明明
// 在美区存在的机型被判成"planCode 拼错/已下架",下单侧据此判 Fatal 把任务永久置失败。
func regionHasPlan(state *app.State, planCode, region string) (bool, error) {
	key := planCode + "\x00" + region

	availProbeMu.Lock()
	if e, ok := availProbeCache[key]; ok && time.Since(e.at) < availProbeTTL {
		availProbeMu.Unlock()
		return e.has, nil
	}
	if f, ok := availProbeFail[key]; ok && time.Since(f.at) < availProbeErrTTL {
		// 刚探失败过,静默期内直接复用上次的错误,不再打 OVH
		availProbeMu.Unlock()
		return false, f.err
	}
	if call, ok := availProbeCalls[key]; ok {
		// 同一个 (机型, 大区) 正在被别的订阅探,等它的结果就好
		availProbeMu.Unlock()
		<-call.done
		return call.has, call.err
	}
	call := &availProbeCall{done: make(chan struct{})}
	availProbeCalls[key] = call
	availProbeMu.Unlock()

	has, err := probeRegionHasPlan(region, planCode)
	if err != nil {
		state.Logger.Debug(fmt.Sprintf("[region探测] %s @ %s 失败: %s", planCode, region, err.Error()), "monitor")
	}

	availProbeMu.Lock()
	delete(availProbeCalls, key)
	if err != nil {
		availProbeFail[key] = availProbeFailure{err: err, at: time.Now()}
	} else {
		availProbeCache[key] = availProbeEntry{has: has, at: time.Now()}
		delete(availProbeFail, key)
	}
	availProbeMu.Unlock()

	call.has, call.err = has, err
	close(call.done)
	return has, err
}

// dedupRegions 归一化候选大区:去空、大写、去重,顺序保持调用方给的优先级。
func dedupRegions(candidateRegions []string) []string {
	out := make([]string, 0, len(candidateRegions))
	seen := map[string]struct{}{}
	for _, r := range candidateRegions {
		r = strings.ToUpper(strings.TrimSpace(r))
		if r == "" {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

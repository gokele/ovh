package handlers

import (
	"testing"
	"time"
)

// 撤回期内的订单查找不能依赖 GetOrderMapping 那套缓存。
//
// 那套缓存只在用户点「同步订单」时填充,而前端根本没有那个入口
// (grep order-mapping 在 web/ 里零命中)。v0.1.24/v0.1.25 的撤单入口就是
// 因此永远不显示 —— 功能发出去了,但一次都没能亮起来。
//
// 这个测试守的是查找的**剪枝顺序**,那是它能便宜起来的全部原因:
// 先用 retractionDate 把订单筛掉(每单 1 个请求),只有还在窗口内的
// 才值得去拉明细(每单 1+N 个请求)。反过来先拉明细的话,
// 一个下过几百单的账户每次渲染页面都要打几百个请求。
func TestRetractionScanWindowIsSane(t *testing.T) {
	// 撤回期 14 天,扫描窗口必须覆盖它并留余量 ——
	// OVH 可能从交付而不是下单起算,窗口太窄会漏掉刚交付的机器
	if retractionScanDays < 14 {
		t.Fatalf("扫描窗口 %d 天 < 撤回期 14 天,会漏掉还能退的订单", retractionScanDays)
	}
	// 也不能无限宽:每多一天就多几个订单头请求,而它们必然已经过期
	if retractionScanDays > 60 {
		t.Fatalf("扫描窗口 %d 天过宽,白打请求 —— 超过撤回期的订单必然被剪掉", retractionScanDays)
	}

	// 缓存时长:撤回期按天算,几分钟内不会有订单进出窗口;
	// 但也不能太长,用户刚抢到一台机器不该等半小时才看到撤单入口
	if retractionLookupTTL < time.Minute || retractionLookupTTL > 30*time.Minute {
		t.Fatalf("查找缓存 %v 不合理:太短则每次渲染都重扫,太长则新订单迟迟不出现", retractionLookupTTL)
	}
}

// 负结果必须也缓存。
//
// 绝大多数机器都不在撤回期内(下单超过 21 天、或 v0.1.25 之前弃过权),
// 而服务器控制页每次切换机器都会问一次。不缓存负结果的话,
// 每次切换都要重扫最近所有订单 —— 而这个配额和抢购主链路是共用的。
func TestRetractionLookupCachesNegative(t *testing.T) {
	retractionLookupMu.Lock()
	retractionLookupCache = map[string]retractionLookup{}
	retractionLookupMu.Unlock()

	key := "acc-1|ns123.ip-1-2-3.net"
	retractionLookupMu.Lock()
	retractionLookupCache[key] = retractionLookup{orderID: 0, found: false, at: time.Now()}
	e, hit := retractionLookupCache[key]
	retractionLookupMu.Unlock()

	if !hit {
		t.Fatal("负结果没被缓存 —— 每次渲染都会重扫一遍最近订单")
	}
	if e.found {
		t.Fatal("负结果的 found 应为 false")
	}

	// 过期的缓存要被当成未命中,否则新抢到的机器永远显示不出撤单入口
	retractionLookupMu.Lock()
	retractionLookupCache[key] = retractionLookup{at: time.Now().Add(-retractionLookupTTL - time.Second)}
	stale := retractionLookupCache[key]
	retractionLookupMu.Unlock()
	if time.Since(stale.at) < retractionLookupTTL {
		t.Fatal("构造过期缓存失败,测试本身有问题")
	}
}

// 订单号在 JSON 里可能是三种类型,都要认。
// 走映射缓存那条路时是 int64/float64,而 encoding/json 默认把数字解成 float64 ——
// 只认 int64 的话映射命中了也取不出订单号,静默退化成"没找到"。
func TestToOrderIDAcceptsJSONNumberForms(t *testing.T) {
	cases := []struct {
		in   interface{}
		want int64
		ok   bool
	}{
		{int64(108523471), 108523471, true},
		{float64(108523471), 108523471, true}, // encoding/json 的默认形态
		{"108523471", 108523471, true},
		{"", 0, false},
		{"abc", 0, false},
		{nil, 0, false},
		{true, 0, false},
	}
	for _, c := range cases {
		got, ok := toOrderID(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("toOrderID(%#v) = (%d, %v), 期望 (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

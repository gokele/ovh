package catalog

import (
	"errors"
	"sync"
	"testing"
)

// 缓存粒度必须是 (机型, 大区)。曾经按 planCode 单键缓存,结果:
// 只配欧区账户的用户候选集是 [EU],探不到就把"空"写进单键;随后候选集
// [EU US CA] 的调用直接命中那个"空",一个明明在美区存在的机型被判成
// "planCode 拼错/已下架",下单侧据此 Fatal 把任务永久置失败。
func TestRegionOfPlan_窄候选集不污染宽候选集(t *testing.T) {
	resetAvailProbeCaches()
	orig := probeRegionHasPlan
	defer func() { probeRegionHasPlan = orig }()

	var mu sync.Mutex
	calls := map[string]int{}
	probeRegionHasPlan = func(region, planCode string) (bool, error) {
		mu.Lock()
		calls[region]++
		mu.Unlock()
		return region == "US", nil // 只有美区有这个机型
	}

	// 先用窄候选集问(模拟只配了欧区账户)
	if r, err := RegionOfPlan(testState(t), "24sk202-us", []string{"EU"}); err != nil || r != "" {
		t.Fatalf("窄候选集应返回空, 得到 %q err=%v", r, err)
	}
	// 再用全量候选集问:必须真的去探 US,而不是复用上一次的"空"
	r, err := RegionOfPlan(testState(t), "24sk202-us", []string{"EU", "US", "CA"})
	if err != nil {
		t.Fatalf("全量候选集报错: %v", err)
	}
	if r != "US" {
		t.Fatalf("全量候选集应探到 US, 得到 %q(窄候选集的结论污染了正缓存)", r)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["EU"] != 1 {
		t.Errorf("EU 应只探一次(第二次命中缓存), 实际 %d", calls["EU"])
	}
	if calls["US"] != 1 {
		t.Errorf("US 应被真正探一次, 实际 %d", calls["US"])
	}
}

// 探测失败不能被当成"该区没有",否则一次网络抖动就会把订阅按死
func TestRegionOfPlan_探测失败不写正缓存(t *testing.T) {
	resetAvailProbeCaches()
	orig := probeRegionHasPlan
	defer func() { probeRegionHasPlan = orig }()

	fail := errors.New("HTTP 429")
	probeRegionHasPlan = func(region, planCode string) (bool, error) { return false, fail }

	if _, err := RegionOfPlan(testState(t), "x", []string{"EU", "US"}); err == nil {
		t.Fatal("全部探测失败时必须返回错误,不能返回'哪都没有'")
	}
}

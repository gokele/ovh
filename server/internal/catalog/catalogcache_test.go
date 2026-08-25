package catalog

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/logger"
)

// testState 一个只带 Logger 的最小 State:这一层的代码只用到 Logger 和账户表。
func testState(t *testing.T) *app.State {
	t.Helper()
	lg := logger.New(filepath.Join(t.TempDir(), "logs.json"),
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	return &app.State{Logger: lg}
}

// resetCatalogCaches 每个用例开跑前清空三张全局表(它们是包级的)。
func resetCatalogCaches() {
	regionCacheMu.Lock()
	regionCache = map[string]*subsidiaryCatalog{}
	regionCacheFail = map[string]catalogFailure{}
	regionCacheCall = map[string]*catalogCall{}
	regionCacheMu.Unlock()
}

func resetAvailProbeCaches() {
	availProbeMu.Lock()
	availProbeCache = map[string]availProbeEntry{}
	availProbeFail = map[string]availProbeFailure{}
	availProbeCalls = map[string]*availProbeCall{}
	availProbeMu.Unlock()
}

// 目录单份 12MB,监控是每订阅 5 秒一轮 × 多 worker 并发。
// 并发进来的调用必须合并成一次拉取,否则光是启动瞬间就能把自己打进 429。
func TestLoadSubsidiaryCatalogSingleflight(t *testing.T) {
	resetCatalogCaches()
	orig := fetchSubsidiaryCatalog
	defer func() { fetchSubsidiaryCatalog = orig; resetCatalogCaches() }()

	var calls int32
	release := make(chan struct{})
	fetchSubsidiaryCatalog = func(state *app.State, subsidiary string) (*subsidiaryCatalog, error) {
		atomic.AddInt32(&calls, 1)
		<-release // 卡住,确保后来的调用都撞在"在途"这条路上
		return &subsidiaryCatalog{plans: map[string]planConfig{"p": {}}, fetchedAt: time.Now()}, nil
	}

	st := testState(t)
	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = loadSubsidiaryCatalog(st, "ie") // 顺便验证小写会被归一化成 IE
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 个调用出错: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("%d 个并发调用发起了 %d 次拉取,期望 1 次", n, got)
	}
	regionCacheMu.Lock()
	_, cached := regionCache["IE"]
	regionCacheMu.Unlock()
	if !cached {
		t.Error("缓存 key 不是大写子公司:OVH 只认大写,小写既会多存一份也会 400")
	}
}

// 拉取失败时必须有负缓存,否则失败期间每一轮监控都会重发完整请求 —— 自己把自己按在 429 里。
func TestLoadSubsidiaryCatalogNegativeCache(t *testing.T) {
	resetCatalogCaches()
	orig := fetchSubsidiaryCatalog
	defer func() { fetchSubsidiaryCatalog = orig; resetCatalogCaches() }()

	var calls int32
	wantErr := errors.New("HTTP 429")
	fetchSubsidiaryCatalog = func(state *app.State, subsidiary string) (*subsidiaryCatalog, error) {
		atomic.AddInt32(&calls, 1)
		return nil, wantErr
	}

	st := testState(t)
	for i := 0; i < 5; i++ {
		if _, err := loadSubsidiaryCatalog(st, "US"); !errors.Is(err, wantErr) {
			t.Fatalf("第 %d 次应当返回原始错误,得到 %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("静默期内发起了 %d 次拉取,期望 1 次", got)
	}

	// 负缓存到期后要能自己恢复,不能把子公司永久钉死
	regionCacheMu.Lock()
	regionCacheFail["US"] = catalogFailure{err: wantErr, at: time.Now().Add(-regionCacheFailTTL - time.Second)}
	regionCacheMu.Unlock()
	if _, err := loadSubsidiaryCatalog(st, "US"); !errors.Is(err, wantErr) {
		t.Fatalf("负缓存过期后应重试,得到 %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("负缓存过期后拉取次数 = %d,期望 2", got)
	}
}

// 正缓存过期后拉取失败:宁可用两小时前那份(区域/机房配置不会变),也不要退回静态兜底。
func TestLoadSubsidiaryCatalogServesStaleOnFailure(t *testing.T) {
	resetCatalogCaches()
	orig := fetchSubsidiaryCatalog
	defer func() { fetchSubsidiaryCatalog = orig; resetCatalogCaches() }()

	stale := &subsidiaryCatalog{
		plans:     map[string]planConfig{"24sk202-us": {regions: []string{"united_states"}}},
		fetchedAt: time.Now().Add(-regionCacheTTL - time.Minute),
	}
	regionCacheMu.Lock()
	regionCache["US"] = stale
	regionCacheMu.Unlock()

	fetchSubsidiaryCatalog = func(state *app.State, subsidiary string) (*subsidiaryCatalog, error) {
		return nil, errors.New("HTTP 429")
	}
	got, err := loadSubsidiaryCatalog(testState(t), "US")
	if err != nil {
		t.Fatalf("有旧缓存时不应把错误抛给调用方: %v", err)
	}
	if got != stale {
		t.Fatal("没有返回上一份缓存的目录")
	}
	// 旧缓存不能被当成"刚拉到的",否则会把它的寿命续到永远
	if !got.fetchedAt.Equal(stale.fetchedAt) {
		t.Error("旧缓存的 fetchedAt 被刷新了")
	}
}

// 一个 planCode 常被多个订阅同时监控,归属探测必须合并,否则每轮对同一机型重复打三个站点。
func TestRegionOfPlanSingleflight(t *testing.T) {
	resetAvailProbeCaches()
	orig := probeRegionHasPlan
	defer func() { probeRegionHasPlan = orig; resetAvailProbeCaches() }()

	var calls int32
	release := make(chan struct{})
	probeRegionHasPlan = func(region, planCode string) (bool, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return region == "US", nil
	}

	st := testState(t)
	const n = 12
	var wg sync.WaitGroup
	got := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := RegionOfPlan(st, "24sk202-us", []string{"eu", "US", "US"})
			if err != nil {
				t.Errorf("探测出错: %v", err)
			}
			got[i] = r
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for i, r := range got {
		if r != "US" {
			t.Fatalf("第 %d 个调用得到 %q,期望 US", i, r)
		}
	}
	// 候选是 [EU US](去重去空后),命中 US 前只该各探一次
	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Errorf("%d 个并发调用发起了 %d 次探测,期望 2 次(EU 未命中 + US 命中)", n, c)
	}
}

// 探测失败不能进正缓存(否则一次抖动把订阅按死 10 分钟),但要有短静默期,不能每 5 秒重打。
func TestRegionOfPlanErrorBackoff(t *testing.T) {
	resetAvailProbeCaches()
	orig := probeRegionHasPlan
	defer func() { probeRegionHasPlan = orig; resetAvailProbeCaches() }()

	var calls int32
	probeRegionHasPlan = func(region, planCode string) (bool, error) {
		atomic.AddInt32(&calls, 1)
		return false, fmt.Errorf("HTTP 429")
	}
	st := testState(t)
	for i := 0; i < 4; i++ {
		if _, err := RegionOfPlan(st, "24hci01", []string{"EU"}); err == nil {
			t.Fatal("探测失败时不应返回 nil error —— 调用方会据此认为机型不属于任何大区")
		}
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Errorf("静默期内探测了 %d 次,期望 1 次", c)
	}
	availProbeMu.Lock()
	_, cached := availProbeCache["24hci01"]
	availProbeMu.Unlock()
	if cached {
		t.Error("失败结论被写进了正缓存 —— 会把订阅静默 10 分钟")
	}

	// 静默期过后要恢复重试
	availProbeMu.Lock()
	for k, v := range availProbeFail {
		availProbeFail[k] = availProbeFailure{err: v.err, at: time.Now().Add(-availProbeErrTTL - time.Second)}
	}
	availProbeMu.Unlock()
	if _, err := RegionOfPlan(st, "24hci01", []string{"EU"}); err == nil {
		t.Fatal("静默期过后应重试")
	}
	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Errorf("静默期过后探测次数 = %d,期望 2", c)
	}
}

// 兜底 region:三区 availabilities 会返回长机房码,静态兜底必须认得出来。
func TestFallbackRegion(t *testing.T) {
	for _, c := range []struct{ dc, sub, want string }{
		// 美区:任何机房都只有 united_states
		{"gra", "US", "united_states"},
		{"us-east-vin-a", "US", "united_states"},
		// 长码(availabilities 实测返回:ca-east-tor-a / eu-west-par-a/b/c)
		{"eu-west-par-a", "IE", "europe"},
		{"eu-west-par-c", "FR", "europe"},
		{"ca-east-tor-a", "IE", "canada"},
		// 带编号的短码
		{"bhs5", "IE", "canada"},
		{"gra1", "IE", "europe"},
		// 亚太在目录里归 canada 桶
		{"sgp", "CA", "canada"},
		{"ynm", "IE", "canada"},
		// 非 US 子公司的目录里没有 united_states,宁可返回空串让调用方去问购物车
		{"vin", "IE", ""},
		{"us-east-vin-a", "IE", ""},
	} {
		if got := FallbackRegion(c.dc, c.sub); got != c.want {
			t.Errorf("FallbackRegion(%q, %q) = %q, 期望 %q", c.dc, c.sub, got, c.want)
		}
	}
}

// 缺省机房必须从该 plan 真正在卖的机房里挑(数字见 PreferredDatacenterForPlan 的注释)。
func TestPickDatacenter(t *testing.T) {
	euAndBHS := []string{"fra", "gra", "lon", "rbx", "sbg", "waw", "bhs"} // 取自真实 IE 目录 24adv01-v3
	for _, c := range []struct {
		name   string
		dcs    []string
		bucket string
		want   string
	}{
		// 同区有多个机房时优先取该大区的主力机房,而不是目录列表的第一个:
		// OVH 目录把 fra 排在 gra 前面,直接取首项会让欧区缺省询价机房无谓地
		// 从格拉沃利讷变成法兰克福(gra 本来就在这些 plan 的列表里)。
		{"欧区账户优先 gra 而不是目录首项 fra", euAndBHS, "europe", "gra"},
		{"加区账户挑 bhs 而不是列表第一个", euAndBHS, "canada", "bhs"},
		{"美区账户:该 plan 没有美国机房,退回目录第一个", euAndBHS, "united_states", "fra"},
		// 主力机房不卖这个机型时退回目录里第一个同区机房
		{"欧区主力机房不在列表:退回首个同区机房", []string{"waw", "lon"}, "europe", "waw"},
		{"只卖亚太的 plan:欧区账户也只能用 sgp", []string{"sgp"}, "europe", "sgp"},
		{"美区自家机型", []string{"vin", "hil"}, "united_states", "vin"},
		{"长机房码也能识别主力机房", []string{"eu-west-par-a", "gra"}, "europe", "gra"},
		{"目录没给机房", nil, "europe", ""},
	} {
		if got := pickDatacenter(c.dcs, c.bucket); got != c.want {
			t.Errorf("%s: pickDatacenter(%v, %q) = %q, 期望 %q", c.name, c.dcs, c.bucket, got, c.want)
		}
	}
}

func TestRegionBucketForSubsidiary(t *testing.T) {
	// 子公司归属仍以 ovh.SubsidiaryRegion 为准:WS/WE 属 CA、MA/TN/SN 属 EU
	for sub, want := range map[string]string{
		"US": "united_states",
		"CA": "canada", "QC": "canada", "SG": "canada", "WS": "canada", "WE": "canada",
		"IE": "europe", "FR": "europe", "MA": "europe", "TN": "europe",
	} {
		if got := regionBucketForSubsidiary(sub); got != want {
			t.Errorf("regionBucketForSubsidiary(%q) = %q, 期望 %q", sub, got, want)
		}
	}
}

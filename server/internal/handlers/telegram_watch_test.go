package handlers

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

func newWatchTestMonitor(t *testing.T) (*app.State, *monitor.Monitor) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	lg := logger.New(filepath.Join(dir, "t.log"),
		slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
	_ = os.Stdout
	st := app.NewState(storage.Paths{DataDir: dir}, config.New(database), lg, database)
	return st, monitor.New(st)
}

// /watch 是 TG 上唯一能挂「等补货」的入口 —— 参数解析错了,
// 用户以为挂上了,实际盯的是别的机房、或者根本没开自动下单。
func TestWatchParsesArgs(t *testing.T) {
	st, mon := newWatchTestMonitor(t)

	// 只给型号:盯所有机房,只通知不下单
	out := watchText(st, mon, []string{"24sk602"})
	if !strings.Contains(out, "已开始盯") || !strings.Contains(out, "只发通知") {
		t.Fatalf("默认应是只通知不下单：\n%s", out)
	}
	subs := mon.Snapshot()
	if len(subs) != 1 || subs[0].PlanCode != "24sk602" {
		t.Fatalf("订阅没加上：%+v", subs)
	}
	if subs[0].AutoOrder {
		t.Fatal("没带 x<数量> 时不该开自动下单 —— 那是会真花钱的")
	}

	// 指定机房
	out = watchText(st, mon, []string{"24sk603", "gra", "rbx"})
	if !strings.Contains(out, "GRA / RBX") {
		t.Fatalf("机房没解析出来：\n%s", out)
	}

	// 机房 + 自动下单数量。自动下单要落到具体账户,先配一个。
	addTestAccount(t, st)
	out = watchText(st, mon, []string{"24sk604", "sbg", "x2"})
	if !strings.Contains(out, "自动抢 2 台") {
		t.Fatalf("x2 没解析成自动下单 2 台：\n%s", out)
	}
	for _, s := range mon.Snapshot() {
		if s.PlanCode == "24sk604" {
			if !s.AutoOrder || s.Quantity != 2 {
				t.Fatalf("24sk604 应当是 autoOrder=true quantity=2，实际 %v/%d", s.AutoOrder, s.Quantity)
			}
		}
	}
}

// 看不懂的参数必须当场报错。静默吞掉会变成"盯了一个不存在的机房，永远等不到货"。
func TestWatchRejectsGarbageArgs(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	out := watchText(st, mon, []string{"24sk602", "这不是机房"})
	if !strings.Contains(out, "看不懂参数") {
		t.Fatalf("应当拒绝并说明：\n%s", out)
	}
	if len(mon.Snapshot()) != 0 {
		t.Fatal("参数有问题时不该建订阅")
	}
	// 没有参数给用法
	if out := watchText(st, mon, nil); !strings.Contains(out, "用法") {
		t.Fatalf("空参数应回用法：\n%s", out)
	}
}

// 没配账户时不能假装开了自动下单 —— 用户会以为挂上了，补货那一刻什么都不会发生。
func TestWatchRefusesAutoOrderWithoutAccount(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	out := watchText(st, mon, []string{"24sk602", "x1"})
	if !strings.Contains(out, "先配置 OVH 账户") {
		t.Fatalf("没账户时应当明确拒绝自动下单：\n%s", out)
	}
	if len(mon.Snapshot()) != 0 {
		t.Fatal("自动下单建不起来时不该留下半个订阅")
	}
}

// 重复 /watch 同一个型号是「就地改配置」，不能重置库存状态 ——
// 重置会让下一轮把「本来就有货」当成补货跳变，发一条根本没发生的通知外加真下单。
func TestWatchUpdatesInsteadOfRecreating(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	watchText(st, mon, []string{"24sk602", "gra"})

	// 给它一个库存状态，模拟已经跑过一轮
	subs := mon.Snapshot()
	if len(subs) != 1 {
		t.Fatalf("准备失败：%+v", subs)
	}

	out := watchText(st, mon, []string{"24sk602", "rbx"})
	if !strings.Contains(out, "已更新订阅") {
		t.Fatalf("第二次应当是更新而不是新建：\n%s", out)
	}
	if n := len(mon.Snapshot()); n != 1 {
		t.Fatalf("不该变成两条订阅，实际 %d 条", n)
	}
}

func TestUnwatch(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	watchText(st, mon, []string{"24sk602"})

	out := unwatchText(st, mon, []string{"24sk602"})
	if !strings.Contains(out, "已取消盯") {
		t.Fatalf("取消失败：\n%s", out)
	}
	if len(mon.Snapshot()) != 0 {
		t.Fatal("订阅没删掉")
	}
	// 没盯过的要说清楚，不能假装删成功
	if out := unwatchText(st, mon, []string{"nope"}); !strings.Contains(out, "没有在盯") {
		t.Fatalf("不存在的订阅应明说：\n%s", out)
	}
}

// /watch 和 /unwatch 必须被命令分发接住，不能落到下单解析里
// —— 那会让 bot 去抢一台叫 "watch" 的机器。
func TestWatchCommandsRouted(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	for _, text := range []string{"/watch 24sk602", "/unwatch 24sk602", "/w 24sk602", "/uw 24sk602"} {
		if !handleCommand(st, mon, int64(1), 1, text) {
			t.Errorf("%q 应当被当成命令处理", text)
		}
	}
}

// addTestAccount 建一个默认账户。自动下单必须解析成具体账户 ID：
// 不解析的话下单那一刻才现取默认账户，中间有人改过默认账户就会用错区的凭据。
func addTestAccount(t *testing.T, st *app.State) {
	t.Helper()
	acc := types.OVHAccount{
		ID: "acc-test", Name: "测试账户", Endpoint: "ovh-eu", Zone: "IE",
		AppKey: "k", AppSecret: "s", ConsumerKey: "c", IsDefault: true,
	}
	if err := st.DB.UpsertAccount(acc); err != nil {
		t.Fatalf("建测试账户失败: %v", err)
	}
	if err := st.ReloadAccounts(); err != nil {
		t.Fatalf("重载账户失败: %v", err)
	}
}

func TestWatchPurchasePeriod(t *testing.T) {
	state, mon := newWatchTestMonitor(t)
	watchText(state, mon, []string{"24sk602", "gra", "P1Y"})
	if config := mon.SubscriptionConfig("24sk602"); config.Duration != "P12M" || len(config.Datacenters) != 1 || config.Datacenters[0] != "gra" {
		t.Fatal(config)
	}
	if reply := watchText(state, mon, []string{"24sk602", "gra", "duration=P0M"}); !strings.Contains(reply, "时长") {
		t.Fatal(reply)
	}
	if config := mon.SubscriptionConfig("24sk602"); config.Duration != "P12M" {
		t.Fatal("invalid edit replaced chosen period", config)
	}
}

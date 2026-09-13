package types

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// 前端有一份同样的上限(web/src/lib/order-limits.ts)。
//
// 后端是权威(EnqueueItems 会拒),前端那份只是为了提前告诉用户,
// 而不是让他发出几百个请求之后才撞墙。但两份数一旦不一致,
// 表现是最难查的那种:界面说"最多 20 台"、点下去后端按 10 拒,
// 或者反过来界面放行 50 而请求发到一半开始报错。
//
// 这条测试直接读那个 .ts 文件核对。不是什么优雅做法,但重复已经存在了,
// 与其指望人记得改两处,不如让忘记的那次当场红。
func TestOrderLimitsMatchFrontend(t *testing.T) {
	const path = "../../../web/src/lib/order-limits.ts"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("读不到前端常量文件(%s),跳过:%v", path, err)
	}
	want := map[string]int{
		"MAX_ORDER_QUANTITY": MaxOrderQuantity,
		"MAX_ORDER_FANOUT":   MaxOrderFanout,
	}
	for name, goVal := range want {
		re := regexp.MustCompile(`(?m)^export const ` + name + `\s*=\s*(\d+)`)
		m := re.FindSubmatch(src)
		if m == nil {
			t.Errorf("前端 order-limits.ts 里找不到 %s —— 它被改名或删掉了?", name)
			continue
		}
		tsVal, _ := strconv.Atoi(string(m[1]))
		if tsVal != goVal {
			t.Errorf("%s 前后端不一致:Go=%d,前端=%d。\n"+
				"两边必须同时改 —— 不一致时用户会看到"+
				"「界面允许但后端拒绝」这种最难查的表现。", name, goVal, tsVal)
		}
	}
}

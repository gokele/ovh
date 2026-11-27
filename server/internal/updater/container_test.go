package updater

import "testing"

// 容器检测错了后果是双向的：
// 漏判 → 容器里给出一个点了必然失败（或者"成功"之后版本回滚）的更新按钮；
// 误判 → 裸机用户的自更新被无故停掉。
func TestInContainerEnvFlag(t *testing.T) {
	cases := map[string]bool{
		"1":     true,
		"true":  true,
		"yes":   true,
		"0":     false,
		"false": false,
		"FALSE": false,
		"":      false, // 空 = 交给别的判据
	}
	for v, want := range cases {
		t.Setenv("OVH_IN_CONTAINER", v)
		if got := InContainer(); got != (want || detectContainerRuntime()) {
			t.Errorf("InContainer() did not honor OVH_IN_CONTAINER=%q or runtime detection", v)
		}
		// 0/false 不是强制裸机：有运行时标记时仍须禁用容器内自更新。
		for _, marker := range []bool{false, true} {
			called := false
			got := inContainer(v, func() bool { called = true; return marker })
			if got != (want || marker) || called == want {
				t.Errorf("flag=%q marker=%v: container=%v detectionCalled=%v", v, marker, got, called)
			}
		}
	}
}

// 更新指引必须把"为什么"说清楚，否则用户只会觉得功能坏了。
func TestContainerUpdateHintIsActionable(t *testing.T) {
	for _, must := range []string{"docker compose pull", "docker pull", "卷", "可写层"} {
		if !contains(ContainerUpdateHint, must) {
			t.Errorf("更新指引里缺少 %q", must)
		}
	}
}

func contains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}

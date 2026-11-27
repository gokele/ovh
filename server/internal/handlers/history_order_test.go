package handlers

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

// 重启后从库里读出的旧记录在前、运行中新追加的在后;接口必须按下单时间从新到旧返回,
// 时区写法不同也按真实时间比较,解析不了的排最后。
func TestPurchaseHistoryNewestFirst(t *testing.T) {
	st, _ := newWatchTestMonitor(t)
	st.History = []types.PurchaseHistoryEntry{
		{ID: "old", PurchaseTime: "2026-11-20T10:00:00Z"},
		{ID: "bad", PurchaseTime: "not-a-time"},
		{ID: "newest", PurchaseTime: "2026-11-25T09:00:00+08:00"}, // 01:00Z
		{ID: "mid", PurchaseTime: "2026-11-24T20:00:00Z"},
		{ID: "latest-utc", PurchaseTime: "2026-11-25T02:00:00Z"},
	}
	router := gin.New()
	router.GET("/h", GetPurchaseHistory(st))
	w := payRequest(router, "GET", "/h", "")
	var got []types.PurchaseHistoryEntry
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, g := range got {
		ids = append(ids, g.ID)
	}
	want := []string{"latest-utc", "newest", "mid", "old", "bad"}
	for i := range want {
		if i >= len(ids) || ids[i] != want[i] {
			t.Fatalf("order %v want %v", ids, want)
		}
	}
	// 不改动内存里的原始顺序(只排序副本)
	if st.History[0].ID != "old" {
		t.Fatalf("state mutated: %v", st.History[0].ID)
	}
}

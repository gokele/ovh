package handlers

import (
	"bytes"
	"encoding/json"
	"testing"
)

// 长轮询用 UseNumber 解析 update,数字是 json.Number。message_id / user_id 必须读得出来:
// 读成 0 时菜单按钮会去编辑「第 0 条消息」,表现为点了没反应。
func TestCallbackIDsSurviveUseNumberDecoding(t *testing.T) {
	raw := `{"update_id":812345678,"callback_query":{"id":"5800000000000000001","from":{"id":7123456789},
		"message":{"message_id":4321,"chat":{"id":-1001234567890}},"data":"{\"a\":\"m\",\"c\":\"status\"}"}}`
	var data map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		t.Fatal(err)
	}
	cb := data["callback_query"].(map[string]interface{})
	msg := cb["message"].(map[string]interface{})
	from := cb["from"].(map[string]interface{})
	if id, ok := getNumOrFloat(msg["message_id"]); !ok || int64(id) != 4321 {
		t.Fatalf("message_id lost: %v %v", id, ok)
	}
	if id, ok := getNumOrFloat(from["id"]); !ok || int64(id) != 7123456789 {
		t.Fatalf("user id lost: %v %v", id, ok)
	}
	if got := idToString(getNested(msg, "chat", "id")); got != "-1001234567890" {
		t.Fatalf("chat id lost: %s", got)
	}
	// 普通 float64(测试桩、旧路径)仍然可读
	if id, ok := getNumOrFloat(float64(9)); !ok || id != 9 {
		t.Fatal("float64 must still work")
	}
	if _, ok := getNumOrFloat(nil); ok {
		t.Fatal("nil must not parse")
	}
}

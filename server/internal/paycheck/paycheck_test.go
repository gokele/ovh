package paycheck

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

// fixture 本地假订单状态接口 + 本地假 webhook 收通知,不访问 OVH。
func fixture(t *testing.T, statuses []string) (*app.State, *ovhsdk.Client, func() []string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	lg := logger.New(filepath.Join(dir, "t.log"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	state := app.NewState(storage.Paths{DataDir: dir}, config.New(database), lg, database)
	var mu sync.Mutex
	var sent []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sent = append(sent, string(b))
		mu.Unlock()
	}))
	t.Cleanup(hook.Close)
	state.Config.Set(types.Config{NotifyWebhookURL: hook.URL})
	i := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/time" {
			fmt.Fprint(w, time.Now().Unix())
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("payment check must be read-only, got %s", r.Method)
		}
		st := statuses[len(statuses)-1]
		if i < len(statuses) {
			st = statuses[i]
		}
		i++
		fmt.Fprintf(w, "%q", st)
	}))
	t.Cleanup(api.Close)
	client, err := ovhsdk.NewClient(api.URL, "ak", "as", "ck")
	if err != nil {
		t.Fatal(err)
	}
	return state, client, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), sent...) }
}

func noSleep(time.Duration) {}

// 付上了不打扰;一直 notPaid 必须提醒手动付款;取消也要提醒。
func TestAutoPayFollowUp(t *testing.T) {
	state, client, sent := fixture(t, []string{"notPaid", "checking"})
	run(state, client, "plan@GRA", "123", "https://example.invalid/order", noSleep)
	if len(sent()) != 0 {
		t.Fatalf("paid order must not notify: %v", sent())
	}

	state, client, sent = fixture(t, []string{"notPaid"})
	run(state, client, "plan@GRA", "124", "https://example.invalid/order", noSleep)
	if msgs := sent(); len(msgs) != 1 || !strings.Contains(msgs[0], "自动付款没有完成") {
		t.Fatalf("unpaid order must warn once: %v", msgs)
	}

	state, client, sent = fixture(t, []string{"cancelled"})
	run(state, client, "plan@GRA", "125", "https://example.invalid/order", noSleep)
	if msgs := sent(); len(msgs) != 1 || !strings.Contains(msgs[0], "已被取消") {
		t.Fatalf("cancelled order must warn: %v", msgs)
	}

	// 要求补交资料:不能说成「自动付款没有完成」,单独提醒补交资料
	state, client, sent = fixture(t, []string{"documentsRequested"})
	run(state, client, "plan@GRA", "126", "https://example.invalid/order", noSleep)
	if msgs := sent(); len(msgs) != 1 || !strings.Contains(msgs[0], "补交资料") || strings.Contains(msgs[0], "自动付款没有完成") {
		t.Fatalf("documentsRequested must ask for documents, not claim unpaid: %v", msgs)
	}

	// OVH 返回 unknown 后再读到已付:不打扰
	state, client, sent = fixture(t, []string{"unknown", "delivering"})
	run(state, client, "plan@GRA", "127", "https://example.invalid/order", noSleep)
	if len(sent()) != 0 {
		t.Fatalf("unknown then paid must not notify: %v", sent())
	}

	// 始终 unknown:按「读不到状态」提醒去订单页核对,不能断言没付上
	state, client, sent = fixture(t, []string{"unknown"})
	run(state, client, "plan@GRA", "128", "https://example.invalid/order", noSleep)
	if msgs := sent(); len(msgs) != 1 || !strings.Contains(msgs[0], "无法读取订单状态") {
		t.Fatalf("always unknown must ask to verify: %v", msgs)
	}
}

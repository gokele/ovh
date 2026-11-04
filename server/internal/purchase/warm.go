package purchase

import (
	"context"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
)

// transportWarmLoop 仅为运行中任务的账户每 20 秒发一次轻量请求,停止处理器后自动退出。
func transportWarmLoop(ctx context.Context, state *app.State) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		state.QueueMu.Lock()
		accounts := map[string]bool{}
		for _, item := range state.Queue {
			if item.Status == "running" {
				accounts[item.AccountID] = true
			}
		}
		state.QueueMu.Unlock()
		if state.OrderPrewarm != nil {
			for _, preparation := range state.OrderPrewarm.Status("") {
				accounts[preparation.AccountID] = true
			}
		}
		for account := range accounts {
			if ctx.Err() != nil {
				return
			}
			client, err := state.OVH.ClientFor(account)
			if err == nil {
				probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				_ = ovh.KeepWarm(probeCtx, client)
				cancel()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

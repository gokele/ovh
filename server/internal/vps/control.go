package vps

import (
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

func subscriptionCurrent(state *app.State, snapshot *types.VPSSubscription) bool {
	state.VPSSubsMu.Lock()
	defer state.VPSSubsMu.Unlock()
	for _, live := range state.VPSSubscriptions {
		if live.ID == snapshot.ID {
			return live.Revision == snapshot.Revision
		}
	}
	return false
}

// Only observations from the current configuration may replace the live baseline.
func saveObservation(state *app.State, snapshot *types.VPSSubscription) error {
	if len(snapshot.History) > 100 {
		snapshot.History = snapshot.History[len(snapshot.History)-100:]
	}
	state.VPSSubsMu.Lock()
	var previous types.VPSSubscription
	found := false
	for i := range state.VPSSubscriptions {
		live := &state.VPSSubscriptions[i]
		if live.ID == snapshot.ID && live.Revision == snapshot.Revision {
			previous, found = *live, true
			live.LastStatus, live.History = snapshot.LastStatus, snapshot.History
			break
		}
	}
	state.VPSSubsMu.Unlock()
	if !found {
		return nil
	}
	if err := SaveSubscriptions(state); err != nil {
		state.VPSSubsMu.Lock()
		for i := range state.VPSSubscriptions {
			live := &state.VPSSubscriptions[i]
			if live.ID == snapshot.ID && live.Revision == snapshot.Revision {
				live.LastStatus, live.History = previous.LastStatus, previous.History
				break
			}
		}
		state.VPSSubsMu.Unlock()
		return err
	}
	return nil
}

func disableAutoOrder(state *app.State, snapshot types.VPSSubscription) {
	state.VPSSubsMu.Lock()
	for i := range state.VPSSubscriptions {
		live := &state.VPSSubscriptions[i]
		if live.ID == snapshot.ID && live.Revision == snapshot.Revision {
			live.AutoOrder = false
			live.Revision++
			break
		}
	}
	state.VPSSubsMu.Unlock()
	if err := SaveSubscriptions(state); err != nil {
		state.Logger.Error("保存 VPS 自动下单停止状态失败: "+err.Error(), "vps_purchase")
	}
}

func InvalidateAccountChecks(state *app.State, accountID string) {
	state.VPSSubsMu.Lock()
	defer state.VPSSubsMu.Unlock()
	for i := range state.VPSSubscriptions {
		sub := &state.VPSSubscriptions[i]
		if sub.AutoOrderAccountID == accountID {
			sub.Revision++
			state.UnregisterTaskCancel("vps:" + sub.ID)
		}
	}
}

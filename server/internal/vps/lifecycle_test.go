package vps

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

func lifecycleState(t *testing.T) *app.State {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	lg := logger.New(filepath.Join(dir, "test.log"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	state := app.NewState(storage.Paths{DataDir: dir}, config.New(database), lg, database)
	state.Config.Set(types.Config{NotifyWebhookURL: "https://notify.invalid/test"})
	return state
}

func TestStoppedDeletedAndEditedChecksCannotCommitOrOrder(t *testing.T) {
	originalFetch, originalTransport := fetchSubscriptionAvailability, http.DefaultTransport
	t.Cleanup(func() { fetchSubscriptionAvailability = originalFetch; http.DefaultTransport = originalTransport })
	for _, action := range []string{"stop", "delete", "edit"} {
		t.Run(action, func(t *testing.T) {
			state := lifecycleState(t)
			sub := types.VPSSubscription{ID: "stale", PlanCode: "vps-model", OvhSubsidiary: "IE", NotifyAvailable: true, AutoOrder: true, AutoOrderAccountID: "eu", LastStatus: map[string]string{"eu-west-lim": "unavailable"}}
			state.VPSSubscriptions = []types.VPSSubscription{sub}
			snapshot := cloneSubs(state.VPSSubscriptions)[0]
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runningMu.Lock()
			generation++
			gen := generation
			running, loopContext, loopCancel = true, ctx, cancel
			runningMu.Unlock()
			defer Stop(state)
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			fetchSubscriptionAvailability = func(context.Context, *app.State, string, string, string, string) (map[string]interface{}, error) {
				close(entered)
				<-release
				return map[string]interface{}{"datacenters": []interface{}{map[string]interface{}{"code": "eu-west-lim", "datacenter": "DE", "status": "available"}}}, nil
			}
			var requests atomic.Int32
			http.DefaultTransport = purchaseTestTransport(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				return testVPSResponse(r, 200, `{}`), nil
			})
			go func() { checkSubscription(ctx, state, gen, &snapshot); close(done) }()
			<-entered
			if action == "stop" {
				Stop(state)
			} else {
				state.VPSSubsMu.Lock()
				if action == "delete" {
					state.VPSSubscriptions = nil
				} else {
					state.VPSSubscriptions[0].Revision++
					state.VPSSubscriptions[0].OS = "Debian 13"
				}
				state.VPSSubsMu.Unlock()
			}
			close(release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("stale check did not return")
			}
			if requests.Load() != 0 || len(state.History) != 0 {
				t.Fatalf("stale check performed side effects: requests=%d history=%d", requests.Load(), len(state.History))
			}
			if action != "delete" && state.VPSSubscriptions[0].LastStatus["eu-west-lim"] != "unavailable" {
				t.Fatal("stale response replaced the stock baseline")
			}
		})
	}
}

func TestRestartDoesNotHoldRunningLockWhileWaiting(t *testing.T) {
	state := lifecycleState(t)
	previous := &sync.WaitGroup{}
	previous.Add(1)
	runningMu.Lock()
	running = false
	loopWg = previous
	oldGeneration := generation
	runningMu.Unlock()
	release := make(chan struct{})
	go func() {
		<-release
		stillMine(oldGeneration)
		previous.Done()
	}()
	started := make(chan bool, 1)
	go func() { started <- Start(state) }()
	time.Sleep(30 * time.Millisecond)
	close(release)
	select {
	case ok := <-started:
		if !ok {
			t.Fatal("restart was rejected")
		}
	case <-time.After(time.Second):
		t.Fatal("restart deadlocked waiting for a loop that needs runningMu")
	}
	Stop(state)
	runningMu.Lock()
	done := loopWg
	runningMu.Unlock()
	exited := make(chan struct{})
	go func() { done.Wait(); close(exited) }()
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("stopped monitor did not exit")
	}
}

func TestManualStopSurvivesRestart(t *testing.T) {
	state := lifecycleState(t)
	if !AutoStartAllowed(state) {
		t.Fatal("new installation cannot auto start")
	}
	if err := StopByUser(state); err != nil {
		t.Fatal(err)
	}
	if AutoStartAllowed(state) {
		t.Fatal("manual stop was lost")
	}
	// A new in-memory State still observes the persisted stop instruction.
	restored := app.NewState(state.Paths, state.Config, state.Logger, state.DB)
	if AutoStartAllowed(restored) {
		t.Fatal("restart would revive a stopped monitor")
	}
	if !Start(state) {
		t.Fatal("explicit start did not clear the stop instruction")
	}
	Stop(state)
	runningMu.Lock()
	done := loopWg
	runningMu.Unlock()
	done.Wait()
	if !AutoStartAllowed(restored) {
		t.Fatal("explicit start was not saved")
	}
}

func TestInventoryMatchesShortAndLongDatacenterNames(t *testing.T) {
	original := fetchSubscriptionAvailability
	t.Cleanup(func() { fetchSubscriptionAvailability = original })
	fetchSubscriptionAvailability = func(context.Context, *app.State, string, string, string, string) (map[string]interface{}, error) {
		return map[string]interface{}{"datacenters": []interface{}{map[string]interface{}{"code": "eu-west-lim", "datacenter": "DE", "status": "available"}}}, nil
	}
	for _, filter := range []string{"de", "EU-WEST-LIM"} {
		state := lifecycleState(t)
		sub := types.VPSSubscription{ID: "dc", PlanCode: "fixture", OvhSubsidiary: "IE", Datacenters: []string{filter}, LastStatus: map[string]string{"DE": "unavailable"}}
		state.VPSSubscriptions = []types.VPSSubscription{sub}
		snapshot := cloneSubs(state.VPSSubscriptions)[0]
		ctx, cancel := context.WithCancel(context.Background())
		runningMu.Lock()
		generation++
		gen := generation
		running, loopContext, loopCancel = true, ctx, cancel
		runningMu.Unlock()
		checkSubscription(ctx, state, gen, &snapshot)
		Stop(state)
		cancel()
		saved, err := state.DB.ListVPSSubscriptions()
		if err != nil || len(saved) != 1 || saved[0].LastStatus["eu-west-lim"] != "available" || len(saved[0].History) != 1 {
			t.Fatalf("filter %s missed stock or persistence: %+v %v", filter, saved, err)
		}
	}
}

func TestFailedObservationSaveKeepsPreviousBaseline(t *testing.T) {
	state := lifecycleState(t)
	state.VPSSubscriptions = []types.VPSSubscription{{ID: "unsaved", LastStatus: map[string]string{"DE": "unavailable"}}}
	snapshot := types.VPSSubscription{ID: "unsaved", LastStatus: map[string]string{"DE": "available"}}
	_ = state.DB.Close()
	if err := saveObservation(state, &snapshot); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if state.VPSSubscriptions[0].LastStatus["DE"] != "unavailable" {
		t.Fatal("failed persistence consumed the restock transition")
	}
}

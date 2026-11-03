package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 同账户/型号合并为一轮探测,所有消费者保留同一个检测起点。
func TestStockFetchCoalescesConcurrentReaders(t *testing.T) {
	var bus StockSignals
	var calls atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan StockSignal, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := bus.Fetch(context.Background(), "eu", "plan", func() ([]map[string]interface{}, error) {
				if calls.Add(1) == 1 {
					close(started)
				}
				<-release
				return []map[string]interface{}{{"fqn": "plan"}}, nil
			})
			if err != nil {
				t.Error(err)
			}
			results <- s
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(results)
	var first time.Time
	for result := range results {
		if first.IsZero() {
			first = result.DetectedAt
		}
		if result.DetectedAt != first {
			t.Error("detection time was reset for a reader")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate stock requests", calls.Load())
	}
}

// 不同账户/机型不能串信号,缓冲区保留最新值且注销不会阻塞发布。
func TestStockSignalIsolationAndLatestValue(t *testing.T) {
	var bus StockSignals
	eu, stop := bus.Subscribe("eu", "plan")
	defer stop()
	us, stopUS := bus.Subscribe("us", "plan")
	defer stopUS()
	old := time.Now()
	latest := old.Add(time.Millisecond)
	bus.Publish(StockSignal{AccountID: "eu", PlanCode: "plan", DetectedAt: old})
	bus.Publish(StockSignal{AccountID: "eu", PlanCode: "plan", DetectedAt: latest})
	if (<-eu).DetectedAt != latest {
		t.Fatal("stale stock signal")
	}
	select {
	case <-us:
		t.Fatal("wrong-account signal")
	default:
	}
	stop()
	bus.Publish(StockSignal{AccountID: "eu", PlanCode: "plan"})
}

// 合并查询的发起任务被暂停时,其余消费者重新查询,不继承取消或耗尽重试次数。
func TestStockFetchCancellationDoesNotCancelOtherTasks(t *testing.T) {
	var bus StockSignals
	owner, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	ownerDone := make(chan error, 1)
	go func() {
		_, err := bus.Fetch(owner, "eu", "plan", func() ([]map[string]interface{}, error) { close(started); <-owner.Done(); return nil, owner.Err() })
		ownerDone <- err
	}()
	<-started
	result := make(chan error, 1)
	go func() {
		_, err := bus.Fetch(context.Background(), "eu", "plan", func() ([]map[string]interface{}, error) { return []map[string]interface{}{{"fqn": "plan"}}, nil })
		result <- err
	}()
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal("another task was cancelled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("another task was stuck")
	}
	if err := <-ownerDone; err != context.Canceled {
		t.Fatal(err)
	}
}

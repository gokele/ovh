package app

import (
	"context"
	"errors"
	"sync"
	"time"
)

// StockSignal 是只读库存响应;发送时间带单调时钟,不经过 JSON、本地 HTTP 或数据库。
type StockSignal struct {
	AccountID  string
	PlanCode   string
	Variants   []map[string]interface{}
	DetectedAt time.Time
}

type stockListener struct {
	account, plan string
	channel       chan StockSignal
}

// StockSignals 按账户和型号投递最新观察,有界覆盖防止慢消费者堆积过期补货。
type StockSignals struct {
	mu           sync.Mutex
	next         uint64
	listeners    map[uint64]stockListener
	observations map[stockKey]StockSignal
	flights      map[stockKey]*stockFlight
}

// Subscribe 为每个独立抢购任务注册一个容量为 1 的直接信箱。
func (b *StockSignals) Subscribe(account, plan string) (<-chan StockSignal, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listeners == nil {
		b.listeners = map[uint64]stockListener{}
	}
	b.next++
	id := b.next
	ch := make(chan StockSignal, 1)
	b.listeners[id] = stockListener{account, plan, ch}
	return ch, func() { b.mu.Lock(); delete(b.listeners, id); b.mu.Unlock() }
}

// Publish 不等待任务执行或写盘,同一时刻只保留最新响应;调用方发布后不得修改 Variants。
func (b *StockSignals) Publish(signal StockSignal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, listener := range b.listeners {
		if listener.account != signal.AccountID || listener.plan != signal.PlanCode {
			continue
		}
		select {
		case <-listener.channel:
		default:
		}
		select {
		case listener.channel <- signal:
		default:
		}
	}
}

// stockKey 不用拼接字符串,避免账户/型号包含分隔符时命中别人的观察。
type stockKey struct{ account, plan string }
type stockFlight struct {
	done   chan struct{}
	signal StockSignal
	err    error
}

// Fetch 合并同账户/型号的并发探测;最多复用 100ms 的原始观察并保留原始检测时间。
func (b *StockSignals) Fetch(ctx context.Context, account, plan string, fetch func() ([]map[string]interface{}, error)) (StockSignal, error) {
	if err := ctx.Err(); err != nil {
		return StockSignal{}, err
	}
	key := stockKey{account, plan}
	b.mu.Lock()
	if b.observations == nil {
		b.observations = map[stockKey]StockSignal{}
		b.flights = map[stockKey]*stockFlight{}
	}
	if signal, ok := b.observations[key]; ok && time.Since(signal.DetectedAt) < 100*time.Millisecond {
		b.mu.Unlock()
		return signal, nil
	}
	if flight := b.flights[key]; flight != nil {
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return StockSignal{}, ctx.Err()
		case <-flight.done:
			// 首个消费者暂停只取消它自己的探测,其它运行任务重新接管查询。
			if errors.Is(flight.err, context.Canceled) && ctx.Err() == nil {
				return b.Fetch(ctx, account, plan, fetch)
			}
			return flight.signal, flight.err
		}
	}
	flight := &stockFlight{done: make(chan struct{})}
	b.flights[key] = flight
	b.mu.Unlock()
	variants, err := fetch()
	signal := StockSignal{AccountID: account, PlanCode: plan, Variants: variants, DetectedAt: time.Now()}
	b.mu.Lock()
	if err == nil {
		if len(b.observations) >= 512 {
			for old := range b.observations {
				delete(b.observations, old)
				break
			}
		}
		b.observations[key] = signal
	}
	flight.signal, flight.err = signal, err
	delete(b.flights, key)
	close(flight.done)
	b.mu.Unlock()
	if err == nil {
		b.Publish(signal)
	}
	return signal, err
}

// Observe 记录一次外部探测结果并投递给订阅者:只在比已有观察更新时写入,
// 让任务自己的 Fetch 在 100ms 内复用它;过期或乱序到达的结果直接丢弃。
func (b *StockSignals) Observe(signal StockSignal) bool {
	key := stockKey{signal.AccountID, signal.PlanCode}
	b.mu.Lock()
	if b.observations == nil {
		b.observations = map[stockKey]StockSignal{}
		b.flights = map[stockKey]*stockFlight{}
	}
	if old, ok := b.observations[key]; ok && !signal.DetectedAt.After(old.DetectedAt) {
		b.mu.Unlock()
		return false
	}
	if len(b.observations) >= 512 {
		for old := range b.observations {
			delete(b.observations, old)
			break
		}
	}
	b.observations[key] = signal
	b.mu.Unlock()
	b.Publish(signal)
	return true
}

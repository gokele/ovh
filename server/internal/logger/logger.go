package logger

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

const (
	maxLogs        = 1000
	writeThreshold = 10
)

// Logger 内存累积 + 批量刷盘 + 控制台输出
type Logger struct {
	mu           sync.Mutex
	entries      []types.LogEntry
	writeCounter int
	logsFile     string
	stdlog       *slog.Logger
}

// New 创建 logger
func New(logsFile string, console *slog.Logger) *Logger {
	if console == nil {
		console = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	l := &Logger{
		entries:  make([]types.LogEntry, 0, maxLogs),
		logsFile: logsFile,
		stdlog:   console,
	}
	l.Load()
	return l
}

// Load 启动时读取已有日志（最多保留 maxLogs 条）
func (l *Logger) Load() {
	l.mu.Lock()
	defer l.mu.Unlock()

	var existing []types.LogEntry
	if err := storage.ReadJSON(l.logsFile, &existing); err != nil {
		l.stdlog.Warn("read logs file", "err", err)
		return
	}
	if len(existing) > maxLogs {
		existing = existing[len(existing)-maxLogs:]
	}
	l.entries = existing
}

// Add 添加一条日志
func (l *Logger) Add(level, message, source string) {
	if source == "" {
		source = "system"
	}
	entry := types.LogEntry{
		ID:        uuid.NewString(),
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Level:     level,
		Message:   message,
		Source:    source,
	}

	l.mu.Lock()
	l.entries = append(l.entries, entry)
	if len(l.entries) > maxLogs {
		l.entries = l.entries[len(l.entries)-maxLogs:]
	}
	l.writeCounter++
	shouldWrite := l.writeCounter >= writeThreshold || level == "ERROR"
	snapshot := l.entries // 直接引用，下面 Flush 会复制
	l.mu.Unlock()

	if shouldWrite {
		l.flush(snapshot)
	}

	// 控制台输出
	msg := fmt.Sprintf("[%s] %s", source, message)
	switch level {
	case "ERROR":
		l.stdlog.Error(msg)
	case "WARNING", "WARN":
		l.stdlog.Warn(msg)
	case "DEBUG":
		l.stdlog.Debug(msg)
	default:
		l.stdlog.Info(msg)
	}
}

// Info/Warn/Error/Debug 便捷方法
func (l *Logger) Info(msg, source string)  { l.Add("INFO", msg, source) }
func (l *Logger) Warn(msg, source string)  { l.Add("WARNING", msg, source) }
func (l *Logger) Error(msg, source string) { l.Add("ERROR", msg, source) }
func (l *Logger) Debug(msg, source string) { l.Add("DEBUG", msg, source) }

// Flush 强制刷盘
func (l *Logger) Flush() {
	l.mu.Lock()
	snapshot := make([]types.LogEntry, len(l.entries))
	copy(snapshot, l.entries)
	l.writeCounter = 0
	l.mu.Unlock()
	l.flush(snapshot)
}

func (l *Logger) flush(entries []types.LogEntry) {
	// 复制后写盘，避免与 Add 竞争
	cp := make([]types.LogEntry, len(entries))
	copy(cp, entries)
	if err := storage.WriteJSON(l.logsFile, cp); err != nil {
		l.stdlog.Error("write logs file", "err", err)
	}
	l.mu.Lock()
	l.writeCounter = 0
	l.mu.Unlock()
}

// Snapshot 取当前内存中的日志副本
func (l *Logger) Snapshot() []types.LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	cp := make([]types.LogEntry, len(l.entries))
	copy(cp, l.entries)
	return cp
}

// Clear 清空所有日志（含文件）
func (l *Logger) Clear() error {
	l.mu.Lock()
	l.entries = l.entries[:0]
	l.writeCounter = 0
	l.mu.Unlock()
	return storage.WriteJSON(l.logsFile, []types.LogEntry{})
}

// MarshalEntries 调试用：序列化全部条目
func (l *Logger) MarshalEntries() string {
	b, _ := json.MarshalIndent(l.Snapshot(), "", "  ")
	return string(b)
}

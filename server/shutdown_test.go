package main

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovh-buy/server/internal/logger"
	"time"
)

// closeSpy 记录 Close 有没有被调用
type closeSpy struct {
	closed bool
	err    error
}

func (c *closeSpy) Close() error { c.closed = true; return c.err }

// 收到 SIGTERM 时必须做两件**可观察**的事:把缓冲区里的日志写进文件、关掉数据库。
//
// 在加信号处理之前,这两件一件都不会发生 —— Go 收到 SIGTERM 默认当场终止,
// defer 不执行。实测(旧二进制,发 SIGTERM):日志文件**根本没被创建**,
// 启动到退出之间的全部日志凭空消失。而 Docker 停/重建容器发的就是 SIGTERM,
// 「compose up -d 拉新镜像」是这个项目最常见的运维动作。
func TestGracefulShutdownFlushesLogsAndClosesDB(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log.json")
	lg := newTestLogger(t, logPath)

	// 先写几条只存在于内存缓冲里的日志
	lg.Info("抢购任务 t1 开始", "queue")
	lg.Info("抢购任务 t1 无货,等待下一轮", "queue")

	if _, err := os.Stat(logPath); err == nil {
		t.Log("注意:日志在收尾之前就已落盘,这条测试的前提变弱了(缓冲策略变了?)")
	}

	db := &closeSpy{}
	// srv 传 nil:这里要验的是"日志落盘 + 关库",HTTP 收尾由 net/http 自己保证
	gracefulShutdown(nil, db, lg, nil, "terminated", time.Second)

	if !db.closed {
		t.Error("数据库没有被关闭")
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("日志没有落盘(这正是修复前的表现): %v", err)
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("日志文件不是合法 JSON: %v", err)
	}
	joined := string(raw)
	for _, want := range []string{"抢购任务 t1 开始", "正在优雅退出", "正在关闭数据库"} {
		if !strings.Contains(joined, want) {
			t.Errorf("日志里缺少 %q —— 收尾时这条没写进去", want)
		}
	}
}

// 关库失败不能把收尾流程打断,而且必须留下痕迹 ——
// 否则用户只会看到进程退出了,不知道数据可能没写完。
func TestGracefulShutdownRecordsCloseFailure(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log.json")
	lg := newTestLogger(t, logPath)

	db := &closeSpy{err: errors.New("database is locked")}
	gracefulShutdown(nil, db, lg, nil, "interrupt", time.Second)

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("日志没有落盘: %v", err)
	}
	if !strings.Contains(string(raw), "database is locked") {
		t.Error("关库失败没有记进日志,用户无从知道数据可能没写完")
	}
}

func newTestLogger(t *testing.T, path string) *logger.Logger {
	t.Helper()
	// console 输出丢掉:测试要看的是落盘的那份
	return logger.New(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

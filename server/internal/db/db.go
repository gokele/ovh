// Package db 实现 SQLite 持久化层，替代原来的 storage 包文件级 JSON 读写。
// 双 driver 设计：
//   - CGO 可用时（默认 go build）走 mattn/go-sqlite3，性能更好；
//   - CGO_ENABLED=0 时走 modernc.org/sqlite（纯 Go），零 C 依赖，方便交叉编译/无 gcc 环境。
// 切换由 build tag 自动完成，详见 driver_cgo.go / driver_purego.go。
package db

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
)

//go:embed schema.sql
var schemaSQL string

// DB 包装 *sqlx.DB，所有表的 CRUD 方法都挂在它上面（按文件分散）
type DB struct {
	*sqlx.DB
	Path   string
	Driver string // 当前实际使用的 driver 名（"sqlite3" / "sqlite"），便于日志展示
}

// Open 打开 SQLite 数据库，启动时调一次。
// dataDir 是数据目录（与原 storage.Paths.DataDir 同一个），DB 文件落在 <dataDir>/sniper.db。
//
// PRAGMA 配置（两个 driver 等价，只是 DSN 语法不同）：
//   - journal_mode=WAL：单 writer 多 reader 并发，性能远高于 DELETE 模式
//   - synchronous=NORMAL：WAL 模式下足够安全（断电最多丢最后几次提交，不会损坏库）
//   - foreign_keys=ON：标准实践
//   - busy_timeout=5000：被其它写阻塞时最多等 5 秒再 SQLITE_BUSY
func Open(dataDir string) (*DB, error) {
	path := filepath.Join(dataDir, "sniper.db")
	sx, err := sqlx.Open(driverName, makeDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite (%s) %s: %w", driverName, path, err)
	}
	// 控制连接池：写时单连接更安全（避免 SQLITE_LOCKED），读用默认池
	sx.SetMaxOpenConns(8)
	sx.SetMaxIdleConns(2)
	sx.SetConnMaxLifetime(time.Hour)

	if err := sx.Ping(); err != nil {
		_ = sx.Close()
		return nil, fmt.Errorf("ping sqlite (%s): %w", driverName, err)
	}

	db := &DB{DB: sx, Path: path, Driver: driverName}
	if err := db.migrate(); err != nil {
		_ = sx.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// migrate 跑 schema.sql 里所有 CREATE TABLE / INDEX（用 IF NOT EXISTS，重复跑安全）
func (db *DB) migrate() error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("exec schema: %w", err)
	}
	return nil
}

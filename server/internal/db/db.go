// Package db 实现 SQLite 持久化层，替代原来的 storage 包文件级 JSON 读写。
// 双 driver 设计：
//   - CGO 可用时（默认 go build）走 mattn/go-sqlite3，性能更好；
//   - CGO_ENABLED=0 时走 modernc.org/sqlite（纯 Go），零 C 依赖，方便交叉编译/无 gcc 环境。
//
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

// migrate 跑 schema.sql 里所有 CREATE TABLE / INDEX（用 IF NOT EXISTS，重复跑安全）+ 增量列迁移
func (db *DB) migrate() error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("exec schema: %w", err)
	}
	if err := db.addColumnIfMissing("queue", "account_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := db.addColumnIfMissing("history", "account_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// retraction_time:撤销权截止日。老库没有这一列,而 ListHistory 用 SELECT * + sqlx 严格映射,
	// 只加结构体字段不加列会让整个历史列表报 missing destination name,两者必须同一次上线。
	// failure_count:只记真正提交并失败的次数(无货轮次不计),MaxRetries 靠它封顶
	// auto_pay:下单后用默认支付方式自动付款的显式开关
	if err := db.addColumnIfMissing("queue", "auto_pay", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := db.addColumnIfMissing("monitor_subscriptions", "auto_pay", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := db.addColumnIfMissing("queue", "failure_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := db.addColumnIfMissing("history", "retraction_time", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := db.addColumnIfMissing("monitor_subscriptions", "auto_order_account_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := db.addColumnIfMissing("vps_subscriptions", "auto_order_account_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// telegram_order_buttons.account_id:一键下单按钮记住"该下到哪个账户"。
	// 老库建表时没有这一列,而 ClaimTelegramButton / GetTelegramButton 是显式列名 SELECT +
	// sqlx 严格映射,只加结构体字段不加列会让所有按钮回调直接 500,两者必须同一次上线。
	if err := db.addColumnIfMissing("telegram_order_buttons", "account_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

// addColumnIfMissing SQLite ALTER TABLE ADD COLUMN 不支持 IF NOT EXISTS,
// 这里查 PRAGMA table_info 自己判断列是否已存在,做幂等加列。
func (db *DB) addColumnIfMissing(table, column, typeDecl string) error {
	type colInfo struct {
		CID     int     `db:"cid"`
		Name    string  `db:"name"`
		Type    string  `db:"type"`
		NotNull int     `db:"notnull"`
		Dflt    *string `db:"dflt_value"`
		PK      int     `db:"pk"`
	}
	var cols []colInfo
	if err := db.Select(&cols, fmt.Sprintf("PRAGMA table_info(%s)", table)); err != nil {
		return fmt.Errorf("pragma %s: %w", table, err)
	}
	for _, c := range cols {
		if c.Name == column {
			return nil // 已有,跳过
		}
	}
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, typeDecl)
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

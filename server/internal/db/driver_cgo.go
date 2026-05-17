//go:build cgo

package db

import (
	"fmt"

	_ "github.com/mattn/go-sqlite3" // 注册 driver "sqlite3"
)

// driverName mattn/go-sqlite3 注册的 driver 名
const driverName = "sqlite3"

// makeDSN mattn/go-sqlite3 的 DSN 语法：每个 PRAGMA 单独一个 query 参数
// 参考: https://github.com/mattn/go-sqlite3#connection-string
func makeDSN(path string) string {
	return fmt.Sprintf(
		"file:%s?_journal=WAL&_synchronous=NORMAL&_fk=true&_busy_timeout=5000",
		path,
	)
}

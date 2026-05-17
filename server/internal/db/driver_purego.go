//go:build !cgo

package db

import (
	"fmt"

	_ "modernc.org/sqlite" // 注册 driver "sqlite"
)

// driverName modernc.org/sqlite 注册的 driver 名
const driverName = "sqlite"

// makeDSN modernc.org/sqlite 的 DSN 语法：用 _pragma=name(value) 形式
// 参考: https://pkg.go.dev/modernc.org/sqlite#hdr-Connection_String
func makeDSN(path string) string {
	return fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		path,
	)
}

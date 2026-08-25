package db

import (
	"fmt"
	"time"
)

// TryClaimTelegramUpdate 幂等认领 update_id。
// claimed=true 表示这条 update 是第一次处理；false 表示重复投递（Telegram 在没收到
// 200 时会重投同一条 update，没有这张表就会重复下单）。
func (db *DB) TryClaimTelegramUpdate(updateID int64) (claimed bool, err error) {
	if updateID <= 0 {
		// 没有 update_id 的请求（不是 Telegram 发的）不进幂等表，交给上层 secret 校验拦
		return true, nil
	}
	res, err := db.Exec(
		`INSERT OR IGNORE INTO telegram_updates (update_id, processed_at) VALUES (?, ?)`,
		updateID, float64(time.Now().Unix()),
	)
	if err != nil {
		return false, fmt.Errorf("claim telegram update: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CleanupTelegramUpdates 删除 processed_at < beforeUnix 的旧记录，防止表无限增长。
func (db *DB) CleanupTelegramUpdates(beforeUnix float64) (int64, error) {
	res, err := db.Exec(`DELETE FROM telegram_updates WHERE processed_at < ?`, beforeUnix)
	if err != nil {
		return 0, fmt.Errorf("cleanup telegram updates: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

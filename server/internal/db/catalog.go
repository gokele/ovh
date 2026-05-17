package db

import (
	"database/sql"
	"fmt"
	"time"
)

// GetCatalog 按 subsidiary 取 catalog JSON 原文 + 更新时间（Unix ms）。
// 没记录时 ok=false。
func (db *DB) GetCatalog(subsidiary string) (raw string, updatedAtMs int64, ok bool, err error) {
	row := struct {
		Data      string `db:"data"`
		UpdatedAt int64  `db:"updated_at"`
	}{}
	err = db.Get(&row, `SELECT data, updated_at FROM catalogs WHERE subsidiary = ?`, subsidiary)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, fmt.Errorf("catalog get %s: %w", subsidiary, err)
	}
	return row.Data, row.UpdatedAt, true, nil
}

// UpsertCatalog 按 subsidiary upsert catalog JSON 原文，updated_at = now ms。
func (db *DB) UpsertCatalog(subsidiary, data string) error {
	_, err := db.Exec(
		`INSERT INTO catalogs(subsidiary, data, updated_at) VALUES(?, ?, ?)
		 ON CONFLICT(subsidiary) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`,
		subsidiary, data, time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("catalog upsert %s: %w", subsidiary, err)
	}
	return nil
}

// ClearCatalogs 清空所有 catalog，缓存管理"清除全部"会调
func (db *DB) ClearCatalogs() error {
	_, err := db.Exec(`DELETE FROM catalogs`)
	return err
}

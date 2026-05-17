package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ovh-buy/server/internal/types"
)

// ListServers 取全部服务器目录条目，按 plan_code 排序
func (db *DB) ListServers() ([]types.ServerPlan, error) {
	type row struct {
		Data string `db:"data"`
	}
	var rows []row
	if err := db.Select(&rows, `SELECT data FROM servers ORDER BY plan_code`); err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	out := make([]types.ServerPlan, 0, len(rows))
	for _, r := range rows {
		var p types.ServerPlan
		if err := json.Unmarshal([]byte(r.Data), &p); err != nil {
			continue // 单条坏数据不阻塞整个列表
		}
		out = append(out, p)
	}
	return out, nil
}

// ReplaceServers 用整套新目录覆盖（refresh-from-OVH 后调用）。
// 事务内 DELETE + 批量 INSERT。
func (db *DB) ReplaceServers(plans []types.ServerPlan) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM servers`); err != nil {
		return fmt.Errorf("clear servers: %w", err)
	}
	nowMs := time.Now().UnixMilli()
	stmt, err := tx.Preparex(`INSERT INTO servers(plan_code, data, updated_at) VALUES(?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range plans {
		raw, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("marshal server %s: %w", p.PlanCode, err)
		}
		if _, err := stmt.Exec(p.PlanCode, string(raw), nowMs); err != nil {
			return fmt.Errorf("insert server %s: %w", p.PlanCode, err)
		}
	}
	return tx.Commit()
}

// ServersUpdatedAt 取最新一次刷新时间（Unix ms），用于缓存信息展示
func (db *DB) ServersUpdatedAt() (int64, error) {
	var ts int64
	err := db.Get(&ts, `SELECT COALESCE(MAX(updated_at), 0) FROM servers`)
	return ts, err
}

// ServerCount 返回 servers 表里的行数，给缓存管理 UI 展示
func (db *DB) ServerCount() (int, error) {
	var n int
	err := db.Get(&n, `SELECT COUNT(*) FROM servers`)
	return n, err
}

// ClearServers 清空 servers 表（缓存管理的"清除 SQLite 缓存"会调）
func (db *DB) ClearServers() error {
	_, err := db.Exec(`DELETE FROM servers`)
	return err
}

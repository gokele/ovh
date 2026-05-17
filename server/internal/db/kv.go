package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// GetKV 取一条 kv 记录，将 JSON value 反序列化进 v。
// key 不存在时 ok=false 且 v 不变（与原 storage.ReadJSON 语义一致）。
func (db *DB) GetKV(key string, v interface{}) (ok bool, err error) {
	var raw string
	err = db.Get(&raw, `SELECT value FROM kv WHERE key = ?`, key)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kv get %s: %w", key, err)
	}
	if raw == "" {
		return false, nil
	}
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		return false, fmt.Errorf("kv unmarshal %s: %w", key, err)
	}
	return true, nil
}

// SetKV 写入一条 kv 记录（upsert）。v 会被 JSON 序列化为字符串存进 value 列。
func (db *DB) SetKV(key string, v interface{}) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("kv marshal %s: %w", key, err)
	}
	_, err = db.Exec(
		`INSERT INTO kv(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, string(raw),
	)
	if err != nil {
		return fmt.Errorf("kv set %s: %w", key, err)
	}
	return nil
}

// DeleteKV 删除一条 kv 记录。
func (db *DB) DeleteKV(key string) error {
	_, err := db.Exec(`DELETE FROM kv WHERE key = ?`, key)
	return err
}

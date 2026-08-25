package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// TelegramButtonRow TG「一键下单」按钮 UUID → 下单参数。
// 原来只存 monitor.messageUUIDCache（内存），进程重启后按钮全部失效，
// 且同一个 UUID 可以被无限次点击重复下单。落库后按钮跨重启可用，
// 并用 used_at 做一次性 nonce。
type TelegramButtonRow struct {
	ID         string  `db:"id"`
	PlanCode   string  `db:"plan_code"`
	Datacenter string  `db:"datacenter"`
	Options    string  `db:"options"`     // JSON []string
	ConfigInfo string  `db:"config_info"` // JSON object
	AccountID  string  `db:"account_id"`  // 触发通知的订阅所用账户,空 = 回调时退回默认账户
	CreatedAt  float64 `db:"created_at"`  // unix 秒
	UsedAt     float64 `db:"used_at"`     // >0 已消费
}

// telegramButtonCols 所有读路径共用的列清单。
// 不用 SELECT *:sqlx 严格映射,加列时 * 会把没有对应字段的列直接报错。
const telegramButtonCols = `id, plan_code, datacenter, options, config_info, account_id, created_at, used_at`

// UpsertTelegramButton 发通知时为每个机房按钮写一行（不带账户）。
// 保留这个签名是为了不动既有调用方；能拿到账户的路径请用 UpsertTelegramButtonForAccount。
func (db *DB) UpsertTelegramButton(id, planCode, datacenter string, options []string, configInfo map[string]interface{}, createdAt float64) error {
	return db.UpsertTelegramButtonForAccount(id, "", planCode, datacenter, options, configInfo, createdAt)
}

// UpsertTelegramButtonForAccount 同上，但把「这一单该用哪个 OVH 账户」一起落库。
// 同 id 重复写视为重置（used_at 归零），实际 id 是 uuid，不会撞。
//
// 为什么按钮必须记账户:planCode 是分区的（EU/US/CA 三份目录基本不重合），
// 回调时按"默认账户"下单,多账户跨区就会把欧区机型下到美区账户上 —— OVH 返回
// 200 + 空库存而不是报错,队列会一直重试到过期,用户完全看不出是账户选错了。
func (db *DB) UpsertTelegramButtonForAccount(id, accountID, planCode, datacenter string, options []string, configInfo map[string]interface{}, createdAt float64) error {
	if id == "" {
		return fmt.Errorf("empty telegram button id")
	}
	if options == nil {
		options = []string{}
	}
	optsRaw, err := json.Marshal(options)
	if err != nil {
		return fmt.Errorf("marshal options: %w", err)
	}
	if configInfo == nil {
		configInfo = map[string]interface{}{}
	}
	cfgRaw, err := json.Marshal(configInfo)
	if err != nil {
		// configInfo 里可能混进不可序列化的值，退化成空对象而不是整条失败
		cfgRaw = []byte("{}")
	}
	if createdAt <= 0 {
		createdAt = float64(time.Now().Unix())
	}
	_, err = db.Exec(
		`INSERT INTO telegram_order_buttons (id, plan_code, datacenter, options, config_info, account_id, created_at, used_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0)
		 ON CONFLICT(id) DO UPDATE SET
		   plan_code   = excluded.plan_code,
		   datacenter  = excluded.datacenter,
		   options     = excluded.options,
		   config_info = excluded.config_info,
		   account_id  = excluded.account_id,
		   created_at  = excluded.created_at,
		   used_at     = 0`,
		id, planCode, datacenter, string(optsRaw), string(cfgRaw), accountID, createdAt,
	)
	if err != nil {
		return fmt.Errorf("upsert telegram button: %w", err)
	}
	return nil
}

// ClaimTelegramButton 原子认领一个未消费的按钮并返回它。
// ok=false 有三种情况：id 不存在、已被消费、已过期（过期由调用方按 created_at 判断）。
// 入队失败时调用方应调 UnclaimTelegramButton 回滚，让用户能重试。
func (db *DB) ClaimTelegramButton(id string) (row TelegramButtonRow, ok bool, err error) {
	if id == "" {
		return row, false, nil
	}
	now := float64(time.Now().Unix())

	tx, err := db.Beginx()
	if err != nil {
		return row, false, fmt.Errorf("begin claim button: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// UPDATE ... WHERE used_at = 0 是原子的：并发双击只有一次 RowsAffected=1
	res, err := tx.Exec(
		`UPDATE telegram_order_buttons SET used_at = ? WHERE id = ? AND used_at = 0`,
		now, id,
	)
	if err != nil {
		return row, false, fmt.Errorf("claim telegram button: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return row, false, nil
	}
	if err := tx.Get(&row, `SELECT `+telegramButtonCols+`
		FROM telegram_order_buttons WHERE id = ?`, id); err != nil {
		return row, false, fmt.Errorf("read claimed button: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return row, false, fmt.Errorf("commit claim button: %w", err)
	}
	return row, true, nil
}

// UnclaimTelegramButton 入队失败时回滚认领，允许用户重试同一个按钮。
func (db *DB) UnclaimTelegramButton(id string) error {
	if id == "" {
		return nil
	}
	_, err := db.Exec(`UPDATE telegram_order_buttons SET used_at = 0 WHERE id = ?`, id)
	return err
}

// GetTelegramButton 只读查询，用于区分「不存在」和「已消费」两种失败，好给用户不同提示。
func (db *DB) GetTelegramButton(id string) (row TelegramButtonRow, ok bool, err error) {
	if id == "" {
		return row, false, nil
	}
	err = db.Get(&row, `SELECT `+telegramButtonCols+`
		FROM telegram_order_buttons WHERE id = ?`, id)
	if err == sql.ErrNoRows {
		return row, false, nil
	}
	if err != nil {
		return row, false, fmt.Errorf("get telegram button: %w", err)
	}
	return row, true, nil
}

// DeleteExpiredTelegramButtons 清理 created_at < beforeUnix 的老按钮。
func (db *DB) DeleteExpiredTelegramButtons(beforeUnix float64) (int64, error) {
	res, err := db.Exec(`DELETE FROM telegram_order_buttons WHERE created_at < ?`, beforeUnix)
	if err != nil {
		return 0, fmt.Errorf("delete expired telegram buttons: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// SetTelegramButtonAccount 补写按钮的账户归属。
//
// 为什么是单独一步而不是并进 Upsert:写按钮的入口 monitor.AddMessageUUID 不在本次改动
// 范围内,它仍走不带账户的 UpsertTelegramButton。发通知的 notify.go 在那之后补这一次
// UPDATE,失败也只是退回"默认账户"的老行为,不影响按钮本身可用。
func (db *DB) SetTelegramButtonAccount(id, accountID string) error {
	if id == "" || accountID == "" {
		return nil
	}
	_, err := db.Exec(`UPDATE telegram_order_buttons SET account_id = ? WHERE id = ?`, accountID, id)
	if err != nil {
		return fmt.Errorf("set telegram button account: %w", err)
	}
	return nil
}

// ParseTelegramButtonOptions 解析 options JSON，永不返回 nil。
func ParseTelegramButtonOptions(raw string) []string {
	opts := []string{}
	if raw == "" {
		return opts
	}
	if err := json.Unmarshal([]byte(raw), &opts); err != nil || opts == nil {
		return []string{}
	}
	return opts
}

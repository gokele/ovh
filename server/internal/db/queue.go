package db

import (
	"encoding/json"
	"fmt"

	"github.com/ovh-buy/server/internal/types"
)

// queueRow 是表结构的一一对应（snake_case 列名 + JSON 列做 string）
type queueRow struct {
	ID                 string  `db:"id"`
	AccountID          string  `db:"account_id"`
	PlanCode           string  `db:"plan_code"`
	Datacenter         string  `db:"datacenter"`
	OptionsJSON        string  `db:"options"`
	Status             string  `db:"status"`
	CreatedAt          string  `db:"created_at"`
	UpdatedAt          string  `db:"updated_at"`
	RetryInterval      int     `db:"retry_interval"`
	RetryCount         int     `db:"retry_count"`
	FailureCount       int     `db:"failure_count"`
	MaxRetries         int     `db:"max_retries"`
	LastCheckTime      float64 `db:"last_check_time"`
	QuickOrder         int     `db:"quick_order"`
	Priority           int     `db:"priority"`
	FromTelegram       int     `db:"from_telegram"`
	ConfigSniperTaskID string  `db:"config_sniper_task_id"`
}

func rowToQueueItem(r queueRow) types.QueueItem {
	var opts []string
	if r.OptionsJSON != "" {
		_ = json.Unmarshal([]byte(r.OptionsJSON), &opts)
	}
	if opts == nil {
		opts = []string{}
	}
	return types.QueueItem{
		ID:                 r.ID,
		AccountID:          r.AccountID,
		PlanCode:           r.PlanCode,
		Datacenter:         r.Datacenter,
		Options:            opts,
		Status:             r.Status,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
		RetryInterval:      r.RetryInterval,
		RetryCount:         r.RetryCount,
		FailureCount:       r.FailureCount,
		MaxRetries:         r.MaxRetries,
		LastCheckTime:      r.LastCheckTime,
		QuickOrder:         r.QuickOrder == 1,
		Priority:           r.Priority,
		FromTelegram:       r.FromTelegram == 1,
		ConfigSniperTaskID: r.ConfigSniperTaskID,
	}
}

func queueItemToRow(q types.QueueItem) (queueRow, error) {
	if q.Options == nil {
		q.Options = []string{}
	}
	optsJSON, err := json.Marshal(q.Options)
	if err != nil {
		return queueRow{}, err
	}
	bi := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	return queueRow{
		ID:                 q.ID,
		AccountID:          q.AccountID,
		PlanCode:           q.PlanCode,
		Datacenter:         q.Datacenter,
		OptionsJSON:        string(optsJSON),
		Status:             q.Status,
		CreatedAt:          q.CreatedAt,
		UpdatedAt:          q.UpdatedAt,
		RetryInterval:      q.RetryInterval,
		RetryCount:         q.RetryCount,
		FailureCount:       q.FailureCount,
		MaxRetries:         q.MaxRetries,
		LastCheckTime:      q.LastCheckTime,
		QuickOrder:         bi(q.QuickOrder),
		Priority:           q.Priority,
		FromTelegram:       bi(q.FromTelegram),
		ConfigSniperTaskID: q.ConfigSniperTaskID,
	}, nil
}

// ListQueue 取全部队列任务
func (db *DB) ListQueue() ([]types.QueueItem, error) {
	var rows []queueRow
	if err := db.Select(&rows, `SELECT * FROM queue ORDER BY created_at`); err != nil {
		return nil, fmt.Errorf("list queue: %w", err)
	}
	out := make([]types.QueueItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToQueueItem(r))
	}
	return out, nil
}

// ReplaceQueue 用给定列表覆盖整张表（事务内 DELETE + 批量 INSERT）。
// 与原 storage.WriteJSON 语义对齐。
func (db *DB) ReplaceQueue(items []types.QueueItem) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM queue`); err != nil {
		return fmt.Errorf("clear queue: %w", err)
	}
	for _, q := range items {
		r, err := queueItemToRow(q)
		if err != nil {
			return err
		}
		_, err = tx.NamedExec(`
			INSERT INTO queue
			(id, account_id, plan_code, datacenter, options, status, created_at, updated_at,
			 retry_interval, retry_count, failure_count, max_retries, last_check_time,
			 quick_order, priority, from_telegram, config_sniper_task_id)
			VALUES
			(:id, :account_id, :plan_code, :datacenter, :options, :status, :created_at, :updated_at,
			 :retry_interval, :retry_count, :failure_count, :max_retries, :last_check_time,
			 :quick_order, :priority, :from_telegram, :config_sniper_task_id)
		`, r)
		if err != nil {
			return fmt.Errorf("insert queue %s: %w", q.ID, err)
		}
	}
	return tx.Commit()
}

// DeleteQueueItem 按 id 删除单条
func (db *DB) DeleteQueueItem(id string) error {
	_, err := db.Exec(`DELETE FROM queue WHERE id = ?`, id)
	return err
}

// ClearQueue 清空队列，返回删了多少条
func (db *DB) ClearQueue() (int64, error) {
	res, err := db.Exec(`DELETE FROM queue`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

package db

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ovh-buy/server/internal/types"
)

type historyRow struct {
	ID             string         `db:"id"`
	TaskID         string         `db:"task_id"`
	PlanCode       string         `db:"plan_code"`
	Datacenter     string         `db:"datacenter"`
	OptionsJSON    string         `db:"options"`
	Status         string         `db:"status"`
	OrderID        string         `db:"order_id"`
	OrderURL       string         `db:"order_url"`
	ErrorMessage   sql.NullString `db:"error_message"`
	PurchaseTime   string         `db:"purchase_time"`
	AttemptCount   int            `db:"attempt_count"`
	ExpirationTime string         `db:"expiration_time"`
	PriceJSON      sql.NullString `db:"price"`
}

func rowToHistory(r historyRow) types.PurchaseHistoryEntry {
	var opts []string
	if r.OptionsJSON != "" {
		_ = json.Unmarshal([]byte(r.OptionsJSON), &opts)
	}
	if opts == nil {
		opts = []string{}
	}
	var price *types.PriceInfo
	if r.PriceJSON.Valid && r.PriceJSON.String != "" {
		var p types.PriceInfo
		if err := json.Unmarshal([]byte(r.PriceJSON.String), &p); err == nil {
			price = &p
		}
	}
	var errMsg *string
	if r.ErrorMessage.Valid {
		s := r.ErrorMessage.String
		errMsg = &s
	}
	return types.PurchaseHistoryEntry{
		ID:             r.ID,
		TaskID:         r.TaskID,
		PlanCode:       r.PlanCode,
		Datacenter:     r.Datacenter,
		Options:        opts,
		Status:         r.Status,
		OrderID:        r.OrderID,
		OrderURL:       r.OrderURL,
		ErrorMessage:   errMsg,
		PurchaseTime:   r.PurchaseTime,
		AttemptCount:   r.AttemptCount,
		ExpirationTime: r.ExpirationTime,
		Price:          price,
	}
}

func historyToRow(h types.PurchaseHistoryEntry) (historyRow, error) {
	if h.Options == nil {
		h.Options = []string{}
	}
	optsJSON, err := json.Marshal(h.Options)
	if err != nil {
		return historyRow{}, err
	}
	row := historyRow{
		ID:             h.ID,
		TaskID:         h.TaskID,
		PlanCode:       h.PlanCode,
		Datacenter:     h.Datacenter,
		OptionsJSON:    string(optsJSON),
		Status:         h.Status,
		OrderID:        h.OrderID,
		OrderURL:       h.OrderURL,
		PurchaseTime:   h.PurchaseTime,
		AttemptCount:   h.AttemptCount,
		ExpirationTime: h.ExpirationTime,
	}
	if h.ErrorMessage != nil {
		row.ErrorMessage = sql.NullString{String: *h.ErrorMessage, Valid: true}
	}
	if h.Price != nil {
		priceJSON, err := json.Marshal(h.Price)
		if err != nil {
			return row, err
		}
		row.PriceJSON = sql.NullString{String: string(priceJSON), Valid: true}
	}
	return row, nil
}

// ListHistory 取全部抢购历史，按时间倒序
func (db *DB) ListHistory() ([]types.PurchaseHistoryEntry, error) {
	var rows []historyRow
	if err := db.Select(&rows, `SELECT * FROM history ORDER BY purchase_time DESC`); err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	out := make([]types.PurchaseHistoryEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToHistory(r))
	}
	return out, nil
}

// UpsertHistory 单条 upsert
func (db *DB) UpsertHistory(h types.PurchaseHistoryEntry) error {
	r, err := historyToRow(h)
	if err != nil {
		return err
	}
	_, err = db.NamedExec(`
		INSERT INTO history
		(id, task_id, plan_code, datacenter, options, status, order_id, order_url,
		 error_message, purchase_time, attempt_count, expiration_time, price)
		VALUES
		(:id, :task_id, :plan_code, :datacenter, :options, :status, :order_id, :order_url,
		 :error_message, :purchase_time, :attempt_count, :expiration_time, :price)
		ON CONFLICT(id) DO UPDATE SET
		  task_id         = excluded.task_id,
		  plan_code       = excluded.plan_code,
		  datacenter      = excluded.datacenter,
		  options         = excluded.options,
		  status          = excluded.status,
		  order_id        = excluded.order_id,
		  order_url       = excluded.order_url,
		  error_message   = excluded.error_message,
		  purchase_time   = excluded.purchase_time,
		  attempt_count   = excluded.attempt_count,
		  expiration_time = excluded.expiration_time,
		  price           = excluded.price
	`, r)
	if err != nil {
		return fmt.Errorf("upsert history %s: %w", h.ID, err)
	}
	return nil
}

// ReplaceHistory 全表覆盖（保留 ReplaceX API 一致）
func (db *DB) ReplaceHistory(items []types.PurchaseHistoryEntry) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM history`); err != nil {
		return fmt.Errorf("clear history: %w", err)
	}
	for _, h := range items {
		r, err := historyToRow(h)
		if err != nil {
			return err
		}
		_, err = tx.NamedExec(`
			INSERT INTO history
			(id, task_id, plan_code, datacenter, options, status, order_id, order_url,
			 error_message, purchase_time, attempt_count, expiration_time, price)
			VALUES
			(:id, :task_id, :plan_code, :datacenter, :options, :status, :order_id, :order_url,
			 :error_message, :purchase_time, :attempt_count, :expiration_time, :price)
		`, r)
		if err != nil {
			return fmt.Errorf("insert history %s: %w", h.ID, err)
		}
	}
	return tx.Commit()
}

// ClearHistory 清空历史
func (db *DB) ClearHistory() (int64, error) {
	res, err := db.Exec(`DELETE FROM history`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

package db

import (
	"encoding/json"
	"fmt"

	"github.com/ovh-buy/server/internal/types"
)

type monitorSubRow struct {
	PlanCode          string `db:"plan_code"`
	DatacentersJSON   string `db:"datacenters"`
	NotifyAvailable   int    `db:"notify_available"`
	NotifyUnavailable int    `db:"notify_unavailable"`
	LastStatusJSON    string `db:"last_status"`
	CreatedAt         string `db:"created_at"`
	HistoryJSON       string `db:"history"`
	ServerName        string `db:"server_name"`
	AutoOrder         int    `db:"auto_order"`
	Quantity          int    `db:"quantity"`
}

func rowToMonitorSub(r monitorSubRow) types.Subscription {
	var dcs []string
	_ = json.Unmarshal([]byte(r.DatacentersJSON), &dcs)
	if dcs == nil {
		dcs = []string{}
	}
	last := map[string]string{}
	_ = json.Unmarshal([]byte(r.LastStatusJSON), &last)
	hist := []types.SubscriptionHistoryEntry{}
	_ = json.Unmarshal([]byte(r.HistoryJSON), &hist)
	return types.Subscription{
		PlanCode:          r.PlanCode,
		Datacenters:       dcs,
		NotifyAvailable:   r.NotifyAvailable == 1,
		NotifyUnavailable: r.NotifyUnavailable == 1,
		LastStatus:        last,
		CreatedAt:         r.CreatedAt,
		History:           hist,
		ServerName:        r.ServerName,
		AutoOrder:         r.AutoOrder == 1,
		Quantity:          r.Quantity,
	}
}

func monitorSubToRow(s types.Subscription) (monitorSubRow, error) {
	if s.Datacenters == nil {
		s.Datacenters = []string{}
	}
	if s.LastStatus == nil {
		s.LastStatus = map[string]string{}
	}
	if s.History == nil {
		s.History = []types.SubscriptionHistoryEntry{}
	}
	dcsJSON, err := json.Marshal(s.Datacenters)
	if err != nil {
		return monitorSubRow{}, err
	}
	lastJSON, err := json.Marshal(s.LastStatus)
	if err != nil {
		return monitorSubRow{}, err
	}
	histJSON, err := json.Marshal(s.History)
	if err != nil {
		return monitorSubRow{}, err
	}
	bi := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	return monitorSubRow{
		PlanCode:          s.PlanCode,
		DatacentersJSON:   string(dcsJSON),
		NotifyAvailable:   bi(s.NotifyAvailable),
		NotifyUnavailable: bi(s.NotifyUnavailable),
		LastStatusJSON:    string(lastJSON),
		CreatedAt:         s.CreatedAt,
		HistoryJSON:       string(histJSON),
		ServerName:        s.ServerName,
		AutoOrder:         bi(s.AutoOrder),
		Quantity:          s.Quantity,
	}, nil
}

// ListMonitorSubscriptions 取全部服务器监控订阅
func (db *DB) ListMonitorSubscriptions() ([]types.Subscription, error) {
	var rows []monitorSubRow
	if err := db.Select(&rows, `SELECT * FROM monitor_subscriptions ORDER BY created_at`); err != nil {
		return nil, fmt.Errorf("list monitor subs: %w", err)
	}
	out := make([]types.Subscription, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToMonitorSub(r))
	}
	return out, nil
}

// UpsertMonitorSubscription 按 plan_code upsert
func (db *DB) UpsertMonitorSubscription(s types.Subscription) error {
	r, err := monitorSubToRow(s)
	if err != nil {
		return err
	}
	_, err = db.NamedExec(`
		INSERT INTO monitor_subscriptions
		(plan_code, datacenters, notify_available, notify_unavailable, last_status,
		 created_at, history, server_name, auto_order, quantity)
		VALUES
		(:plan_code, :datacenters, :notify_available, :notify_unavailable, :last_status,
		 :created_at, :history, :server_name, :auto_order, :quantity)
		ON CONFLICT(plan_code) DO UPDATE SET
		  datacenters        = excluded.datacenters,
		  notify_available   = excluded.notify_available,
		  notify_unavailable = excluded.notify_unavailable,
		  last_status        = excluded.last_status,
		  history            = excluded.history,
		  server_name        = excluded.server_name,
		  auto_order         = excluded.auto_order,
		  quantity           = excluded.quantity
	`, r)
	if err != nil {
		return fmt.Errorf("upsert monitor sub %s: %w", s.PlanCode, err)
	}
	return nil
}

// ReplaceMonitorSubscriptions 全表覆盖
func (db *DB) ReplaceMonitorSubscriptions(subs []types.Subscription) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM monitor_subscriptions`); err != nil {
		return err
	}
	for _, s := range subs {
		r, err := monitorSubToRow(s)
		if err != nil {
			return err
		}
		_, err = tx.NamedExec(`
			INSERT INTO monitor_subscriptions
			(plan_code, datacenters, notify_available, notify_unavailable, last_status,
			 created_at, history, server_name, auto_order, quantity)
			VALUES
			(:plan_code, :datacenters, :notify_available, :notify_unavailable, :last_status,
			 :created_at, :history, :server_name, :auto_order, :quantity)
		`, r)
		if err != nil {
			return fmt.Errorf("insert monitor sub %s: %w", s.PlanCode, err)
		}
	}
	return tx.Commit()
}

// DeleteMonitorSubscription 按 plan_code 删除
func (db *DB) DeleteMonitorSubscription(planCode string) error {
	_, err := db.Exec(`DELETE FROM monitor_subscriptions WHERE plan_code = ?`, planCode)
	return err
}

// ClearMonitorSubscriptions 清空
func (db *DB) ClearMonitorSubscriptions() (int64, error) {
	res, err := db.Exec(`DELETE FROM monitor_subscriptions`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

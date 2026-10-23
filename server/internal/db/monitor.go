package db

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ovh-buy/server/internal/types"
)

type monitorSubRow struct {
	PlanCode           string `db:"plan_code"`
	DatacentersJSON    string `db:"datacenters"`
	NotifyAvailable    int    `db:"notify_available"`
	NotifyUnavailable  int    `db:"notify_unavailable"`
	LastStatusJSON     string `db:"last_status"`
	CreatedAt          string `db:"created_at"`
	HistoryJSON        string `db:"history"`
	ServerName         string `db:"server_name"`
	AutoOrder          int    `db:"auto_order"`
	Quantity           int    `db:"quantity"`
	AutoOrderAccountID string `db:"auto_order_account_id"`
	AutoPay            int    `db:"auto_pay"`
	Paused             int    `db:"paused"`
	OptionsJSON        string `db:"options"`
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
	var opts []string
	_ = json.Unmarshal([]byte(r.OptionsJSON), &opts)
	return types.Subscription{
		PlanCode:           r.PlanCode,
		Datacenters:        dcs,
		NotifyAvailable:    r.NotifyAvailable == 1,
		NotifyUnavailable:  r.NotifyUnavailable == 1,
		LastStatus:         last,
		CreatedAt:          r.CreatedAt,
		History:            hist,
		ServerName:         r.ServerName,
		AutoOrder:          r.AutoOrder == 1,
		Quantity:           r.Quantity,
		AutoOrderAccountID: r.AutoOrderAccountID,
		AutoPay:            r.AutoPay == 1,
		Paused:             r.Paused == 1,
		Options:            opts,
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
	if s.Options == nil {
		s.Options = []string{}
	}
	opts := append([]string{}, s.Options...)
	sort.Strings(opts)
	optsJSON, err := json.Marshal(opts)
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
		PlanCode:           s.PlanCode,
		DatacentersJSON:    string(dcsJSON),
		NotifyAvailable:    bi(s.NotifyAvailable),
		NotifyUnavailable:  bi(s.NotifyUnavailable),
		LastStatusJSON:     string(lastJSON),
		CreatedAt:          s.CreatedAt,
		HistoryJSON:        string(histJSON),
		ServerName:         s.ServerName,
		AutoOrder:          bi(s.AutoOrder),
		Quantity:           s.Quantity,
		AutoOrderAccountID: s.AutoOrderAccountID,
		AutoPay:            bi(s.AutoPay),
		Paused:             bi(s.Paused),
		OptionsJSON:        string(optsJSON),
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

// UpsertMonitorSubscription 按型号和规范化配置 upsert
// auto_order_account_id 必须出现在 INSERT 列表里:ON CONFLICT 分支引用的 excluded.xxx
// 取的是"这次 INSERT 尝试插入的值",列不在 INSERT 列表里时它等于列默认值(空串),
// 于是每次更新都会把订阅的自动下单账户清空。
func (db *DB) UpsertMonitorSubscription(s types.Subscription) error {
	r, err := monitorSubToRow(s)
	if err != nil {
		return err
	}
	_, err = db.NamedExec(`
		INSERT INTO monitor_subscriptions
		(plan_code, datacenters, notify_available, notify_unavailable, last_status,
		 created_at, history, server_name, auto_order, quantity, auto_order_account_id, auto_pay, paused, options)
		VALUES
		(:plan_code, :datacenters, :notify_available, :notify_unavailable, :last_status,
		 :created_at, :history, :server_name, :auto_order, :quantity, :auto_order_account_id, :auto_pay, :paused, :options)
		ON CONFLICT(plan_code, options) DO UPDATE SET
		  datacenters        = excluded.datacenters,
		  notify_available   = excluded.notify_available,
		  notify_unavailable = excluded.notify_unavailable,
		  last_status        = excluded.last_status,
		  history            = excluded.history,
		  server_name        = excluded.server_name,
		  auto_order             = excluded.auto_order,
		  quantity               = excluded.quantity,
		  auto_order_account_id  = excluded.auto_order_account_id,
		  auto_pay               = excluded.auto_pay,
          paused                 = excluded.paused,
		  options                = excluded.options
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
			 created_at, history, server_name, auto_order, quantity, auto_order_account_id, auto_pay, paused, options)
			VALUES
			(:plan_code, :datacenters, :notify_available, :notify_unavailable, :last_status,
			 :created_at, :history, :server_name, :auto_order, :quantity, :auto_order_account_id, :auto_pay, :paused, :options)
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

// migrateMonitorIdentity 保留旧订阅,允许同型号的不同配置分别保存。
func (db *DB) migrateMonitorIdentity() error {
	var cols []struct {
		Name string `db:"name"`
		PK   int    `db:"pk"`
	}
	// sqlx 严格映射 PRAGMA 的其余字段,因此这里只选需要的两列。
	if err := db.Select(&cols, `SELECT name, pk FROM pragma_table_info('monitor_subscriptions')`); err != nil {
		return err
	}
	for _, c := range cols {
		if c.Name == "options" && c.PK > 0 {
			return nil
		}
	}
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE monitor_subscriptions_v2 (
 plan_code TEXT NOT NULL, datacenters TEXT NOT NULL DEFAULT '[]',
 notify_available INTEGER NOT NULL DEFAULT 1, notify_unavailable INTEGER NOT NULL DEFAULT 0,
 last_status TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL,
 history TEXT NOT NULL DEFAULT '[]', server_name TEXT NOT NULL DEFAULT '',
 auto_order INTEGER NOT NULL DEFAULT 0, quantity INTEGER NOT NULL DEFAULT 1,
 auto_order_account_id TEXT NOT NULL DEFAULT '', auto_pay INTEGER NOT NULL DEFAULT 0,
 paused INTEGER NOT NULL DEFAULT 0, options TEXT NOT NULL DEFAULT '[]',
 PRIMARY KEY(plan_code, options))`); err != nil {
		return err
	}
	const columns = `plan_code, datacenters, notify_available, notify_unavailable, last_status, created_at, history, server_name, auto_order, quantity, auto_order_account_id, auto_pay, paused, options`
	if _, err = tx.Exec(`INSERT INTO monitor_subscriptions_v2 (` + columns + `) SELECT ` + columns + ` FROM monitor_subscriptions`); err != nil {
		return err
	}
	var rows []monitorSubRow
	if err = tx.Select(&rows, `SELECT * FROM monitor_subscriptions`); err != nil {
		return err
	}
	for _, old := range rows {
		normalized, e := monitorSubToRow(rowToMonitorSub(old))
		if e != nil {
			return e
		}
		if _, err = tx.Exec(`UPDATE monitor_subscriptions_v2 SET options=? WHERE plan_code=?`, normalized.OptionsJSON, old.PlanCode); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`DROP TABLE monitor_subscriptions`); err != nil {
		return err
	}
	if _, err = tx.Exec(`ALTER TABLE monitor_subscriptions_v2 RENAME TO monitor_subscriptions`); err != nil {
		return err
	}
	return tx.Commit()
}

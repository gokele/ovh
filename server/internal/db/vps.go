package db

import (
	"encoding/json"
	"fmt"

	"github.com/ovh-buy/server/internal/types"
)

type vpsSubRow struct {
	ID                string `db:"id"`
	PlanCode          string `db:"plan_code"`
	OvhSubsidiary     string `db:"ovh_subsidiary"`
	DatacentersJSON   string `db:"datacenters"`
	MonitorLinux      int    `db:"monitor_linux"`
	MonitorWindows    int    `db:"monitor_windows"`
	NotifyAvailable   int    `db:"notify_available"`
	NotifyUnavailable int    `db:"notify_unavailable"`
	LastStatusJSON    string `db:"last_status"`
	HistoryJSON       string `db:"history"`
	CreatedAt         string `db:"created_at"`
}

func rowToVPSSub(r vpsSubRow) types.VPSSubscription {
	var dcs []string
	_ = json.Unmarshal([]byte(r.DatacentersJSON), &dcs)
	if dcs == nil {
		dcs = []string{}
	}
	last := map[string]string{}
	_ = json.Unmarshal([]byte(r.LastStatusJSON), &last)
	var hist []map[string]interface{}
	_ = json.Unmarshal([]byte(r.HistoryJSON), &hist)
	if hist == nil {
		hist = []map[string]interface{}{}
	}
	return types.VPSSubscription{
		ID:                r.ID,
		PlanCode:          r.PlanCode,
		OvhSubsidiary:     r.OvhSubsidiary,
		Datacenters:       dcs,
		MonitorLinux:      r.MonitorLinux == 1,
		MonitorWindows:    r.MonitorWindows == 1,
		NotifyAvailable:   r.NotifyAvailable == 1,
		NotifyUnavailable: r.NotifyUnavailable == 1,
		LastStatus:        last,
		History:           hist,
		CreatedAt:         r.CreatedAt,
	}
}

func vpsSubToRow(s types.VPSSubscription) (vpsSubRow, error) {
	if s.Datacenters == nil {
		s.Datacenters = []string{}
	}
	if s.LastStatus == nil {
		s.LastStatus = map[string]string{}
	}
	if s.History == nil {
		s.History = []map[string]interface{}{}
	}
	dcsJSON, _ := json.Marshal(s.Datacenters)
	lastJSON, _ := json.Marshal(s.LastStatus)
	histJSON, _ := json.Marshal(s.History)
	bi := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	return vpsSubRow{
		ID:                s.ID,
		PlanCode:          s.PlanCode,
		OvhSubsidiary:     s.OvhSubsidiary,
		DatacentersJSON:   string(dcsJSON),
		MonitorLinux:      bi(s.MonitorLinux),
		MonitorWindows:    bi(s.MonitorWindows),
		NotifyAvailable:   bi(s.NotifyAvailable),
		NotifyUnavailable: bi(s.NotifyUnavailable),
		LastStatusJSON:    string(lastJSON),
		HistoryJSON:       string(histJSON),
		CreatedAt:         s.CreatedAt,
	}, nil
}

// ListVPSSubscriptions 取全部 VPS 订阅
func (db *DB) ListVPSSubscriptions() ([]types.VPSSubscription, error) {
	var rows []vpsSubRow
	if err := db.Select(&rows, `SELECT * FROM vps_subscriptions ORDER BY created_at`); err != nil {
		return nil, fmt.Errorf("list vps subs: %w", err)
	}
	out := make([]types.VPSSubscription, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToVPSSub(r))
	}
	return out, nil
}

// UpsertVPSSubscription 按 id upsert
func (db *DB) UpsertVPSSubscription(s types.VPSSubscription) error {
	r, err := vpsSubToRow(s)
	if err != nil {
		return err
	}
	_, err = db.NamedExec(`
		INSERT INTO vps_subscriptions
		(id, plan_code, ovh_subsidiary, datacenters, monitor_linux, monitor_windows,
		 notify_available, notify_unavailable, last_status, history, created_at)
		VALUES
		(:id, :plan_code, :ovh_subsidiary, :datacenters, :monitor_linux, :monitor_windows,
		 :notify_available, :notify_unavailable, :last_status, :history, :created_at)
		ON CONFLICT(id) DO UPDATE SET
		  plan_code          = excluded.plan_code,
		  ovh_subsidiary     = excluded.ovh_subsidiary,
		  datacenters        = excluded.datacenters,
		  monitor_linux      = excluded.monitor_linux,
		  monitor_windows    = excluded.monitor_windows,
		  notify_available   = excluded.notify_available,
		  notify_unavailable = excluded.notify_unavailable,
		  last_status        = excluded.last_status,
		  history            = excluded.history
	`, r)
	if err != nil {
		return fmt.Errorf("upsert vps sub %s: %w", s.ID, err)
	}
	return nil
}

// ReplaceVPSSubscriptions 全表覆盖
func (db *DB) ReplaceVPSSubscriptions(subs []types.VPSSubscription) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM vps_subscriptions`); err != nil {
		return err
	}
	for _, s := range subs {
		r, err := vpsSubToRow(s)
		if err != nil {
			return err
		}
		_, err = tx.NamedExec(`
			INSERT INTO vps_subscriptions
			(id, plan_code, ovh_subsidiary, datacenters, monitor_linux, monitor_windows,
			 notify_available, notify_unavailable, last_status, history, created_at)
			VALUES
			(:id, :plan_code, :ovh_subsidiary, :datacenters, :monitor_linux, :monitor_windows,
			 :notify_available, :notify_unavailable, :last_status, :history, :created_at)
		`, r)
		if err != nil {
			return fmt.Errorf("insert vps sub %s: %w", s.ID, err)
		}
	}
	return tx.Commit()
}

// DeleteVPSSubscription 按 id 删
func (db *DB) DeleteVPSSubscription(id string) error {
	_, err := db.Exec(`DELETE FROM vps_subscriptions WHERE id = ?`, id)
	return err
}

// ClearVPSSubscriptions 清空
func (db *DB) ClearVPSSubscriptions() (int64, error) {
	res, err := db.Exec(`DELETE FROM vps_subscriptions`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

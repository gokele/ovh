package db

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ovh-buy/server/internal/types"
)

type sniperRow struct {
	ID               string         `db:"id"`
	API1PlanCode     string         `db:"api1_plan_code"`
	BoundConfigJSON  string         `db:"bound_config"`
	MatchStatus      string         `db:"match_status"`
	MatchedAPI2JSON  string         `db:"matched_api2"`
	KnownPlanCodes   string         `db:"known_plancodes"`
	Enabled          int            `db:"enabled"`
	LastCheck        sql.NullString `db:"last_check"`
	CreatedAt        string         `db:"created_at"`
}

func rowToSniper(r sniperRow) types.ConfigSniperTask {
	bound := map[string]interface{}{}
	_ = json.Unmarshal([]byte(r.BoundConfigJSON), &bound)
	var matched []string
	_ = json.Unmarshal([]byte(r.MatchedAPI2JSON), &matched)
	if matched == nil {
		matched = []string{}
	}
	var known []string
	_ = json.Unmarshal([]byte(r.KnownPlanCodes), &known)
	if known == nil {
		known = []string{}
	}
	var lastCheck *string
	if r.LastCheck.Valid {
		s := r.LastCheck.String
		lastCheck = &s
	}
	return types.ConfigSniperTask{
		ID:             r.ID,
		API1PlanCode:   r.API1PlanCode,
		BoundConfig:    bound,
		MatchStatus:    r.MatchStatus,
		MatchedAPI2:    matched,
		KnownPlanCodes: known,
		Enabled:        r.Enabled == 1,
		LastCheck:      lastCheck,
		CreatedAt:      r.CreatedAt,
	}
}

func sniperToRow(t types.ConfigSniperTask) (sniperRow, error) {
	if t.BoundConfig == nil {
		t.BoundConfig = map[string]interface{}{}
	}
	if t.MatchedAPI2 == nil {
		t.MatchedAPI2 = []string{}
	}
	if t.KnownPlanCodes == nil {
		t.KnownPlanCodes = []string{}
	}
	boundJSON, err := json.Marshal(t.BoundConfig)
	if err != nil {
		return sniperRow{}, err
	}
	matchedJSON, _ := json.Marshal(t.MatchedAPI2)
	knownJSON, _ := json.Marshal(t.KnownPlanCodes)
	bi := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	row := sniperRow{
		ID:              t.ID,
		API1PlanCode:    t.API1PlanCode,
		BoundConfigJSON: string(boundJSON),
		MatchStatus:     t.MatchStatus,
		MatchedAPI2JSON: string(matchedJSON),
		KnownPlanCodes:  string(knownJSON),
		Enabled:         bi(t.Enabled),
		CreatedAt:       t.CreatedAt,
	}
	if t.LastCheck != nil {
		row.LastCheck = sql.NullString{String: *t.LastCheck, Valid: true}
	}
	return row, nil
}

// ListSniperTasks 取全部配置绑定狙击任务
func (db *DB) ListSniperTasks() ([]types.ConfigSniperTask, error) {
	var rows []sniperRow
	if err := db.Select(&rows, `SELECT * FROM config_sniper_tasks ORDER BY created_at`); err != nil {
		return nil, fmt.Errorf("list sniper tasks: %w", err)
	}
	out := make([]types.ConfigSniperTask, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToSniper(r))
	}
	return out, nil
}

// UpsertSniperTask 按 id upsert
func (db *DB) UpsertSniperTask(t types.ConfigSniperTask) error {
	r, err := sniperToRow(t)
	if err != nil {
		return err
	}
	_, err = db.NamedExec(`
		INSERT INTO config_sniper_tasks
		(id, api1_plan_code, bound_config, match_status, matched_api2,
		 known_plancodes, enabled, last_check, created_at)
		VALUES
		(:id, :api1_plan_code, :bound_config, :match_status, :matched_api2,
		 :known_plancodes, :enabled, :last_check, :created_at)
		ON CONFLICT(id) DO UPDATE SET
		  api1_plan_code  = excluded.api1_plan_code,
		  bound_config    = excluded.bound_config,
		  match_status    = excluded.match_status,
		  matched_api2    = excluded.matched_api2,
		  known_plancodes = excluded.known_plancodes,
		  enabled         = excluded.enabled,
		  last_check      = excluded.last_check
	`, r)
	if err != nil {
		return fmt.Errorf("upsert sniper task %s: %w", t.ID, err)
	}
	return nil
}

// ReplaceSniperTasks 全表覆盖
func (db *DB) ReplaceSniperTasks(tasks []types.ConfigSniperTask) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM config_sniper_tasks`); err != nil {
		return err
	}
	for _, t := range tasks {
		r, err := sniperToRow(t)
		if err != nil {
			return err
		}
		_, err = tx.NamedExec(`
			INSERT INTO config_sniper_tasks
			(id, api1_plan_code, bound_config, match_status, matched_api2,
			 known_plancodes, enabled, last_check, created_at)
			VALUES
			(:id, :api1_plan_code, :bound_config, :match_status, :matched_api2,
			 :known_plancodes, :enabled, :last_check, :created_at)
		`, r)
		if err != nil {
			return fmt.Errorf("insert sniper task %s: %w", t.ID, err)
		}
	}
	return tx.Commit()
}

// DeleteSniperTask 按 id 删
func (db *DB) DeleteSniperTask(id string) error {
	_, err := db.Exec(`DELETE FROM config_sniper_tasks WHERE id = ?`, id)
	return err
}

package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// OrderWatchSnapshot 记录单个 OVH 订单最近一次已知状态、交付阶段与历史事件键，用于增量变更推送。
type OrderWatchSnapshot struct {
	AccountID      string
	OrderID        string
	OrderDate      string
	ExpirationDate string
	OrderURL       string
	Summary        string
	PriceText      string
	Status         string
	Steps          map[string]string
	Events         []string
	Terminal       bool
	UpdatedAt      string
}

type orderWatchRow struct {
	AccountID      string `db:"account_id"`
	OrderID        string `db:"order_id"`
	OrderDate      string `db:"order_date"`
	ExpirationDate string `db:"expiration_date"`
	OrderURL       string `db:"order_url"`
	Summary        string `db:"summary"`
	PriceText      string `db:"price_text"`
	Status         string `db:"status"`
	StepsJSON      string `db:"steps_json"`
	EventsJSON     string `db:"events_json"`
	Terminal       int    `db:"terminal"`
	UpdatedAt      string `db:"updated_at"`
}

// IsOrderWatchAccountSeeded 查询账户是否已完成首次基线初始化。
func (db *DB) IsOrderWatchAccountSeeded(accountID string) (bool, error) {
	var seededAt string
	err := db.Get(&seededAt, `SELECT seeded_at FROM order_watch_accounts WHERE account_id = ?`, accountID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check order watch account seeded %s: %w", accountID, err)
	}
	return seededAt != "", nil
}

// MarkOrderWatchAccountSeeded 标记账户已完成订单基线初始化。
func (db *DB) MarkOrderWatchAccountSeeded(accountID, seededAt string) error {
	_, err := db.Exec(
		`INSERT INTO order_watch_accounts (account_id, seeded_at) VALUES (?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET seeded_at = excluded.seeded_at`,
		accountID, seededAt,
	)
	if err != nil {
		return fmt.Errorf("mark order watch account seeded %s: %w", accountID, err)
	}
	return nil
}

// ListOrderWatchSnapshots 读取指定账户下的全部订单监听快照（按 order_id 索引）。
func (db *DB) ListOrderWatchSnapshots(accountID string) (map[string]OrderWatchSnapshot, error) {
	var rows []orderWatchRow
	err := db.Select(&rows, `
		SELECT account_id, order_id, order_date, expiration_date, order_url, summary, price_text,
		       status, steps_json, events_json, terminal, updated_at
		FROM order_watch_orders
		WHERE account_id = ?
	`, accountID)
	if err != nil {
		return nil, fmt.Errorf("list order watch snapshots %s: %w", accountID, err)
	}
	out := make(map[string]OrderWatchSnapshot, len(rows))
	for _, r := range rows {
		steps := map[string]string{}
		if r.StepsJSON != "" {
			_ = json.Unmarshal([]byte(r.StepsJSON), &steps)
		}
		if steps == nil {
			steps = map[string]string{}
		}
		var events []string
		if r.EventsJSON != "" {
			_ = json.Unmarshal([]byte(r.EventsJSON), &events)
		}
		if events == nil {
			events = []string{}
		}
		out[r.OrderID] = OrderWatchSnapshot{
			AccountID:      r.AccountID,
			OrderID:        r.OrderID,
			OrderDate:      r.OrderDate,
			ExpirationDate: r.ExpirationDate,
			OrderURL:       r.OrderURL,
			Summary:        r.Summary,
			PriceText:      r.PriceText,
			Status:         r.Status,
			Steps:          steps,
			Events:         events,
			Terminal:       r.Terminal == 1,
			UpdatedAt:      r.UpdatedAt,
		}
	}
	return out, nil
}

// UpsertOrderWatchSnapshot 插入或更新单个订单的监听快照。
func (db *DB) UpsertOrderWatchSnapshot(s OrderWatchSnapshot) error {
	steps := s.Steps
	if steps == nil {
		steps = map[string]string{}
	}
	stepsRaw, err := json.Marshal(steps)
	if err != nil {
		return fmt.Errorf("marshal order watch steps: %w", err)
	}
	events := s.Events
	if events == nil {
		events = []string{}
	}
	eventsRaw, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("marshal order watch events: %w", err)
	}
	row := orderWatchRow{
		AccountID:      s.AccountID,
		OrderID:        s.OrderID,
		OrderDate:      s.OrderDate,
		ExpirationDate: s.ExpirationDate,
		OrderURL:       s.OrderURL,
		Summary:        s.Summary,
		PriceText:      s.PriceText,
		Status:         s.Status,
		StepsJSON:      string(stepsRaw),
		EventsJSON:     string(eventsRaw),
		Terminal:       boolInt(s.Terminal),
		UpdatedAt:      s.UpdatedAt,
	}
	_, err = db.NamedExec(`
		INSERT INTO order_watch_orders (
			account_id, order_id, order_date, expiration_date, order_url, summary, price_text,
			status, steps_json, events_json, terminal, updated_at
		) VALUES (
			:account_id, :order_id, :order_date, :expiration_date, :order_url, :summary, :price_text,
			:status, :steps_json, :events_json, :terminal, :updated_at
		)
		ON CONFLICT(account_id, order_id) DO UPDATE SET
			order_date      = excluded.order_date,
			expiration_date = excluded.expiration_date,
			order_url       = excluded.order_url,
			summary         = excluded.summary,
			price_text      = excluded.price_text,
			status          = excluded.status,
			steps_json      = excluded.steps_json,
			events_json     = excluded.events_json,
			terminal        = excluded.terminal,
			updated_at      = excluded.updated_at
	`, row)
	if err != nil {
		return fmt.Errorf("upsert order watch snapshot %s/%s: %w", s.AccountID, s.OrderID, err)
	}
	return nil
}

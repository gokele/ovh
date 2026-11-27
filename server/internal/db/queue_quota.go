package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/ovh-buy/server/internal/types"
)

type QueueQuota struct{ Reserved, Ordered, Uncertain int }

func (db *DB) QueueQuota(item types.QueueItem) (QueueQuota, error) {
	var result QueueQuota
	if item.OrderGroup == "" {
		return result, nil
	}
	if item.GroupLimit < 1 || item.GroupLimit > types.MaxOrderFanout {
		return result, fmt.Errorf("invalid total order limit")
	}
	err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(c.state='ordered'),0),
		COALESCE(SUM(c.state='uncertain' OR (c.state='pending' AND NOT EXISTS
		(SELECT 1 FROM queue q WHERE q.id=c.task_id AND q.status IN ('running','pending')))),0)
		FROM queue_order_claims c WHERE c.group_id=?`, item.OrderGroup).Scan(&result.Reserved, &result.Ordered, &result.Uncertain)
	return result, err
}

// Reserve the group slot and the task's durable checkout guard in one transaction.
func (db *DB) ClaimQueueOrder(item types.QueueItem) (bool, error) {
	if item.OrderGroup == "" {
		return db.ClaimQueueCheckout(item.ID)
	}
	if item.GroupLimit < 1 || item.GroupLimit > types.MaxOrderFanout {
		return false, fmt.Errorf("invalid total order limit")
	}
	tx, err := db.Beginx()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT OR IGNORE INTO queue_order_groups(id,max_orders) VALUES(?,?)`, item.OrderGroup, item.GroupLimit); err != nil {
		return false, err
	}
	var limit int
	if err = tx.Get(&limit, `SELECT max_orders FROM queue_order_groups WHERE id=?`, item.OrderGroup); err != nil {
		return false, err
	}
	if limit != item.GroupLimit {
		return false, fmt.Errorf("total order limit changed")
	}
	result, err := tx.Exec(`INSERT OR IGNORE INTO queue_order_claims(task_id,group_id,state)
		SELECT ?,?,'pending' WHERE (SELECT COUNT(*) FROM queue_order_claims WHERE group_id=?) < ?
		AND NOT EXISTS (SELECT 1 FROM queue_order_claims WHERE group_id=? AND state='uncertain')`, item.ID, item.OrderGroup, item.OrderGroup, limit, item.OrderGroup)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	result, err = tx.Exec(`INSERT OR IGNORE INTO kv(key,value) VALUES(?,'{"state":"pending"}')`, QueueCheckoutKey(item.ID))
	if err != nil {
		return false, err
	}
	n, err = result.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	return true, tx.Commit()
}

func (db *DB) ReleaseQueueOrder(item types.QueueItem) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM queue_order_claims WHERE task_id=? AND state='pending'`, item.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM kv WHERE key=?`, QueueCheckoutKey(item.ID)); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) MarkQueueOrder(item types.QueueItem, state string) error {
	if item.OrderGroup == "" {
		return nil
	}
	if state != "ordered" && state != "uncertain" {
		return fmt.Errorf("invalid checkout state")
	}
	_, err := db.Exec(`UPDATE queue_order_claims SET state=? WHERE task_id=? AND group_id=? AND state!='ordered'`, state, item.ID, item.OrderGroup)
	return err
}

// SetOrderGroupLimit 整单编辑改了「合计几台」时同步已登记的上限。
// ClaimQueueOrder 要求任务上的 GroupLimit 与这里一致,不同步的话之后每次结账都会报「上限已变」。
// 返回原来的值(没有登记过时 ok=false),用于落库失败时回滚。
func (db *DB) SetOrderGroupLimit(group string, limit int) (previous int, ok bool, err error) {
	if group == "" {
		return 0, false, nil
	}
	if limit < 1 || limit > types.MaxOrderFanout {
		return 0, false, fmt.Errorf("invalid total order limit")
	}
	err = db.Get(&previous, `SELECT max_orders FROM queue_order_groups WHERE id=?`, group)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil // 还没有任何结账登记,首次结账时会按新值登记
		}
		return 0, false, err
	}
	_, err = db.Exec(`UPDATE queue_order_groups SET max_orders=? WHERE id=?`, limit, group)
	return previous, true, err
}

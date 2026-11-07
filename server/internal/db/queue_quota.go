package db

import (
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

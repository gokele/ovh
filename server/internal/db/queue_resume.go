package db

import (
	"database/sql"

	"github.com/ovh-buy/server/internal/types"
)

// ResumeQueueItem commits the status, automatic-order permission and checkout reset together.
func (db *DB) ResumeQueueItem(item types.QueueItem, blockKey string, resetCheckout bool) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE queue SET status=?, updated_at=?, failure_count=?, last_check_time=?, stop_reason='' WHERE id=?`, item.Status, item.UpdatedAt, item.FailureCount, item.LastCheckTime, item.ID)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count != 1 {
		return sql.ErrNoRows
	}
	if _, err = tx.Exec(`INSERT INTO kv(key,value) VALUES(?,'false') ON CONFLICT(key) DO UPDATE SET value=excluded.value`, blockKey); err != nil {
		return err
	}
	if resetCheckout {
		if _, err = tx.Exec(`DELETE FROM queue_order_claims WHERE task_id=? AND state!='ordered'`, item.ID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM kv WHERE key=?`, QueueCheckoutKey(item.ID)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

package db

func VPSCheckoutKey(subscriptionID string) string { return "vps-checkout:" + subscriptionID }

func QueueCheckoutKey(taskID string) string { return "queue-checkout:" + taskID }

// ClaimVPSCheckout survives crashes and ambiguous responses; only a known rejection releases it.
func (db *DB) ClaimVPSCheckout(subscriptionID string) (bool, error) {
	return db.claimCheckout(VPSCheckoutKey(subscriptionID))
}

func (db *DB) ClaimQueueCheckout(taskID string) (bool, error) {
	return db.claimCheckout(QueueCheckoutKey(taskID))
}

func (db *DB) claimCheckout(key string) (bool, error) {
	result, err := db.Exec(`INSERT OR IGNORE INTO kv(key,value) VALUES(?,?)`, key, `{"state":"pending"}`)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

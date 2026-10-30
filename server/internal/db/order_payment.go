package db

import "fmt"

// OrderPaymentKey 按账户与订单隔离付款记录,重启后仍能阻止重复付款。
func OrderPaymentKey(accountID string, orderID int64) string {
	return fmt.Sprintf("order-payment:%s:%d", accountID, orderID)
}

// ClaimOrderPayment 必须在发出付款请求前落库;同一订单只能有一个请求持有记录。
func (db *DB) ClaimOrderPayment(accountID string, orderID int64) (bool, error) {
	result, err := db.Exec(`INSERT OR IGNORE INTO kv(key,value) VALUES(?,?)`, OrderPaymentKey(accountID, orderID), `{"state":"pending"}`)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

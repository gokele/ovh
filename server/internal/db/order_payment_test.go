package db

import "testing"

// 付款登记必须跨进程重开数据库保留,并按账户隔离。
func TestOrderPaymentClaimSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := database.ClaimOrderPayment("a", 123); err != nil || !ok {
		t.Fatal(ok, err)
	}
	database.Close()
	database, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if ok, err := database.ClaimOrderPayment("a", 123); err != nil || ok {
		t.Fatal("duplicate claim after reopen", ok, err)
	}
	if ok, err := database.ClaimOrderPayment("b", 123); err != nil || !ok {
		t.Fatal("other account blocked", ok, err)
	}
}

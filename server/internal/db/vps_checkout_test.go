package db

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestVPSCheckoutClaimIsAtomicAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, err := database.ClaimVPSCheckout("subscription")
			if err != nil {
				t.Error(err)
			} else if won {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("checkout had %d owners", winners.Load())
	}
	database.Close()
	database, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if won, err := database.ClaimVPSCheckout("subscription"); err != nil || won {
		t.Fatal("restart allowed the same checkout again")
	}
	if won, err := database.ClaimVPSCheckout("other"); err != nil || !won {
		t.Fatal("unrelated subscription was blocked")
	}
}

package purchase

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

func TestCachedOptionRejectionInvalidatesMetadataBeforeRetry(t *testing.T) {
	state, _, checkouts, _ := hotFixture(t)
	base, err := state.OVH.ClientFor("hot-eu")
	if err != nil {
		t.Fatal(err)
	}
	original := base.Client.Transport
	var reads atomic.Int32
	var reject atomic.Bool
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/eco/options") {
			if r.Method == http.MethodGet {
				reads.Add(1)
			}
			if r.Method == http.MethodPost && reject.Load() {
				return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"option changed"}`)), Request: r}, nil
			}
		}
		return original.RoundTrip(r)
	})
	pool := NewCartPool(state)
	state.OrderPrewarm = pool
	t.Cleanup(pool.Close)
	item := poolItem()
	account := state.Accounts[0]
	prepare := func() (*preparedCart, Outcome) {
		client, err := state.OVH.IsolatedClientForAccount(account)
		if err != nil {
			t.Fatal(err)
		}
		return prepareCart(context.Background(), state, &item, account, client, item.Options, "gra", "plan.ram-32g", newTimeline(), "", true)
	}
	first, out := prepare()
	if first == nil {
		t.Fatal(out)
	}
	first.cleanup()
	if reads.Load() != 1 {
		t.Fatal(reads.Load())
	}
	reject.Store(true)
	second, out := prepare()
	if second != nil || !out.RetryImmediately || reads.Load() != 1 {
		t.Fatal(out, reads.Load())
	}
	reject.Store(false)
	third, out := prepare()
	if third == nil || reads.Load() != 2 {
		t.Fatal(out, reads.Load())
	}
	third.cleanup()
	if checkouts.Load() != 0 {
		t.Fatal("metadata operation submitted checkout")
	}
	other := item
	other.Datacenter = "bhs"
	if optionCacheKey(&other, account) == optionCacheKey(&item, account) {
		t.Fatal("cross-DC metadata shared")
	}
	otherAccount := types.OVHAccount{ID: "other", Endpoint: account.Endpoint}
	if optionCacheKey(&item, otherAccount) == optionCacheKey(&item, account) {
		t.Fatal("cross-account metadata shared")
	}
}

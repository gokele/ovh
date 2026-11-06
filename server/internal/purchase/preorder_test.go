package purchase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

type preorderTransport func(*http.Request) (*http.Response, error)

func (f preorderTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPreorderCanPrepareFromStockBeforePublicListing(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, tc := range []struct {
		sub            string
		listed, outage bool
		options        []string
		expectCart     bool
	}{
		{"IE", false, false, nil, true},
		{"DE", false, true, nil, true},
		{"FR", true, false, nil, true},
		{"ES", true, false, []string{"ram-32g"}, true},
		{"NL", true, false, []string{"ram-64g"}, false},
		{"GB", true, false, []string{"ram-32g", "softraid-2x480ssd"}, false},
	} {
		t.Run(tc.sub, func(t *testing.T) {
			dir := t.TempDir()
			database, err := db.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			lg := logger.New(filepath.Join(dir, "test.log"), slog.New(slog.NewTextHandler(io.Discard, nil)))
			st := app.NewState(storage.Paths{DataDir: dir}, config.New(database), lg, database)
			st.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu", Zone: tc.sub, AppKey: "test", AppSecret: "test", ConsumerKey: "test"}}
			carts, checkouts := 0, 0
			transport := preorderTransport(func(r *http.Request) (*http.Response, error) {
				body, status := "{}", http.StatusOK
				switch {
				case strings.Contains(r.URL.Path, "/order/catalog/public/eco"):
					body = `{"plans":[]}`
					if tc.outage {
						status = http.StatusTooManyRequests
					}
					if tc.listed {
						body = `{"plans":[{"planCode":"27skle03-v1","configurations":[{"name":"dedicated_datacenter","values":["gra"]}]}]}`
					}
				case strings.HasSuffix(r.URL.Path, "/auth/time"):
					body = fmt.Sprint(time.Now().Unix())
				case strings.Contains(r.URL.Path, "/dedicated/server/datacenter/availabilities"):
					body = `[{"fqn":"27skle03-v1.ram-32g","datacenters":[{"datacenter":"gra","availability":"1H-low"}]}]`
				case strings.HasSuffix(r.URL.Path, "/order/cart"):
					carts++
					body = `{"cartId":"pending"}`
				case strings.HasSuffix(r.URL.Path, "/pending/assign") || r.Method == "DELETE":
				case strings.HasSuffix(r.URL.Path, "/pending/eco") && r.Method == "POST":
					status, body = http.StatusBadRequest, `{"message":"model not open for ordering yet"}`
				case strings.HasSuffix(r.URL.Path, "/pending/eco") && r.Method == "GET":
					body = `[]`
				default:
					if strings.Contains(r.URL.Path, "checkout") {
						checkouts++
					}
					return nil, fmt.Errorf("unexpected test request: %s", r.URL)
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			http.DefaultTransport = transport // public catalog; every request stays inside this fixture
			client, err := st.OVH.ClientFor("eu")
			if err != nil {
				t.Fatal(err)
			}
			client.Client = &http.Client{Transport: transport}
			item := &types.QueueItem{ID: "pre", AccountID: "eu", PlanCode: "27skle03-v1", Datacenter: "gra", Preorder: true, MaxRetries: 1, Options: tc.options}
			outcome := PurchaseServer(context.Background(), st, item)
			if tc.expectCart {
				if !outcome.WaitingForCatalog || outcome.Attempted || outcome.Fatal || carts != 1 {
					t.Fatalf("stock plan did not reach live ordering API or did not wait safely: %+v carts=%d", outcome, carts)
				}
			} else if outcome.Attempted || outcome.Fatal || carts != 0 {
				t.Fatalf("nonmatching hardware must not prepare a cart: %+v carts=%d", outcome, carts)
			}
			if checkouts != 0 {
				t.Fatal("fixture must never reach checkout")
			}
		})
	}
}

func TestAllCurrentPrereleaseConfigurationsKeepExactStockIdentifiers(t *testing.T) {
	data, err := os.ReadFile("testdata/preorder-stock.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct{ PlanCode, FQN, Memory, Storage, SystemStorage string }
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	models := map[string]bool{}
	for _, row := range rows {
		models[row.PlanCode] = true
		options := []string{row.Memory, row.Storage}
		if row.SystemStorage != "" {
			options = append(options, row.SystemStorage)
		}
		if !preorderFQNCoversOptions(row.FQN, options) {
			t.Errorf("API configuration did not match: %s", row.FQN)
		}
		if strings.SplitN(row.FQN, ".", 2)[0] != row.PlanCode {
			t.Fatalf("model changed: %+v", row)
		}
		wrong := append([]string(nil), options...)
		wrong[1] += "-2x9999sa"
		if preorderFQNCoversOptions(row.FQN, wrong) {
			t.Fatalf("extra disks were accepted: %s", row.FQN)
		}
	}
	if len(models) != 12 || len(rows) != 56 {
		t.Fatal("fixture lost prerelease configurations", len(models), len(rows))
	}
}

func TestPreorderRequiresAllExactHardwareSegments(t *testing.T) {
	const fqn = "27skle03-v1.ram-64g-ddr4-ecc-2666.softraid-2x480ssd"
	for _, tc := range []struct {
		options []string
		want    bool
	}{
		{[]string{"ram-64g-ddr4-ecc-2666", "softraid-2x480ssd"}, true},
		{[]string{"ram-128g-ddr4-ecc-2666"}, false},
		{[]string{"ram-64g-ddr4-ecc-2133"}, false},
		{[]string{"softraid-2x480ssd-2x8000sa"}, false},
	} {
		if got := preorderFQNCoversOptions(fqn, tc.options); got != tc.want {
			t.Errorf("%v: got %v", tc.options, got)
		}
	}
}

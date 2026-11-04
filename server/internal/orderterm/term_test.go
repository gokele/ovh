package orderterm

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormalizePurchasePeriod(t *testing.T) {
	if Label("") != "1 个月" || Label("P1Y") != "1 年" || Label("P24M") != "2 年" {
		t.Fatal("incorrect purchase period label")
	}
	for input, want := range map[string]string{"": "P1M", " p1y ": "P12M", "P12M": "P12M", "P2Y": "P24M", "P3M": "P3M"} {
		got, err := Normalize(input)
		if err != nil || got != want {
			t.Fatalf("%q: %s %v", input, got, err)
		}
	}
	for _, input := range []string{"P0M", "P-1M", "P+1M", "P1.5Y", "P1D", "P999999999999Y", "P11Y", "year"} {
		if _, err := Normalize(input); err == nil {
			t.Fatal("accepted invalid period", input)
		}
	}
}

func TestPickRetainsExactPeriodAndMode(t *testing.T) {
	var prices interface{}
	_ = json.Unmarshal([]byte(`[
 {"duration":"P1M","pricingMode":"default","pricingType":"rental","capacities":["renew"]},
 {"duration":"P1Y","pricingMode":"upfront12","pricingType":"rental","capacities":["renew"]},
 {"duration":"P12M","pricingMode":"default","pricingType":"rental","capacities":["renew"]}
]`), &prices)
	if d, m, ok := Pick(prices, "P12M", "upfront12"); !ok || d != "P1Y" || m != "upfront12" {
		t.Fatal(d, m, ok)
	}
	if d, m, ok := Pick(prices, "P1Y", ""); !ok || d != "P12M" || m != "default" {
		t.Fatal(d, m, ok)
	}
	if _, _, ok := Pick(prices, "P24M", "default"); ok {
		t.Fatal("silently changed a 2-year order to another period")
	}
	if _, _, ok := Pick(nil, "P1M", "default"); ok {
		t.Fatal("missing prices must not invent a monthly offer")
	}
	if got := Durations(prices); !reflect.DeepEqual(got, []string{"P1M", "P12M"}) {
		t.Fatal(got)
	}
}

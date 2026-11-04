package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPriceGateRejectsInvalidAmounts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value interface{}
		valid bool
	}{
		{"number", 12.5, true}, {"numeric string", "12.5", true},
		{"zero", 0, false}, {"negative", -1, false}, {"missing", nil, false},
		{"not numeric", "pending", false}, {"NaN", "NaN", false}, {"infinity", "+Inf", false},
		{"object", map[string]int{"value": 10}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "price": map[string]interface{}{"prices": map[string]interface{}{"withTax": tc.value}}})
			}))
			defer server.Close()
			m := renderTestMonitor(t)
			m.state.Port = strings.TrimPrefix(server.URL, "http://127.0.0.1:")
			if valid, _ := m.verifyPriceAvailable("plan", "gra", nil); valid != tc.valid {
				t.Fatalf("valid=%t, want %t", valid, tc.valid)
			}
		})
	}
}

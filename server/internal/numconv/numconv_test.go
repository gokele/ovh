package numconv

import (
	"encoding/json"
	"math"
	"testing"
)

func TestToInt64RejectsLossyConversions(t *testing.T) {
	for _, input := range []interface{}{1.5, float32(1.5), "1.5", json.Number("1.5"), json.Number("-9223372036854775809"), math.NaN(), math.Inf(1), "NaN", "Inf", uint64(math.MaxUint64), uint(math.MaxUint64), float64(math.MaxInt64), "9223372036854775808", "-9223372036854775809"} {
		if value, ok := ToInt64(input); ok {
			t.Errorf("accepted lossy conversion %v (%T) -> %d", input, input, value)
		}
	}
	for _, tc := range []struct {
		input interface{}
		want  int64
	}{{1, 1}, {1.0, 1}, {"1", 1}, {json.Number("1000"), 1000}, {uint64(math.MaxInt64), math.MaxInt64}, {"9223372036854775807", math.MaxInt64}, {"-9223372036854775808", math.MinInt64}} {
		if value, ok := ToInt64(tc.input); !ok || value != tc.want {
			t.Errorf("%v: got %d, %t; want %d", tc.input, value, ok, tc.want)
		}
	}
}

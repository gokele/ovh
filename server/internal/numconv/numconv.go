// Package numconv 把任意 interface{} 数字（OVH SDK 用 UseNumber，会出 json.Number；
// gin 默认走 float64；其它路径可能给 int/int64/字符串）统一转成 int64/float64/string。
//
// 背景：OVH 官方 go-ovh SDK 在 UnmarshalResponse 里启用了 d.UseNumber()，
// 所以 c.Post(..., &m) 得到 map[string]interface{} 时所有数字字段都是 json.Number 而不是 float64。
// 我们之前的 ".(float64)" 类型断言会全部失败，typed value 永远是 0/false，
// 进而出现 "Invalid Cart Item ID"、"Invalid signature"（itemId 拼成 0）等怪错。
package numconv

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// ToInt64 把任意数字/数字字符串转 int64
func ToInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		if uint64(x) <= math.MaxInt64 {
			return int64(x), true
		}
		return 0, false
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		if x <= math.MaxInt64 {
			return int64(x), true
		}
		return 0, false
	case float32:
		return floatToInt64(float64(x))
	case float64:
		return floatToInt64(x)
	case json.Number:
		n, err := x.Int64()
		if err == nil {
			return n, true
		}
		return 0, false
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, false
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err == nil {
			return n, true
		}
		return 0, false
	}
	return 0, false
}

// Floating-point IDs must be finite, integral and within the exact int64 bounds.
func floatToInt64(v float64) (int64, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v || v < math.MinInt64 || v >= -float64(math.MinInt64) {
		return 0, false
	}
	return int64(v), true
}

// ToFloat64 把任意数字/数字字符串转 float64
func ToFloat64(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f, true
		}
		return 0, false
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, true
		}
		return 0, false
	}
	return 0, false
}

// ToString 把任意数字/字符串转字符串，专用于 URL 拼接与日志
func ToString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return ""
}

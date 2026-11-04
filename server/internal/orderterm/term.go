// Package orderterm keeps purchase periods identical across quotes, carts and tasks.
package orderterm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const Default = "P1M"

// Normalize accepts whole months/years; an omitted period preserves monthly orders.
func Normalize(raw string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return Default, nil
	}
	if len(s) < 3 || s[0] != 'P' || (s[len(s)-1] != 'M' && s[len(s)-1] != 'Y') {
		return "", fmt.Errorf("购买时长必须使用整月或整年格式，例如 P1M、P12M、P1Y")
	}
	for _, c := range s[1 : len(s)-1] {
		if c < '0' || c > '9' {
			return "", fmt.Errorf("购买时长必须使用整月或整年格式，例如 P1M、P12M、P1Y")
		}
	}
	n, err := strconv.Atoi(s[1 : len(s)-1])
	if err != nil || n < 1 || n > 120 {
		return "", fmt.Errorf("购买时长超出支持范围（1–120 个月）")
	}
	if s[len(s)-1] == 'Y' {
		n *= 12
	}
	if n > 120 {
		return "", fmt.Errorf("购买时长超出支持范围（1–120 个月）")
	}
	return fmt.Sprintf("P%dM", n), nil
}

func Same(a, b string) bool {
	x, err := Normalize(a)
	y, other := Normalize(b)
	return err == nil && other == nil && x == y
}

func Label(raw string) string {
	normalized, err := Normalize(raw)
	if err != nil {
		return raw
	}
	months, _ := strconv.Atoi(normalized[1 : len(normalized)-1])
	if months%12 == 0 {
		return fmt.Sprintf("%d 年", months/12)
	}
	return fmt.Sprintf("%d 个月", months)
}

// Pick never substitutes a different period. Keep OVH's original ISO spelling.
func Pick(raw interface{}, wanted, preferredMode string) (duration, mode string, ok bool) {
	prices, _ := raw.([]interface{})
	best := -1
	for _, entry := range prices {
		p, _ := entry.(map[string]interface{})
		d, _ := p["duration"].(string)
		m, _ := p["pricingMode"].(string)
		if d == "" || m == "" || !Same(d, wanted) {
			continue
		}
		score := 0
		if m == preferredMode && preferredMode != "" {
			score += 8
		}
		if m == "default" {
			score += 4
		}
		if p["pricingType"] == "rental" {
			score++
		}
		caps, _ := p["capacities"].([]interface{})
		for _, c := range caps {
			if c == "renew" {
				score += 2
				break
			}
		}
		if score > best {
			duration, mode, best = d, m, score
		}
	}
	return duration, mode, best >= 0
}

func Durations(raw interface{}) []string {
	prices, _ := raw.([]interface{})
	set := map[string]bool{}
	for _, entry := range prices {
		p, _ := entry.(map[string]interface{})
		d, _ := p["duration"].(string)
		m, _ := p["pricingMode"].(string)
		if normalized, err := Normalize(d); err == nil && d != "" && m != "" {
			set[normalized] = true
		}
	}
	result := make([]string, 0, len(set))
	for d := range set {
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool {
		return len(result[i]) < len(result[j]) || len(result[i]) == len(result[j]) && result[i] < result[j]
	})
	return result
}

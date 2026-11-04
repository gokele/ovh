package catalog

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ovh-buy/server/internal/numconv"
)

// Products are keyed by name; a plan's product is normally a foreign key, not an object.
func productCPU(product map[string]interface{}) string {
	blobs, _ := product["blobs"].(map[string]interface{})
	technical, _ := blobs["technical"].(map[string]interface{})
	server, _ := technical["server"].(map[string]interface{})
	switch cpu := server["cpu"].(type) {
	case map[string]interface{}:
		model := strings.TrimSpace(getString(cpu, "model", ""))
		if model != "" && !strings.EqualFold(model, "N/A") {
			brand := strings.TrimSpace(getString(cpu, "brand", ""))
			if brand != "" && !strings.EqualFold(model, brand) && !strings.HasPrefix(strings.ToLower(model), strings.ToLower(brand)+" ") {
				model = brand + " " + model
			}
			if n, ok := numconv.ToInt64(cpu["number"]); ok && n > 1 {
				model = fmt.Sprintf("%dx %s", n, model)
			}
			return model
		}
	case string:
		if cpu = strings.TrimSpace(cpu); cpu != "" && !strings.EqualFold(cpu, "N/A") {
			return cpu
		}
	}
	if cpu := cpuFromText(getString(product, "description", "")); cpu != "" {
		return cpu
	}
	return "N/A"
}

var cpuTextStart = regexp.MustCompile(`(?i)\b(?:(?:dual|[2-9]\d*\s*x)\s+)?(?:intel|amd|ampere|arm|xeon|epyc|ryzen)\b|\bi[3579]-\d`)

func cpuFromText(text string) string {
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == '|' || r == ',' || r == ';' || r == '\n' }) {
		if loc := cpuTextStart.FindStringIndex(part); loc != nil {
			return strings.TrimSpace(part[loc[0]:])
		}
	}
	return ""
}

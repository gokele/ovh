package ovh

import (
	"context"
	"net/http"
	"net/url"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// GetAvailabilities 读取实时库存。官方 /1.0/dedicated/server.json 三区都把
// GET /dedicated/server/datacenter/availabilities 标为 noAuthentication,
// 不签名可省掉时钟校准与 SHA1,也不占用账户的鉴权调用额度。
func GetAvailabilities(ctx context.Context, client *ovhsdk.Client, planCode string) ([]map[string]interface{}, error) {
	q := url.Values{}
	q.Set("planCode", planCode)
	var variants []map[string]interface{}
	err := client.CallAPIWithContext(ctx, http.MethodGet, "/dedicated/server/datacenter/availabilities?"+q.Encode(), nil, &variants, false)
	return variants, err
}

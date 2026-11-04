package orderterm

import (
	"context"
	"errors"
	"fmt"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

var ErrUnavailable = errors.New("OVH 不支持所选购买时长")

func Lookup(ctx context.Context, client *ovhsdk.Client, cartID, product, planCode, wanted string) (string, string, error) {
	definitions, err := Offers(ctx, client, cartID, product)
	if err != nil {
		return "", "", err
	}
	for _, def := range definitions {
		if def["planCode"] != planCode {
			continue
		}
		if d, mode, ok := Pick(def["prices"], wanted, ""); ok {
			return d, mode, nil
		}
	}
	return "", "", fmt.Errorf("%w: %s / %s；请修改任务的购买时长后重试", ErrUnavailable, planCode, wanted)
}

func Offers(ctx context.Context, client *ovhsdk.Client, cartID, product string) ([]map[string]interface{}, error) {
	var offers []map[string]interface{}
	var err error
	switch product {
	case "eco":
		err = client.GetWithContext(ctx, "/order/cart/"+cartID+"/eco", &offers)
	case "vps":
		err = client.GetWithContext(ctx, "/order/cart/"+cartID+"/vps", &offers)
	default:
		err = fmt.Errorf("invalid product type: %s", product)
	}
	return offers, err
}

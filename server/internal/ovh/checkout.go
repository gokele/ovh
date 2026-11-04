package ovh

import (
	"errors"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// CheckoutUncertain requires an explicit business rejection before retrying a checkout.
func CheckoutUncertain(err error) bool {
	var apiErr *ovhsdk.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code < 400 || apiErr.Code >= 500 || apiErr.Code == 408 || apiErr.Code == 429
	}
	return err != nil
}

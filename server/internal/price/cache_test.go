package price

import (
	"reflect"
	"testing"
)

func TestPriceCacheKeyDoesNotModifySelectedOptions(t *testing.T) {
	options := []string{"storage-b", "ram-a"}
	before := append([]string(nil), options...)
	first := priceCacheKey("account", "plan", "gra", options)
	if !reflect.DeepEqual(options, before) {
		t.Fatal("computing a cache key modified the subscription's options")
	}
	if second := priceCacheKey("account", "plan", "gra", []string{"ram-a", "storage-b"}); first != second {
		t.Fatal("equivalent option sets produced different keys")
	}
}

package ovh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// 库存接口官方免鉴权:不能签名,也不能先去 /auth/time 校准时钟。
func TestGetAvailabilitiesIsUnsigned(t *testing.T) {
	var timeCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/time" {
			timeCalls++
		}
		if r.Header.Get("X-Ovh-Signature") != "" || r.Header.Get("X-Ovh-Consumer") != "" || r.Header.Get("X-Ovh-Timestamp") != "" {
			t.Errorf("availability request was signed: %v", r.Header)
		}
		if r.URL.Query().Get("planCode") != "24ska01" {
			t.Errorf("planCode = %q", r.URL.Query().Get("planCode"))
		}
		_, _ = w.Write([]byte(`[{"fqn":"24ska01.ram-32g","datacenters":[{"datacenter":"gra","availability":"1H-low"}]}]`))
	}))
	defer srv.Close()
	client, err := ovhsdk.NewClient(srv.URL, "ak", "as", "ck")
	if err != nil {
		t.Fatal(err)
	}
	variants, err := GetAvailabilities(context.Background(), client, "24ska01")
	if err != nil || len(variants) != 1 || variants[0]["fqn"] != "24ska01.ram-32g" {
		t.Fatalf("variants=%v err=%v", variants, err)
	}
	if timeCalls != 0 {
		t.Fatal("unsigned call must not calibrate the clock")
	}
}

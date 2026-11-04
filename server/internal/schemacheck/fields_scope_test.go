package schemacheck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFieldScannerDistinguishesShadowedVariablesAndLocalWrites(t *testing.T) {
	dir := t.TempDir()
	const source = `package fixture
func read(client interface{}) {
 item := map[string]interface{}{}
 client.Get("/support/tickets/123", &item)
 _ = item["ticketId"]
 item["params"] = map[string]string{"detail":"local"}
 for _, item := range []map[string]interface{}{} { _ = item["error"] }
 details, _ := item["details"].(map[string]interface{})
 _ = details["id"]
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	fields := map[string]bool{}
	for _, chain := range scanFieldChains(t, dir) {
		fields[chain.field] = true
	}
	if !fields["ticketId"] || !fields["details.id"] {
		t.Fatal("real response reads were lost", fields)
	}
	if fields["params"] || fields["error"] {
		t.Fatal("local fields were mistaken for OVH response fields", fields)
	}
}

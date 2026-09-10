package collectors

import (
	"encoding/json"
	"testing"
)

func TestLimitParameter(t *testing.T) {
	args, err := limitParameter(json.RawMessage(`{"limit":25}`))
	if err != nil || len(args) != 1 || args[0].(int) != 25 {
		t.Fatalf("unexpected result: %#v %v", args, err)
	}
	if _, err := limitParameter(json.RawMessage(`{"limit":1001}`)); err == nil {
		t.Fatal("oversized limit was accepted")
	}
	if _, err := limitParameter(json.RawMessage(`{"limit":10,"sql":"DROP TABLE x"}`)); err == nil {
		t.Fatal("unknown parameter was accepted")
	}
}

func TestCatalogueDoesNotExposeArbitrarySQL(t *testing.T) {
	if Known("sql.execute") {
		t.Fatal("arbitrary SQL operation exists")
	}
	if !Known("collect.server_identity.v1") {
		t.Fatal("expected collector missing")
	}
}

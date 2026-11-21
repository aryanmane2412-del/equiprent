package bookings

import "testing"

func TestDecodeStored(t *testing.T) {
	var rules []CancellationRule
	for _, empty := range [][]byte{nil, []byte(""), []byte("  "), []byte("null")} {
		if err := decodeStored(empty, &rules, "rules"); err != nil {
			t.Errorf("empty/null %q must mean no data, got %v", empty, err)
		}
	}
	if err := decodeStored([]byte(`[{"bogus":`), &rules, "rules"); err == nil {
		t.Error("corrupt JSON must be reported, not treated as no rules")
	}
	if err := decodeStored([]byte(`[]`), &rules, "rules"); err != nil {
		t.Errorf("valid empty list: %v", err)
	}
}

package bookings

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// decodeStored decodes a JSON column into v. Empty and null values mean "no
// data" and leave v untouched; anything else that fails to parse is reported,
// so a corrupt cancellation policy or price is never silently treated as
// "no rules" (which could waive a fee or a refund limit).
func decodeStored(raw []byte, v any, what string) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("decode stored %s: %w", what, err)
	}
	return nil
}

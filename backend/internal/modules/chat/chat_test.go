package chat

import (
	"strings"
	"testing"
)

func TestPreview(t *testing.T) {
	s := func(v string) *string { return &v }
	if got := preview(s("  Please   start at\nthe north corner. "), false); got != "Please start at the north corner." {
		t.Fatalf("got %q", got)
	}
	if got := preview(nil, true); got != "📷 Photo" {
		t.Fatalf("photo = %q", got)
	}
	if got := preview(s("see this"), true); got != "📷 see this" {
		t.Fatalf("photo + text = %q", got)
	}
	long := preview(s(strings.Repeat("काम ", 60)), false)
	if n := len([]rune(long)); n != 118 || !strings.HasSuffix(long, "…") {
		t.Fatalf("truncated to %d runes: %q", n, long)
	}
}

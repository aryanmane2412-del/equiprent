package pdfdoc

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sample() *Doc {
	d := &Doc{Title: "Rental agreement ER-2611-AB12", Author: "EQUIPRENT", Created: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC),
		Header: "EQUIPRENT rental agreement", Footer: "ER-2611-AB12", Watermark: "DRAFT"}
	d.Heading("EQUIPMENT RENTAL AGREEMENT", 16)
	d.Field("Owner (lessor)", "Patil Earthmovers Pvt Ltd (GSTIN 27ABCDE1234F1Z5)")
	d.Field("Estimated total", "₹1,23,456.00 — incl. GST")
	d.Rule()
	for i := range 80 {
		d.Para(strconv.Itoa(i+1) + ". The customer uses the equipment only at the agreed site (Hinjewadi) and for the described work; breakdown hours are not billed.")
	}
	d.Small("Terms SHA-256: " + strings.Repeat("ab", 32))
	return d
}

func TestDocumentStructure(t *testing.T) {
	out := sample().Bytes()
	if !bytes.HasPrefix(out, []byte("%PDF-1.4")) || !bytes.HasSuffix(out, []byte("%%EOF\n")) {
		t.Fatal("missing PDF header or trailer")
	}
	// Every xref offset must point at the start of its object.
	m := regexp.MustCompile(`startxref\n(\d+)\n`).FindSubmatch(out)
	if m == nil {
		t.Fatal("no startxref")
	}
	xref, _ := strconv.Atoi(string(m[1]))
	if !bytes.HasPrefix(out[xref:], []byte("xref\n")) {
		t.Fatalf("startxref %d does not point at the xref table", xref)
	}
	rows := regexp.MustCompile(`(\d{10}) 00000 n `).FindAllSubmatch(out[xref:], -1)
	for i, r := range rows {
		off, _ := strconv.Atoi(string(r[1]))
		want := strconv.Itoa(i+1) + " 0 obj\n"
		if !bytes.HasPrefix(out[off:], []byte(want)) {
			t.Fatalf("object %d offset %d points at %q", i+1, off, out[off:off+12])
		}
	}
	if pages := bytes.Count(out, []byte("/Type /Page ")); pages < 2 {
		t.Fatalf("expected the long text to flow onto several pages, got %d", pages)
	}
	if !bytes.Contains(out, []byte("Page 1 of ")) || !bytes.Contains(out, []byte("(DRAFT)")) {
		t.Fatal("footer page numbers or watermark missing")
	}
	// Stream lengths match their content.
	for _, sm := range regexp.MustCompile(`(?s)<< /Length (\d+) >>\nstream\n(.*?)endstream`).FindAllSubmatch(out, -1) {
		n, _ := strconv.Atoi(string(sm[1]))
		if n != len(sm[2]) {
			t.Fatalf("stream length %d, content %d", n, len(sm[2]))
		}
	}
	// No active content.
	if regexp.MustCompile(`/(JavaScript|JS|Launch|OpenAction|AA)\b`).Match(out) {
		t.Fatal("generated PDF contains active content")
	}
}

func TestDeterministic(t *testing.T) {
	if !bytes.Equal(sample().Bytes(), sample().Bytes()) {
		t.Fatal("same content gave different bytes")
	}
}

func TestEncode(t *testing.T) {
	cases := map[string]string{
		"₹500":      "Rs.500",
		"a — b":     "a \x97 b",
		"café":      "caf\xe9",
		"राम":       "???",
		"(x) \\ y":  "(x) \\ y",
		"tab\there": "tab here",
	}
	for in, want := range cases {
		if got := encode(in); got != want {
			t.Errorf("encode(%q) = %q, want %q", in, got, want)
		}
	}
	if got := esc("a (b) \\ c"); got != `a \(b\) \\ c` {
		t.Errorf("esc = %q", got)
	}
}

func TestWrap(t *testing.T) {
	lines := wrap(encode(strings.Repeat("excavator ", 40)), regular, 10, 200)
	if len(lines) < 5 {
		t.Fatalf("lines = %d", len(lines))
	}
	for _, l := range lines {
		if width(l, regular, 10) > 200 {
			t.Fatalf("line %q is %.1f wide", l, width(l, regular, 10))
		}
	}
	long := wrap(strings.Repeat("f", 400), regular, 10, 100)
	if len(long) < 2 {
		t.Fatal("an unbreakable token was not split")
	}
	if width("Hello", bold, 10) <= width("Hello", regular, 10) {
		t.Fatal("bold should be wider")
	}
}

func TestListMarker(t *testing.T) {
	for in, want := range map[string]string{"1. a": "1.", "12. b": "12.", "- c": "-", "x. d": "", " 1. e": "", "1.5 m": ""} {
		if got := listMarker(in); got != want {
			t.Errorf("listMarker(%q) = %q, want %q", in, got, want)
		}
	}
}

// Package pdfdoc writes simple, text-only A4 PDF documents (headings,
// paragraphs, label/value rows, rules) with the standard Helvetica fonts.
// It exists for server-generated documents such as rental agreements, so it
// needs no external dependency and its output is deterministic: the same
// content and metadata always give the same bytes (and the same SHA-256).
//
// Text is encoded as WinAnsi (Latin-1 plus typographic punctuation). The
// rupee sign becomes "Rs." and characters outside WinAnsi become "?"; the
// standard fonts carry no Indic glyphs.
package pdfdoc

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Page geometry (points; A4).
const (
	pageW   = 595.0
	pageH   = 842.0
	marginX = 56.0
	marginT = 64.0
	marginB = 64.0
	textW   = pageW - 2*marginX
)

type font int

const (
	regular font = iota
	bold
)

type line struct {
	font  font
	size  float64
	x     float64
	text  string // already WinAnsi-encoded
	gap   float64
	rule  bool
	color float64 // grey level, 0 = black
	label string  // bold label at the left margin (Field rows)
}

// Doc accumulates content; Bytes lays it out and renders the PDF.
type Doc struct {
	Title     string
	Author    string
	Subject   string
	Created   time.Time // fixed by the caller for deterministic output
	Header    string    // small text at the top of every page
	Footer    string    // small text at the bottom of every page (page numbers are added)
	Watermark string    // large grey diagonal text on every page (e.g. DRAFT)

	lines []line
}

// Heading adds a bold heading.
func (d *Doc) Heading(text string, size float64) {
	d.add(bold, size, marginX, text, size*0.6, 0)
}

// Para adds a wrapped paragraph.
func (d *Doc) Para(text string) { d.ParaStyled(text, 10, false, 0) }

// Small adds a wrapped paragraph in small grey type.
func (d *Doc) Small(text string) { d.ParaStyled(text, 8, false, 0.35) }

// ParaStyled adds a wrapped paragraph.
func (d *Doc) ParaStyled(text string, size float64, isBold bool, grey float64) {
	f := regular
	if isBold {
		f = bold
	}
	for i, para := range strings.Split(text, "\n") {
		if i > 0 && strings.TrimSpace(para) == "" {
			d.Space(size * 0.4)
			continue
		}
		indent := 0.0
		body := para
		// "1. text" and "- text" items hang under their marker.
		if m := listMarker(para); m != "" {
			indent = width(encode(m+" "), f, size)
			body = strings.TrimSpace(para[len(m):])
			first := true
			for _, l := range wrap(encode(body), f, size, textW-indent) {
				if first {
					d.add(f, size, marginX, encode(m+" ")+l, 2, grey)
					first = false
					continue
				}
				d.add(f, size, marginX+indent, l, 2, grey)
			}
			continue
		}
		for _, l := range wrap(encode(body), f, size, textW) {
			d.add(f, size, marginX, l, 2, grey)
		}
	}
}

// Field adds a "Label: value" row with the value wrapped in a second column.
func (d *Doc) Field(label, value string) {
	const labelW = 150.0
	vals := wrap(encode(value), regular, 10, textW-labelW)
	if len(vals) == 0 {
		vals = []string{"-"}
	}
	for i, v := range vals {
		l := line{font: regular, size: 10, x: marginX + labelW, text: v, gap: 2}
		if i == 0 {
			l.label = encode(label) // drawn on the same baseline
		}
		d.lines = append(d.lines, l)
	}
}

// Rule adds a horizontal line.
func (d *Doc) Rule() { d.lines = append(d.lines, line{rule: true, size: 1, gap: 8}) }

// Space adds vertical space.
func (d *Doc) Space(pt float64) { d.lines = append(d.lines, line{size: 0, gap: pt}) }

func (d *Doc) add(f font, size, x float64, text string, gap, grey float64) {
	d.lines = append(d.lines, line{font: f, size: size, x: x, text: text, gap: gap, color: grey})
}

func listMarker(s string) string {
	t := strings.TrimLeft(s, " ")
	if len(t) != len(s) {
		return ""
	}
	if strings.HasPrefix(s, "- ") {
		return "-"
	}
	i := 0
	for i < len(s) && i < 3 && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s)-1 && s[i] == '.' && s[i+1] == ' ' {
		return s[:i+1]
	}
	return ""
}

// ---- layout and rendering ----------------------------------------------------------------

type placed struct {
	line
	y float64
}

func (d *Doc) layout() [][]placed {
	var pages [][]placed
	var cur []placed
	y := pageH - marginT
	for _, l := range d.lines {
		if l.text == "" && !l.rule {
			y -= l.gap
			continue
		}
		if y-l.size*1.25 < marginB {
			pages = append(pages, cur)
			cur = nil
			y = pageH - marginT
		}
		y -= l.size * 1.25
		cur = append(cur, placed{line: l, y: y})
		y -= l.gap
	}
	pages = append(pages, cur)
	return pages
}

// Bytes renders the document.
func (d *Doc) Bytes() []byte {
	pages := d.layout()
	n := len(pages)

	var objs [][]byte // object i+1
	addObj := func(b []byte) int { objs = append(objs, b); return len(objs) }

	catalog := addObj(nil) // filled later
	pagesObj := addObj(nil)
	f1 := addObj([]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"))
	f2 := addObj([]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>"))
	gs := addObj([]byte("<< /Type /ExtGState /ca 0.08 /CA 0.08 >>"))

	var kids []string
	for i, pg := range pages {
		var c bytes.Buffer
		if d.Watermark != "" {
			fmt.Fprintf(&c, "q /GS1 gs BT /F2 72 Tf 0.6 g 0.7071 0.7071 -0.7071 0.7071 150 250 Tm (%s) Tj ET Q\n", esc(encode(d.Watermark)))
		}
		if d.Header != "" {
			fmt.Fprintf(&c, "BT /F1 8 Tf 0.4 g %s %s Td (%s) Tj ET\n", num(marginX), num(pageH-36), esc(encode(d.Header)))
		}
		for _, p := range pg {
			if p.rule {
				fmt.Fprintf(&c, "0.75 G 0.5 w %s %s m %s %s l S\n", num(marginX), num(p.y+4), num(pageW-marginX), num(p.y+4))
				continue
			}
			fname := "/F1"
			if p.font == bold {
				fname = "/F2"
			}
			if p.label != "" {
				fmt.Fprintf(&c, "BT /F2 %s Tf 0 g %s %s Td (%s) Tj ET\n", num(p.size), num(marginX), num(p.y), esc(p.label))
			}
			fmt.Fprintf(&c, "BT %s %s Tf %s g %s %s Td (%s) Tj ET\n", fname, num(p.size), num(p.color), num(p.x), num(p.y), esc(p.text))
		}
		footer := fmt.Sprintf("Page %d of %d", i+1, n)
		if d.Footer != "" {
			footer = d.Footer + "   |   " + footer
		}
		fmt.Fprintf(&c, "BT /F1 8 Tf 0.4 g %s %s Td (%s) Tj ET\n", num(marginX), num(36), esc(encode(footer)))

		content := addObj(fmt.Appendf(nil, "<< /Length %d >>\nstream\n%sendstream", c.Len(), c.String()))
		page := addObj(fmt.Appendf(nil, "<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Resources << /Font << /F1 %d 0 R /F2 %d 0 R >> /ExtGState << /GS1 %d 0 R >> >> /Contents %d 0 R >>",
			pagesObj, num(pageW), num(pageH), f1, f2, gs, content))
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
	}
	objs[pagesObj-1] = fmt.Appendf(nil, "<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n)
	objs[catalog-1] = fmt.Appendf(nil, "<< /Type /Catalog /Pages %d 0 R >>", pagesObj)
	created := d.Created.UTC().Format("20060102150405") + "Z"
	info := addObj(fmt.Appendf(nil, "<< /Title (%s) /Author (%s) /Subject (%s) /Producer (EQUIPRENT) /CreationDate (D:%s) >>",
		esc(encode(d.Title)), esc(encode(d.Author)), esc(encode(d.Subject)), created))

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, catalog, info, xref)
	return out.Bytes()
}

func num(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// esc escapes a WinAnsi string for a PDF literal string.
func esc(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`, "\r", "", "\n", " ")
	return r.Replace(s)
}

// ---- text encoding and metrics -----------------------------------------------------------

var special = map[rune]string{
	'₹': "Rs.", '–': "\x96", '—': "\x97", '‘': "\x91", '’': "\x92", '“': "\x93", '”': "\x94",
	'•': "\x95", '…': "\x85", '€': "\x80", '™': "\x99", '\t': " ", '→': "->", '≥': ">=", '≤': "<=",
}

// encode converts UTF-8 to single-byte WinAnsi.
func encode(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == utf8.RuneError:
			b.WriteByte('?')
		case r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20: // other control characters are dropped
		case r < 0x7f:
			b.WriteRune(r)
		case special[r] != "":
			b.WriteString(special[r])
		case r >= 0xa0 && r <= 0xff:
			b.WriteByte(byte(r))
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// Helvetica and Helvetica-Bold advance widths (1/1000 em) for 0x20..0x7E,
// from the Adobe core font metrics.
var helv = [95]uint16{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
}

var helvBold = [95]uint16{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
	975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
	333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
	611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584,
}

func width(s string, f font, size float64) float64 {
	table := &helv
	if f == bold {
		table = &helvBold
	}
	total := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 0x20 && c <= 0x7e:
			total += int(table[c-0x20])
		case c == 0x95:
			total += 350
		default:
			total += 556
		}
	}
	return float64(total) * size / 1000
}

// wrap breaks an encoded string into lines no wider than max.
func wrap(s string, f font, size, max float64) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var out []string
	cur := ""
	for _, w := range words {
		for width(w, f, size) > max { // split very long tokens (hashes, URLs)
			cut := len(w)
			for cut > 1 && width(w[:cut], f, size) > max {
				cut--
			}
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			out = append(out, w[:cut])
			w = w[cut:]
		}
		next := w
		if cur != "" {
			next = cur + " " + w
		}
		if width(next, f, size) <= max {
			cur = next
			continue
		}
		out = append(out, cur)
		cur = w
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

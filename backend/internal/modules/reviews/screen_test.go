package reviews

import (
	"slices"
	"testing"

	"github.com/equiprent/backend/internal/db"
)

func TestScreen(t *testing.T) {
	cases := map[string][]string{
		"Great machine, operator was on time.":                  {},
		"Bucket 1.2 m3, 20 t class, 3 days of work":             {},
		"Call me on 98765 43210 for a cheaper deal":             {"contact_details"},
		"Mail ravi.patil@example.com directly":                  {"contact_details"},
		"Book at www.cheapdiggers.in instead":                   {"link"},
		"See https://example.org/deal":                          {"link"},
		"Total chutiya service, never again":                    {"abusive_language"},
		"What the FUCK, +91 9876543210":                         {"contact_details", "abusive_language"},
		"Reference ER-2611-AB12, invoice ER/2627/00012 settled": {},
	}
	for in, want := range cases {
		if got := Screen(in); !slices.Equal(got, want) {
			t.Errorf("Screen(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSummary(t *testing.T) {
	s := summarise([]db.ReviewSummaryRow{{Rating: 5, Cnt: 3}, {Rating: 4, Cnt: 1}, {Rating: 1, Cnt: 1}})
	if s.Count != 5 || s.Average != 4.0 || s.Histogram["5"] != 3 || s.Histogram["2"] != 0 {
		t.Fatalf("summary = %+v", s)
	}
	s = summarise(nil)
	if s.Count != 0 || s.Average != 0 || len(s.Histogram) != 5 {
		t.Fatalf("empty summary = %+v", s)
	}
	if got := summarise([]db.ReviewSummaryRow{{Rating: 5, Cnt: 2}, {Rating: 4, Cnt: 1}}).Average; got != 4.67 {
		t.Fatalf("average rounding = %v", got)
	}
	if stars(4) != "★★★★☆" {
		t.Fatal(stars(4))
	}
}

package bookings

import (
	"encoding/json"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

// The worked example from docs/API_DESIGN.md §3.
func TestQuoteMatchesDocumentedExample(t *testing.T) {
	start := time.Date(2026, 11, 20, 2, 30, 0, 0, time.UTC)
	p := Quote(PriceInputs{
		Unit: "shift", RatePaise: 880000, MinUnits: 1, WithOperator: true,
		OperatorBataPerDayPaise: 50000, MobFixedPaise: 450000,
		Start: start, End: start.Add(3*24*time.Hour - 2*time.Hour), // spans 3 days
		CustomerFeeBps: 200, GSTBps: 1800, AdvanceBps: 5000, DepositPaise: 1000000,
	})
	want := Price{
		RentalPaise: 2640000, MobilisationPaise: 450000, OperatorBataPaise: 150000,
		PlatformFeePaise: 52800, GSTPaise: 592704, EstimatedTotalPaise: 3885504,
		DepositPaise: 1000000, AdvanceDuePaise: 1942752, DueOnConfirmationPaise: 2942752,
	}
	if p.RentalPaise != want.RentalPaise || p.MobilisationPaise != want.MobilisationPaise ||
		p.OperatorBataPaise != want.OperatorBataPaise || p.PlatformFeePaise != want.PlatformFeePaise ||
		p.GSTPaise != want.GSTPaise || p.EstimatedTotalPaise != want.EstimatedTotalPaise ||
		p.AdvanceDuePaise != want.AdvanceDuePaise || p.DueOnConfirmationPaise != want.DueOnConfirmationPaise {
		t.Fatalf("quote = %+v\nwant  %+v", p, want)
	}
	if p.Units != 3 || p.Days != 3 {
		t.Errorf("units=%d days=%d", p.Units, p.Days)
	}
}

func TestQuoteUnitsAndMinimums(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		unit      string
		dur       time.Duration
		minUnits  int32
		wantUnits int64
	}{
		{"partial hour rounds up", "hour", 90 * time.Minute, 1, 2},
		{"min units applies", "hour", time.Hour, 4, 4},
		{"day", "day", 25 * time.Hour, 1, 2},
		{"week", "week", 8 * 24 * time.Hour, 1, 2},
		{"month", "month", 30 * 24 * time.Hour, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Quote(PriceInputs{Unit: tc.unit, RatePaise: 1000, MinUnits: tc.minUnits, Start: start, End: start.Add(tc.dur)})
			if p.Units != tc.wantUnits || p.RentalPaise != 1000*tc.wantUnits {
				t.Fatalf("units=%d rental=%d", p.Units, p.RentalPaise)
			}
		})
	}
}

func TestQuoteExtraHoursAndMobilisationDistance(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := Quote(PriceInputs{
		Unit: "shift", RatePaise: 800000, MinUnits: 1, IncludedHours: ptr(8.0), ExtraHourRatePaise: ptr(int64(110000)),
		Start: start, End: start.Add(48 * time.Hour), ExpectedHours: 19.5, // 2 shifts × 8 h included → 3.5 → 4 extra h
		MobFixedPaise: 100000, MobPerKmPaise: 5000, MobFreeKm: 10, DistanceKm: 24.6, // 25 km - 10 free = 15 km
	})
	if p.ExtraHours != 4 || p.RentalPaise != 2*800000+4*110000 {
		t.Errorf("extra hours=%v rental=%d", p.ExtraHours, p.RentalPaise)
	}
	if p.MobilisationKm != 15 || p.MobilisationPaise != 100000+15*5000 {
		t.Errorf("mob km=%v paise=%d", p.MobilisationKm, p.MobilisationPaise)
	}
	if p.OperatorBataPaise != 0 {
		t.Error("dry hire must not charge bata")
	}
}

func TestCancellationFee(t *testing.T) {
	rules := json.RawMessage(`[{"hours_before": 0, "fee_bps": 2500}, {"hours_before": 24, "fee_bps": 0}]`)
	start := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		now  time.Time
		want int64
	}{
		{"two days ahead is free", start.Add(-48 * time.Hour), 0},
		{"exactly 24h ahead is free", start.Add(-24 * time.Hour), 0},
		{"same day pays 25%", start.Add(-2 * time.Hour), 25000},
		{"after start pays 25%", start.Add(time.Hour), 25000},
	}
	for _, tc := range cases {
		if got := CancellationFee(rules, 100000, tc.now, start); got != tc.want {
			t.Errorf("%s: fee=%d want %d", tc.name, got, tc.want)
		}
	}
	if CancellationFee(nil, 100000, start, start) != 0 {
		t.Error("no policy should be free")
	}
}

func TestBpsRoundsHalfUp(t *testing.T) {
	if got := bps(52800, 1800); got != 9504 {
		t.Fatalf("bps = %d", got)
	}
	if got := bps(1, 5000); got != 1 { // 0.5 paise rounds up
		t.Fatalf("half-up = %d", got)
	}
}

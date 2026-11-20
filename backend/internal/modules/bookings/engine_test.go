package bookings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
)

var t0 = time.Date(2027, 3, 10, 2, 30, 0, 0, time.UTC) // 08:00 IST

func gst18() []TaxRule {
	return []TaxRule{{Code: "gst", Name: "GST", RateBps: 1800, AppliesTo: AllComponents}}
}

// excavator: hourly ₹1,500, daily ₹12,000, weekly ₹70,000, monthly ₹2,40,000.
func excavator() Terms {
	return Terms{
		Plans: []PlanRate{
			{Unit: "hour", RatePaise: 150_000, MinUnits: 4},
			{Unit: "day", RatePaise: 1_200_000, MinUnits: 1, IncludedHours: ptr(8.0), ExtraHourRatePaise: ptr[int64](180_000), OperatorBataPerDayPaise: 60_000},
			{Unit: "week", RatePaise: 7_000_000, MinUnits: 1},
			{Unit: "month", RatePaise: 24_000_000, MinUnits: 1},
		},
		DeliveryFixedPaise: 300_000, DeliveryPerKmPaise: 6_000, FreeKm: 10, PickupFixedPaise: 200_000, PickupPerKmPaise: 4_000,
		FuelPolicy: "customer", LateGraceMinutes: 60, ServiceRadiusKm: 60, Timezone: "Asia/Kolkata",
	}
}

func com() Commercials {
	return Commercials{CustomerFeeBps: 200, AdvanceBps: 3000, DefaultDepositPaise: 2_500_000, Taxes: gst18()}
}

func opts(unit string, h float64) Options {
	return Options{Unit: unit, Start: t0, End: t0.Add(time.Duration(h * float64(time.Hour)))}
}

func mustEstimate(t *testing.T, tr Terms, o Options, c Commercials) Price {
	t.Helper()
	p, err := Estimate(tr, o, c)
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, p)
	return p
}

// checkInvariants: integer paise only, no negative charge, the parts add
// up, and the display lines match the totals.
func checkInvariants(t *testing.T, p Price) {
	t.Helper()
	var lines int64
	for _, l := range p.Lines {
		if l.Paise != l.Count*l.RatePaise || l.Count <= 0 {
			t.Fatalf("bad line %+v", l)
		}
		lines += l.Paise
	}
	for name, v := range map[string]int64{"rental": p.RentalPaise, "delivery": p.DeliveryPaise, "pickup": p.PickupPaise, "operator": p.OperatorBataPaise,
		"fuel": p.FuelPaise, "fee": p.PlatformFeePaise, "tax": p.GSTPaise, "deposit": p.DepositPaise} {
		if v < 0 {
			t.Fatalf("%s is negative: %d", name, v)
		}
	}
	if p.RentalPaise != lines+p.ExtraHoursPaise {
		t.Fatalf("rental %d != lines %d + extra %d", p.RentalPaise, lines, p.ExtraHoursPaise)
	}
	sub := p.RentalPaise + p.DeliveryPaise + p.PickupPaise + p.OperatorBataPaise + p.FuelPaise + p.PlatformFeePaise
	if p.SubtotalPaise != sub || p.EstimatedTotalPaise != sub+p.GSTPaise || p.MobilisationPaise != p.DeliveryPaise+p.PickupPaise {
		t.Fatalf("totals do not add up: %+v", p)
	}
	var shown, taxes int64
	for _, c := range p.Charges {
		shown += c.Paise
	}
	for _, x := range p.Taxes {
		taxes += x.Paise
	}
	if shown != p.EstimatedTotalPaise || taxes != p.GSTPaise {
		t.Fatalf("charge lines %d / tax lines %d do not match totals %d / %d", shown, taxes, p.EstimatedTotalPaise, p.GSTPaise)
	}
	if p.Kind != "estimate" || p.Currency != "INR" || p.Terms == nil {
		t.Fatalf("kind/currency/terms: %+v", p)
	}
}

func lines(p Price) string {
	var s []string
	for _, l := range p.Lines {
		s = append(s, fmt.Sprintf("%d %s", l.Count, l.Unit))
	}
	return strings.Join(s, " + ")
}

func TestRentalOptionsPerUnit(t *testing.T) {
	cases := []struct {
		unit  string
		hours float64
		want  string
		paise int64
	}{
		{"hour", 1, "4 hour", 600_000}, // minimum 4 hours
		{"hour", 6.5, "7 hour", 1_050_000},
		{"day", 24, "1 day", 1_200_000},
		{"day", 25, "2 day", 2_400_000}, // a started day is a day
		{"day", 72, "3 day", 3_600_000},
		{"week", 24 * 7, "1 week", 7_000_000},
		{"week", 24 * 8, "2 week", 14_000_000},
		{"month", 24 * 30, "1 month", 24_000_000},
		{"month", 24 * 45, "2 month", 48_000_000},
	}
	for _, c := range cases {
		p := mustEstimate(t, excavator(), opts(c.unit, c.hours), com())
		if lines(p) != c.want || p.RentalPaise != c.paise {
			t.Errorf("%s for %vh = %s (%d paise), want %s (%d)", c.unit, c.hours, lines(p), p.RentalPaise, c.want, c.paise)
		}
	}
	if _, err := Estimate(excavator(), opts("shift", 8), com()); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("no shift plan: %v", err)
	}
}

func TestCustomDurationPicksCheapestMix(t *testing.T) {
	cases := []struct {
		hours float64
		want  string
		paise int64
	}{
		{5, "5 hour", 750_000},                   // under a day's price
		{9, "1 day", 1_200_000},                  // 9 h × ₹1,500 = ₹13,500 > a day
		{26, "1 day + 4 hour", 1_800_000},        // 2 h left over, but hourly minimum is 4
		{24 * 5, "5 day", 6_000_000},             // ₹60,000 < a week
		{24 * 6, "1 week", 7_000_000},            // ₹72,000 by day > ₹70,000 week
		{24 * 9, "1 week + 2 day", 9_400_000},    // vs 9 days ₹1,08,000 or 2 weeks
		{24 * 12, "1 week + 5 day", 13_000_000},  // cheaper than 2 weeks (₹1,40,000)
		{24 * 26, "1 month", 24_000_000},         // 3 weeks + 5 days ₹2,70,000
		{24 * 33, "1 month + 3 day", 27_600_000}, // month is 30 days
	}
	for _, c := range cases {
		p := mustEstimate(t, excavator(), opts(UnitAuto, c.hours), com())
		if lines(p) != c.want || p.RentalPaise != c.paise || p.Unit != UnitAuto {
			t.Errorf("auto %vh = %s (%d), want %s (%d)", c.hours, lines(p), p.RentalPaise, c.want, c.paise)
		}
	}
	// Only a daily plan: whole days.
	daily := excavator()
	daily.Plans = daily.Plans[1:2]
	if p := mustEstimate(t, daily, opts(UnitAuto, 30), com()); lines(p) != "2 day" {
		t.Fatalf("daily only = %s", lines(p))
	}
}

// Property: for any duration, auto is never dearer than any single unit.
func TestCustomDurationNeverDearerThanSingleUnit(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 3000; i++ {
		h := 1 + r.Float64()*(180*24-1)
		auto := mustEstimate(t, excavator(), opts(UnitAuto, h), com())
		for _, u := range []string{"hour", "day", "week", "month"} {
			single := mustEstimate(t, excavator(), opts(u, h), com())
			if auto.RentalPaise > single.RentalPaise {
				t.Fatalf("%.2fh: auto %s ₹%d dearer than %s ₹%d", h, lines(auto), auto.RentalPaise, u, single.RentalPaise)
			}
		}
		var covered float64
		for _, l := range auto.Lines {
			covered += float64(l.Count) * unitHours[l.Unit]
		}
		if covered+1e-6 < h {
			t.Fatalf("%.2fh covered only %.2fh by %s", h, covered, lines(auto))
		}
	}
}

func TestChargesBreakdown(t *testing.T) {
	o := opts("day", 72)
	o.WithOperator, o.Delivery, o.Pickup, o.DistanceKm, o.ExpectedHours = true, true, true, 25.4, 30
	p := mustEstimate(t, excavator(), o, com())

	// Rental 3 days ₹36,000; included 24 h, expected 30 h → 6 extra × ₹1,800.
	if p.RentalPaise != 3_600_000+6*180_000 || p.ExtraHours != 6 {
		t.Fatalf("rental/extra = %d / %v", p.RentalPaise, p.ExtraHours)
	}
	// 25.4 km rounds to 25; 10 free → 15 km.
	if p.MobilisationKm != 15 || p.DeliveryPaise != 300_000+15*6_000 || p.PickupPaise != 200_000+15*4_000 {
		t.Fatalf("transport = %v km, %d + %d", p.MobilisationKm, p.DeliveryPaise, p.PickupPaise)
	}
	if p.OperatorBataPaise != 3*60_000 {
		t.Fatalf("operator = %d", p.OperatorBataPaise)
	}
	fee := bps(p.RentalPaise, 200)
	supply := p.RentalPaise + p.DeliveryPaise + p.PickupPaise + p.OperatorBataPaise
	if p.PlatformFeePaise != fee || p.GSTPaise != bps(supply, 1800)+bps(fee, 1800) {
		t.Fatalf("fee/gst = %d / %d", p.PlatformFeePaise, p.GSTPaise)
	}
	if p.DepositPaise != 2_500_000 || p.AdvanceDuePaise != bps(p.EstimatedTotalPaise, 3000) ||
		p.DueOnConfirmationPaise != p.AdvanceDuePaise+p.DepositPaise {
		t.Fatalf("deposit/advance = %+v", p)
	}
	if p.Period != nil {
		t.Fatal("the pure engine leaves the period view to the service")
	}

	// Without operator, delivery or pickup those charges disappear.
	o.WithOperator, o.Delivery, o.Pickup = false, false, false
	q := mustEstimate(t, excavator(), o, com())
	if q.OperatorBataPaise != 0 || q.DeliveryPaise != 0 || q.PickupPaise != 0 || q.EstimatedTotalPaise >= p.EstimatedTotalPaise {
		t.Fatalf("options off = %+v", q)
	}
	// Collecting yourself works beyond the delivery radius.
	o.DistanceKm = 500
	mustEstimate(t, excavator(), o, com())
	o.Delivery = true
	if _, err := Estimate(excavator(), o, com()); !errors.Is(err, ErrOutsideArea) {
		t.Fatalf("delivery beyond radius: %v", err)
	}
}

func TestFuelPolicies(t *testing.T) {
	tr := excavator()
	tr.FuelPolicy, tr.FuelRatePerHourPaise = "owner", 90_000 // ₹900 per working hour
	o := opts("day", 48)
	o.ExpectedHours = 20
	if p := mustEstimate(t, tr, o, com()); p.FuelPaise != 20*90_000 || p.FuelHours != 20 {
		t.Fatalf("fuel on expected hours = %d", p.FuelPaise)
	}
	o.ExpectedHours = 0 // default 8 h a day
	p := mustEstimate(t, tr, o, com())
	if p.FuelPaise != 16*90_000 || !strings.Contains(strings.Join(p.Notes, " "), "8 working hours") {
		t.Fatalf("default fuel = %d %v", p.FuelPaise, p.Notes)
	}
	tr.FuelPolicy = "customer"
	if p := mustEstimate(t, tr, o, com()); p.FuelPaise != 0 || !strings.Contains(strings.Join(p.Notes, " "), "arranged by you") {
		t.Fatalf("customer fuel = %d", p.FuelPaise)
	}
}

func TestConfigurableTaxes(t *testing.T) {
	c := com()
	c.Taxes = []TaxRule{
		{Code: "gst", Name: "GST", RateBps: 1800, AppliesTo: []string{CompRental, CompDelivery, CompPickup, CompOperator, CompPlatformFee}},
		{Code: "cess", Name: "Infra cess", RateBps: 100, AppliesTo: []string{CompRental}},
		{Code: "fuel_vat", Name: "Fuel VAT", RateBps: 500, AppliesTo: []string{CompFuel}}, // no fuel: no line
	}
	o := opts("day", 24)
	o.Delivery, o.DistanceKm = true, 0
	p := mustEstimate(t, excavator(), o, c)
	if len(p.Taxes) != 2 || p.Taxes[0].Code != "gst" || p.Taxes[1].Code != "cess" {
		t.Fatalf("tax lines = %+v", p.Taxes)
	}
	if p.Taxes[1].Paise != bps(1_200_000, 100) || p.Taxes[1].TaxablePaise != 1_200_000 {
		t.Fatalf("cess = %+v", p.Taxes[1])
	}
	gstBase := 1_200_000 + int64(300_000) // rental + delivery (no pickup chosen)
	if p.Taxes[0].Paise != bps(gstBase, 1800)+bps(p.PlatformFeePaise, 1800) {
		t.Fatalf("gst = %+v", p.Taxes[0])
	}
	// No taxes configured: none charged.
	c.Taxes = nil
	if p := mustEstimate(t, excavator(), o, c); p.GSTPaise != 0 || len(p.Taxes) != 0 {
		t.Fatalf("untaxed = %+v", p.Taxes)
	}
}

func TestRoundingIsHalfUpPerBucket(t *testing.T) {
	if bps(1, 5000) != 1 || bps(1, 4999) != 0 || bps(333, 1800) != 60 || bps(0, 1800) != 0 {
		t.Fatal("bps rounding")
	}
	tr := excavator()
	tr.Plans = []PlanRate{{Unit: "hour", RatePaise: 333, MinUnits: 1}}
	p := mustEstimate(t, tr, opts("hour", 1), Commercials{CustomerFeeBps: 250, Taxes: gst18()})
	if p.PlatformFeePaise != 8 || p.GSTPaise != bps(333, 1800)+bps(8, 1800) {
		t.Fatalf("rounding = fee %d gst %d", p.PlatformFeePaise, p.GSTPaise)
	}
}

func TestOwnerRulesAndDeposit(t *testing.T) {
	tr := excavator()
	tr.MinRentalHours, tr.MaxRentalDays = 24, 30
	if _, err := Estimate(tr, opts("hour", 8), com()); !errors.Is(err, ErrTooShort) {
		t.Fatalf("min: %v", err)
	}
	if _, err := Estimate(tr, opts("day", 31*24), com()); !errors.Is(err, ErrTooLong) {
		t.Fatalf("max: %v", err)
	}
	mustEstimate(t, tr, opts("day", 24), com())
	mustEstimate(t, tr, opts("day", 30*24), com())
	tr.DepositPaise = 10_000_000
	if p := mustEstimate(t, tr, opts("day", 24), com()); p.DepositPaise != 10_000_000 {
		t.Fatalf("owner deposit = %d", p.DepositPaise)
	}
}

func TestLateRate(t *testing.T) {
	tr := excavator()
	if lateRate(tr) != 150_000 {
		t.Fatalf("hourly plan rate = %d", lateRate(tr))
	}
	tr.Plans = tr.Plans[1:2]
	if lateRate(tr) != 150_000 { // ₹12,000 / 8
		t.Fatalf("1/8 day = %d", lateRate(tr))
	}
	tr.LateFeePerHourPaise = 250_000
	if lateRate(tr) != 250_000 {
		t.Fatal("owner late fee")
	}
}

func TestFinalChargesFromActuals(t *testing.T) {
	o := opts("day", 72)
	o.WithOperator, o.Delivery, o.Pickup, o.DistanceKm, o.ExpectedHours = true, true, true, 20, 24
	tr := excavator()
	tr.FuelPolicy, tr.FuelRatePerHourPaise = "owner", 90_000
	est := mustEstimate(t, tr, o, com())
	start, end := o.Start, o.End

	// Exactly as booked: final equals the estimate.
	same, err := Finalize(est, start, end, Actuals{ActualStart: start, ActualEnd: end, OperatingHours: 24})
	if err != nil || same.FinalTotalPaise != est.EstimatedTotalPaise || same.DifferencePaise != 0 || same.Kind != "final" {
		t.Fatalf("on time: %+v %v", same, err)
	}

	// Within the 60-minute grace: no late fee.
	grace, _ := Finalize(est, start, end, Actuals{ActualStart: start, ActualEnd: end.Add(59 * time.Minute), OperatingHours: 24})
	if grace.LateHours != 0 || grace.FinalTotalPaise != est.EstimatedTotalPaise {
		t.Fatalf("grace: %+v", grace)
	}

	// 3 h 10 min late, 31 hours worked, ₹50,000 damage.
	late, err := Finalize(est, start, end, Actuals{ActualStart: start, ActualEnd: end.Add(3*time.Hour + 10*time.Minute),
		OperatingHours: 31, DamagePaise: 5_000_000, DamageNote: "Cracked bucket tooth"})
	if err != nil {
		t.Fatal(err)
	}
	if late.LateHours != 4 || late.ExtraHours != 7 {
		t.Fatalf("late %d h, extra %v h", late.LateHours, late.ExtraHours)
	}
	byComp := map[string]int64{}
	for _, c := range late.Charges {
		byComp[c.Component] += c.Paise
	}
	rental := int64(3_600_000)
	lateFee, extra, fuel := 4*int64(150_000), 7*int64(180_000), 31*int64(90_000)
	fee := bps(rental+extra+lateFee, 200)
	if byComp[CompLateReturn] != lateFee || byComp[CompExtraHours] != extra || byComp[CompFuel] != fuel ||
		byComp[CompDamage] != 5_000_000 || byComp[CompOperator] != 4*60_000 || byComp[CompPlatformFee] != fee {
		t.Fatalf("components = %v", byComp)
	}
	supply := rental + extra + lateFee + est.DeliveryPaise + est.PickupPaise + 4*60_000 + fuel // damage is not taxed by GST here
	if late.TaxPaise != bps(supply, 1800)+bps(fee, 1800) {
		t.Fatalf("tax = %d", late.TaxPaise)
	}
	var sum int64
	for _, c := range late.Charges {
		sum += c.Paise
	}
	if sum != late.FinalTotalPaise || late.DifferencePaise != late.FinalTotalPaise-est.EstimatedTotalPaise || late.DepositPaise != est.DepositPaise {
		t.Fatalf("final = %+v", late)
	}

	// Early return still pays the booked period.
	early, _ := Finalize(est, start, end, Actuals{ActualStart: start, ActualEnd: end.Add(-24 * time.Hour), OperatingHours: 10})
	if early.LateHours != 0 || !strings.Contains(strings.Join(early.Notes, " "), "booked period") {
		t.Fatalf("early: %+v", early)
	}
	if _, err := Finalize(est, start, end, Actuals{ActualStart: end, ActualEnd: start}); err == nil {
		t.Fatal("end before start accepted")
	}
	if _, err := Finalize(Price{}, start, end, Actuals{ActualStart: start, ActualEnd: end}); err == nil {
		t.Fatal("old snapshots cannot be finalised")
	}
}

func TestCancellationRuleValidation(t *testing.T) {
	ok := []CancellationRule{{HoursBefore: 48, FeeBps: 0}, {HoursBefore: 24, FeeBps: 2500}, {HoursBefore: 0, FeeBps: 5000}}
	if err := ValidateCancellationRules(ok); err != nil {
		t.Fatal(err)
	}
	for name, rules := range map[string][]CancellationRule{
		"empty":          {},
		"no last-minute": {{HoursBefore: 24, FeeBps: 0}},
		"falling fee":    {{HoursBefore: 24, FeeBps: 5000}, {HoursBefore: 0, FeeBps: 1000}},
		"over 100%":      {{HoursBefore: 0, FeeBps: 12000}},
		"duplicate":      {{HoursBefore: 0, FeeBps: 0}, {HoursBefore: 0, FeeBps: 100}},
	} {
		if ValidateCancellationRules(rules) == nil {
			t.Errorf("%s accepted", name)
		}
	}
	raw, _ := json.Marshal(ok)
	if fee := CancellationFee(raw, 1_000_000, t0, t0.Add(30*time.Hour)); fee != 250_000 {
		t.Fatalf("30 h notice fee = %d", fee)
	}
}

func TestTimeZonesAndLocalDates(t *testing.T) {
	// Inclusive local dates in India: 12-14 March from 08:00 = 72 h.
	p, err := resolve(Slot{Dates: &DateRange{StartDate: "2027-03-12", EndDate: "2027-03-14"}}, "Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Start.Equal(time.Date(2027, 3, 12, 2, 30, 0, 0, time.UTC)) || p.End.Sub(p.Start) != 72*time.Hour {
		t.Fatalf("IST = %v - %v", p.Start, p.End)
	}
	v := view(p, "Asia/Kolkata")
	if v.StartLocal != "2027-03-12T08:00:00+05:30" || v.Timezone != "Asia/Kolkata" || v.Hours != 72 {
		t.Fatalf("view = %+v", v)
	}
	// Explicit end time.
	p, _ = resolve(Slot{Dates: &DateRange{StartDate: "2027-03-12", EndDate: "2027-03-12", StartTime: "09:00", EndTime: "17:30"}}, "Asia/Kolkata")
	if p.End.Sub(p.Start) != 8*time.Hour+30*time.Minute {
		t.Fatalf("same-day = %v", p.End.Sub(p.Start))
	}
	// A zone with daylight saving: the night clocks go forward is 23 h long,
	// and is still one day.
	p, _ = resolve(Slot{Dates: &DateRange{StartDate: "2027-03-13", EndDate: "2027-03-13"}}, "America/New_York")
	if p.End.Sub(p.Start) != 23*time.Hour {
		t.Fatalf("DST day = %v", p.End.Sub(p.Start))
	}
	if est := mustEstimate(t, excavator(), Options{Unit: "day", Start: p.Start, End: p.End}, com()); lines(est) != "1 day" {
		t.Fatalf("DST day priced as %s", lines(est))
	}
	// Exact instants keep their meaning whatever offset they were sent in.
	ist := time.Date(2027, 3, 12, 8, 0, 0, 0, time.FixedZone("IST", 19800))
	utc, _ := resolve(Slot{Period: &Period{Start: ist, End: ist.Add(24 * time.Hour)}}, "Asia/Kolkata")
	if !utc.Start.Equal(ist) || utc.Start.Location() != time.UTC {
		t.Fatalf("instant = %v", utc.Start)
	}
	// Plans and taxes follow the local date: 00:30 IST on 1 April is still
	// 31 March in UTC.
	if d := localDate(time.Date(2027, 3, 31, 19, 0, 0, 0, time.UTC), "Asia/Kolkata"); d.Format("2006-01-02") != "2027-04-01" {
		t.Fatalf("local date = %v", d)
	}
	for _, bad := range []Slot{{}, {Period: &Period{}, Dates: &DateRange{}}, {Dates: &DateRange{StartDate: "2027-13-01", EndDate: "2027-13-02"}}} {
		if _, err := resolve(bad, "Asia/Kolkata"); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if location("Not/AZone").String() != "Asia/Kolkata" {
		t.Fatal("unknown zone falls back to India")
	}
}

func TestINRFormatting(t *testing.T) {
	for in, want := range map[int64]string{0: "₹0", 150_000: "₹1,500", 12_345_678_950: "₹12,34,56,789.50", 99: "₹0.99", -100_000: "-₹1,000"} {
		if got := inr(in); got != want {
			t.Errorf("inr(%d) = %s, want %s", in, got, want)
		}
	}
}

// ---- service level ----------------------------------------------------------------------------

func TestEstimateServiceNeedsSiteForTransportAndReportsAvailability(t *testing.T) {
	svc, repo := newSvc()
	ctx := context.Background()
	id := uuid.New()
	req := EstimateRequest{Period: period(), PricingUnit: "auto"}
	if _, err := svc.Estimate(ctx, id, req); apperror.From(err).Status != 400 {
		t.Fatalf("no site with delivery: %v", err)
	}
	no := false
	req.Delivery, req.Pickup = &no, &no
	res, err := svc.Estimate(ctx, id, req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Availability.Available || res.Kind != "estimate" || res.Period == nil || res.Period.Timezone != "Asia/Kolkata" {
		t.Fatalf("estimate = %+v", res)
	}
	repo.busy = []Busy{{Start: req.Period.Start, End: req.Period.Start.Add(24 * time.Hour), Kind: "booked"}}
	res, _ = svc.Estimate(ctx, id, req)
	if res.Availability.Available || len(res.Availability.Conflicts) != 1 || res.Availability.NextAvailableStart == nil {
		t.Fatalf("busy = %+v", res.Availability)
	}
}

func TestSettleFinalRules(t *testing.T) {
	svc, repo := newSvc()
	ctx := context.Background()
	b := book(t, svc, "offline")
	stored := repo.bookings[b.ID]
	// Pretend the rental ran 4 to 1 days ago.
	stored.Period = Period{Start: time.Now().Add(-96 * time.Hour), End: time.Now().Add(-24 * time.Hour)}
	req := FinalChargesRequest{ActualStart: stored.Period.Start, ActualEnd: stored.Period.End.Add(2 * time.Hour)}

	// Too early in the lifecycle, the wrong side, the future.
	if _, err := svc.SettleFinal(ctx, owner, b.ID, req, true); !errors.Is(err, apperror.ErrInvalidTransition) {
		t.Fatalf("requested booking: %v", err)
	}
	stored.Status = StatusReturned
	if _, err := svc.SettleFinal(ctx, customer, b.ID, req, true); !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("customer: %v", err)
	}
	future := req
	future.ActualStart, future.ActualEnd = time.Now(), time.Now().Add(2*time.Hour)
	if _, err := svc.SettleFinal(ctx, owner, b.ID, future, false); apperror.From(err).Status != 400 {
		t.Fatalf("future end: %v", err)
	}

	preview, err := svc.SettleFinal(ctx, owner, b.ID, req, true)
	if err != nil || preview.Saved || !preview.DryRun || len(repo.final) != 0 {
		t.Fatalf("dry run: %+v %v", preview, err)
	}
	if preview.LateHours != 2 || preview.FinalTotalPaise <= preview.EstimatedTotalPaise {
		t.Fatalf("2 h late: %+v", preview)
	}
	saved, err := svc.SettleFinal(ctx, owner, b.ID, req, false)
	if err != nil || !saved.Saved || *stored.FinalTotal != saved.FinalTotalPaise || stored.FinalizedAt == nil {
		t.Fatalf("save: %+v %v", saved, err)
	}
	if _, err := svc.SettleFinal(ctx, owner, b.ID, req, false); !errors.Is(err, apperror.ErrConflict) {
		t.Fatalf("second settlement: %v", err)
	}
}

package bookings

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// Pricing engine. Every amount is integer paise; percentages are basis
// points rounded half-up per component (docs/API_DESIGN.md §3). The server
// always recomputes prices from stored listing terms: client-sent amounts
// are never trusted.

// Charge components (also the tax applicability keys in tax_rates).
const (
	CompRental      = "rental"
	CompExtraHours  = "extra_hours"
	CompDelivery    = "delivery"
	CompPickup      = "pickup"
	CompOperator    = "operator"
	CompFuel        = "fuel"
	CompPlatformFee = "platform_fee"
	CompLateReturn  = "late_return"
	CompDamage      = "damage"
)

// UnitAuto picks the cheapest mix of the listing's plans for any duration.
const UnitAuto = "auto"

// unitHours is the nominal length of each pricing unit.
var unitHours = map[string]float64{"hour": 1, "shift": 24, "day": 24, "week": 24 * 7, "month": 24 * 30}

// PlanRate is one active price plan of a listing for the chosen hire mode.
type PlanRate struct {
	Unit                    string   `json:"unit"`
	RatePaise               int64    `json:"rate_paise"`
	MinUnits                int32    `json:"min_units"`
	IncludedHours           *float64 `json:"included_hours,omitempty"`
	ExtraHourRatePaise      *int64   `json:"extra_hour_rate_paise,omitempty"`
	OperatorBataPerDayPaise int64    `json:"operator_bata_per_day_paise"`
}

// Terms are a listing's commercial terms, read from the database.
type Terms struct {
	Plans                []PlanRate
	DeliveryFixedPaise   int64
	DeliveryPerKmPaise   int64
	FreeKm               int32
	PickupFixedPaise     int64
	PickupPerKmPaise     int64
	FuelPolicy           string // customer | owner | shared
	FuelRatePerHourPaise int64
	DepositPaise         int64 // 0 = platform default
	LateFeePerHourPaise  int64 // 0 = derived from the hourly/daily rate
	LateGraceMinutes     int32
	MinRentalHours       int32 // 0 = none
	MaxRentalDays        int32 // 0 = platform maximum
	ServiceRadiusKm      int32
	Timezone             string
	CancellationRules    json.RawMessage
}

// TaxRule is a configurable tax (tax_rates).
type TaxRule struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	RateBps   int      `json:"rate_bps"`
	AppliesTo []string `json:"applies_to"`
	SACCode   string   `json:"sac_code,omitempty" doc:"Services accounting code printed on invoices"`
}

func (t TaxRule) applies(component string) bool { return slices.Contains(t.AppliesTo, component) }

// Applies reports whether the tax is charged on a charge component.
func (t TaxRule) Applies(component string) bool { return t.applies(component) }

// Commercials are platform settings that apply to a quote.
type Commercials struct {
	CustomerFeeBps      int32
	AdvanceBps          int
	DefaultDepositPaise int64
	Taxes               []TaxRule
}

// Options are what the customer chose.
type Options struct {
	Unit          string // hour | shift | day | week | month | auto
	WithOperator  bool
	Delivery      bool
	Pickup        bool
	DistanceKm    float64
	Start, End    time.Time
	ExpectedHours float64 // working hours; drives extra-hour and fuel estimates
}

// RentalLine is one priced unit line (several in auto mode).
type RentalLine struct {
	Unit      string `json:"unit"`
	Count     int64  `json:"count"`
	RatePaise int64  `json:"rate_paise"`
	Paise     int64  `json:"paise"`
}

// ChargeLine is one line of the breakdown shown to people.
type ChargeLine struct {
	Component string `json:"component"`
	Label     string `json:"label"`
	Paise     int64  `json:"paise"`
	Detail    string `json:"detail,omitempty"`
}

// TaxLine is one tax charged.
type TaxLine struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	RateBps      int    `json:"rate_bps"`
	TaxablePaise int64  `json:"taxable_paise"`
	Paise        int64  `json:"paise"`
}

// PeriodView is a rental period in UTC and the listing's local time.
type PeriodView struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Timezone   string    `json:"timezone"`
	StartLocal string    `json:"start_local" doc:"RFC 3339 in the listing's time zone"`
	EndLocal   string    `json:"end_local"`
	Hours      float64   `json:"hours"`
}

// FrozenTerms are the rates a booking was accepted at; final charges use
// these, never the listing's current prices.
type FrozenTerms struct {
	IncludedHours        float64         `json:"included_hours"`
	ExtraHourRatePaise   int64           `json:"extra_hour_rate_paise"`
	OperatorBataPerDay   int64           `json:"operator_bata_per_day_paise"`
	FuelPolicy           string          `json:"fuel_policy"`
	FuelRatePerHourPaise int64           `json:"fuel_rate_per_hour_paise"`
	LateFeePerHourPaise  int64           `json:"late_fee_per_hour_paise"`
	LateGraceMinutes     int32           `json:"late_grace_minutes"`
	CustomerFeeBps       int32           `json:"customer_fee_bps"`
	Taxes                []TaxRule       `json:"taxes"`
	CancellationRules    json.RawMessage `json:"cancellation_rules,omitempty"`
}

// Price is the estimate stored as the booking's price_snapshot. The older
// flat fields stay for payments, invoices and agreements.
type Price struct {
	Kind     string `json:"kind" doc:"Always estimate: final charges are settled after the rental"`
	Version  int    `json:"version"`
	Currency string `json:"currency"`

	Unit      string       `json:"unit" doc:"Chosen unit, or auto (cheapest mix)"`
	Units     int64        `json:"units"`
	RatePaise int64        `json:"rate_paise"`
	Lines     []RentalLine `json:"lines"`
	Days      int64        `json:"days"`
	Period    *PeriodView  `json:"period,omitempty"`

	WithOperator bool `json:"with_operator"`
	Delivery     bool `json:"delivery"`
	Pickup       bool `json:"pickup"`

	ExtraHours             float64   `json:"extra_hours"`
	ExtraHoursPaise        int64     `json:"extra_hours_paise"`
	RentalPaise            int64     `json:"rental_paise" doc:"Rental including extra hours"`
	ExpectedHours          float64   `json:"expected_hours,omitempty" doc:"Working hours the customer expects (as asked)"`
	MobilisationKm         float64   `json:"mobilisation_km"`
	DeliveryPaise          int64     `json:"delivery_paise"`
	PickupPaise            int64     `json:"pickup_paise"`
	MobilisationPaise      int64     `json:"mobilisation_paise" doc:"Delivery + pickup"`
	OperatorBataPaise      int64     `json:"operator_bata_paise"`
	FuelHours              float64   `json:"fuel_hours"`
	FuelPaise              int64     `json:"fuel_paise"`
	PlatformFeePaise       int64     `json:"platform_fee_paise"`
	SubtotalPaise          int64     `json:"subtotal_paise" doc:"Before taxes"`
	Taxes                  []TaxLine `json:"taxes"`
	GSTPaise               int64     `json:"gst_paise" doc:"All taxes"`
	EstimatedTotalPaise    int64     `json:"estimated_total_paise"`
	DepositPaise           int64     `json:"deposit_paise" doc:"Refundable security deposit, not part of the total"`
	AdvanceDuePaise        int64     `json:"advance_due_paise"`
	DueOnConfirmationPaise int64     `json:"due_on_confirmation_paise"`
	GSTRateBps             int       `json:"gst_rate_bps"`

	Charges []ChargeLine `json:"charges"`
	Notes   []string     `json:"notes"`
	Terms   *FrozenTerms `json:"terms,omitempty"`
}

// Pricing errors (mapped to user-facing validation messages by the service).
var (
	ErrNoPlan      = errors.New("this machine has no price for the selected unit and hire mode")
	ErrTooShort    = errors.New("rental is shorter than the owner's minimum")
	ErrTooLong     = errors.New("rental is longer than the owner's maximum")
	ErrOutsideArea = errors.New("the site is outside this machine's delivery area")
	ErrNeedSite    = errors.New("choose a site location for delivery or pickup")
)

// bps returns amount × rate / 10000, rounded half up.
func bps(amount int64, rate int) int64 {
	return (amount*int64(rate) + 5000) / 10000
}

func ceilEps(x float64) int64 { return int64(math.Ceil(x - 1e-9)) }

// chooseLines picks rental lines for a duration of h hours.
func chooseLines(plans []PlanRate, unit string, h float64) ([]RentalLine, error) {
	if unit != UnitAuto {
		var plan *PlanRate
		for i := range plans {
			if plans[i].Unit == unit {
				plan = &plans[i]
			}
		}
		if plan == nil {
			return nil, ErrNoPlan
		}
		n := max(ceilEps(h/unitHours[unit]), int64(plan.MinUnits), 1)
		return []RentalLine{{Unit: unit, Count: n, RatePaise: plan.RatePaise, Paise: n * plan.RatePaise}}, nil
	}

	by := map[string]PlanRate{}
	for _, p := range plans {
		if p.Unit != "shift" { // a shift is a day with fewer included hours; not mixed
			by[p.Unit] = p
		}
	}
	if len(by) == 0 {
		return nil, ErrNoPlan
	}
	count := func(u string, n int64) (int64, int64) { // effective count (min units) and cost
		p, ok := by[u]
		if !ok || n == 0 {
			return 0, 0
		}
		n = max(n, int64(p.MinUnits))
		return n, n * p.RatePaise
	}
	bound := func(u string) int64 {
		if _, ok := by[u]; !ok {
			return 0
		}
		return ceilEps(h/unitHours[u]) + 1
	}
	best := int64(math.MaxInt64)
	var bestLines []RentalLine
	for m := int64(0); m <= bound("month"); m++ {
		for w := int64(0); w <= bound("week"); w++ {
			for d := int64(0); d <= bound("day"); d++ {
				mc, mCost := count("month", m)
				wc, wCost := count("week", w)
				dc, dCost := count("day", d)
				covered := float64(mc)*720 + float64(wc)*168 + float64(dc)*24
				rest := h - covered
				var hc, hCost int64
				if rest > 1e-9 {
					if _, ok := by["hour"]; !ok {
						continue
					}
					hc, hCost = count("hour", ceilEps(rest))
				}
				cost := mCost + wCost + dCost + hCost
				units := mc + wc + dc + hc
				if units == 0 {
					continue
				}
				if cost < best || (cost == best && units < lineUnits(bestLines)) {
					best = cost
					bestLines = bestLines[:0:0]
					for _, x := range []struct {
						u string
						n int64
					}{{"month", mc}, {"week", wc}, {"day", dc}, {"hour", hc}} {
						if x.n > 0 {
							r := by[x.u].RatePaise
							bestLines = append(bestLines, RentalLine{Unit: x.u, Count: x.n, RatePaise: r, Paise: x.n * r})
						}
					}
				}
			}
		}
	}
	if bestLines == nil {
		return nil, ErrNoPlan
	}
	return bestLines, nil
}

func lineUnits(ls []RentalLine) int64 {
	if ls == nil {
		return math.MaxInt64
	}
	var n int64
	for _, l := range ls {
		n += l.Count
	}
	return n
}

// lateRate is the late-return charge per hour: the owner's own rate, else
// the hourly rate, else one eighth of the daily rate.
func lateRate(t Terms) int64 {
	if t.LateFeePerHourPaise > 0 {
		return t.LateFeePerHourPaise
	}
	for _, u := range []string{"hour", "day", "shift", "week", "month"} {
		for _, p := range t.Plans {
			if p.Unit == u {
				if u == "hour" {
					return p.RatePaise
				}
				return (p.RatePaise*24/int64(unitHours[u]) + 7) / 8
			}
		}
	}
	return 0
}

// Estimate prices a rental from the listing terms and the customer's
// options. It is deterministic: the same inputs always give the same paise.
func Estimate(t Terms, o Options, c Commercials) (Price, error) {
	h := o.End.Sub(o.Start).Hours()
	if h <= 0 {
		return Price{}, fmt.Errorf("end must be after start")
	}
	if t.MinRentalHours > 0 && h+1e-9 < float64(t.MinRentalHours) {
		return Price{}, ErrTooShort
	}
	if t.MaxRentalDays > 0 && h > float64(t.MaxRentalDays)*24+1e-9 {
		return Price{}, ErrTooLong
	}
	if (o.Delivery || o.Pickup) && t.ServiceRadiusKm > 0 && o.DistanceKm > float64(t.ServiceRadiusKm) {
		return Price{}, ErrOutsideArea
	}
	lines, err := chooseLines(t.Plans, o.Unit, h)
	if err != nil {
		return Price{}, err
	}

	days := max(ceilEps(h/24), 1)
	p := Price{
		Kind: "estimate", Version: 2, Currency: "INR", Unit: o.Unit, Lines: lines, Days: days,
		WithOperator: o.WithOperator, Delivery: o.Delivery, Pickup: o.Pickup, GSTRateBps: 0, Notes: []string{},
	}
	if len(lines) == 1 {
		p.Units, p.RatePaise = lines[0].Count, lines[0].RatePaise
	} else {
		for _, l := range lines {
			p.Units += l.Count
		}
	}
	var rental int64
	for _, l := range lines {
		rental += l.Paise
	}

	// Extra working hours beyond what the plan(s) include.
	frozen := FrozenTerms{FuelPolicy: t.FuelPolicy, FuelRatePerHourPaise: t.FuelRatePerHourPaise, LateFeePerHourPaise: lateRate(t),
		LateGraceMinutes: t.LateGraceMinutes, CustomerFeeBps: c.CustomerFeeBps, Taxes: c.Taxes, CancellationRules: t.CancellationRules}
	var bata int64
	for _, l := range lines {
		for _, pl := range t.Plans {
			if pl.Unit != l.Unit {
				continue
			}
			if pl.IncludedHours != nil {
				frozen.IncludedHours += *pl.IncludedHours * float64(l.Count)
			}
			if pl.ExtraHourRatePaise != nil && frozen.ExtraHourRatePaise == 0 {
				frozen.ExtraHourRatePaise = *pl.ExtraHourRatePaise
			}
			bata = max(bata, pl.OperatorBataPerDayPaise)
		}
	}
	frozen.OperatorBataPerDay = bata
	if frozen.IncludedHours > 0 && frozen.ExtraHourRatePaise > 0 && o.ExpectedHours > frozen.IncludedHours {
		p.ExtraHours = math.Ceil(o.ExpectedHours - frozen.IncludedHours)
		p.ExtraHoursPaise = int64(p.ExtraHours) * frozen.ExtraHourRatePaise
	}
	p.RentalPaise = rental + p.ExtraHoursPaise

	// Transport.
	km := math.Max(0, math.Round(o.DistanceKm)-float64(t.FreeKm))
	p.MobilisationKm = km
	if o.Delivery {
		p.DeliveryPaise = t.DeliveryFixedPaise + int64(km)*t.DeliveryPerKmPaise
	}
	if o.Pickup {
		p.PickupPaise = t.PickupFixedPaise + int64(km)*t.PickupPerKmPaise
	}
	p.MobilisationPaise = p.DeliveryPaise + p.PickupPaise

	if o.WithOperator {
		p.OperatorBataPaise = bata * days
	}

	// Fuel, when the owner supplies it: per working hour.
	if t.FuelPolicy == "owner" && t.FuelRatePerHourPaise > 0 {
		hrs := o.ExpectedHours
		if hrs <= 0 {
			hrs = math.Min(h, float64(days)*8)
			p.Notes = append(p.Notes, "Fuel is estimated at 8 working hours a day; you pay for the hours actually worked.")
		}
		p.FuelHours = math.Ceil(hrs)
		p.FuelPaise = int64(p.FuelHours) * t.FuelRatePerHourPaise
	} else if t.FuelPolicy == "customer" || t.FuelPolicy == "" {
		p.Notes = append(p.Notes, "Fuel is arranged by you.")
	} else {
		p.Notes = append(p.Notes, "Fuel is shared as agreed with the owner and settled separately.")
	}

	p.PlatformFeePaise = bps(p.RentalPaise, int(c.CustomerFeeBps))
	comps := map[string]int64{
		CompRental: rental, CompExtraHours: p.ExtraHoursPaise, CompDelivery: p.DeliveryPaise, CompPickup: p.PickupPaise,
		CompOperator: p.OperatorBataPaise, CompFuel: p.FuelPaise, CompPlatformFee: p.PlatformFeePaise,
	}
	p.Taxes, p.GSTPaise = taxes(c.Taxes, comps)
	for _, tx := range c.Taxes {
		if tx.Code == "gst" {
			p.GSTRateBps = tx.RateBps
		}
	}
	p.SubtotalPaise = rental + p.ExtraHoursPaise + p.MobilisationPaise + p.OperatorBataPaise + p.FuelPaise + p.PlatformFeePaise
	p.EstimatedTotalPaise = p.SubtotalPaise + p.GSTPaise

	p.DepositPaise = t.DepositPaise
	if p.DepositPaise == 0 {
		p.DepositPaise = c.DefaultDepositPaise
	}
	p.AdvanceDuePaise = bps(p.EstimatedTotalPaise, c.AdvanceBps)
	p.DueOnConfirmationPaise = p.AdvanceDuePaise + p.DepositPaise
	p.Charges = chargeLines(p, lines, frozen)
	if late := frozen.LateFeePerHourPaise; late > 0 {
		p.Notes = append(p.Notes, fmt.Sprintf("Late return: %s per hour after a %d-minute grace period.", inr(late), t.LateGraceMinutes))
	}
	p.Terms = &frozen
	return p, nil
}

// taxes applies each rule to the components it covers. The platform fee is
// taxed separately from the owner's supply (two invoices), as in §3.
func taxes(rules []TaxRule, comps map[string]int64) ([]TaxLine, int64) {
	out := []TaxLine{}
	var total int64
	for _, r := range rules {
		var supply int64
		for comp, v := range comps {
			if comp != CompPlatformFee && r.applies(comp) {
				supply += v
			}
		}
		amount := bps(supply, r.RateBps)
		taxable := supply
		if r.applies(CompPlatformFee) {
			amount += bps(comps[CompPlatformFee], r.RateBps)
			taxable += comps[CompPlatformFee]
		}
		if taxable == 0 {
			continue
		}
		out = append(out, TaxLine{Code: r.Code, Name: r.Name, RateBps: r.RateBps, TaxablePaise: taxable, Paise: amount})
		total += amount
	}
	return out, total
}

func chargeLines(p Price, lines []RentalLine, f FrozenTerms) []ChargeLine {
	var out []ChargeLine
	add := func(comp, label string, paise int64, detail string) {
		if paise > 0 {
			out = append(out, ChargeLine{Component: comp, Label: label, Paise: paise, Detail: detail})
		}
	}
	for _, l := range lines {
		add(CompRental, "Rental", l.Paise, fmt.Sprintf("%d %s × %s", l.Count, plural(l.Unit, l.Count), inr(l.RatePaise)))
	}
	add(CompExtraHours, "Extra hours", p.ExtraHoursPaise, fmt.Sprintf("%.0f h × %s", p.ExtraHours, inr(f.ExtraHourRatePaise)))
	add(CompDelivery, "Delivery to site", p.DeliveryPaise, fmt.Sprintf("%.0f chargeable km", p.MobilisationKm))
	add(CompPickup, "Pickup from site", p.PickupPaise, "")
	add(CompOperator, "Operator", p.OperatorBataPaise, fmt.Sprintf("%d %s × %s", p.Days, plural("day", p.Days), inr(f.OperatorBataPerDay)))
	add(CompFuel, "Fuel", p.FuelPaise, fmt.Sprintf("%.0f h × %s", p.FuelHours, inr(f.FuelRatePerHourPaise)))
	add(CompPlatformFee, "Platform fee", p.PlatformFeePaise, "")
	for _, t := range p.Taxes {
		add("tax", fmt.Sprintf("%s %s%%", t.Name, pct(t.RateBps)), t.Paise, "")
	}
	return out
}

func plural(unit string, n int64) string {
	if n == 1 {
		return unit
	}
	return unit + "s"
}

func pct(b int) string {
	if b%100 == 0 {
		return fmt.Sprint(b / 100)
	}
	return fmt.Sprintf("%.2f", float64(b)/100)
}

// inr formats paise as ₹1,23,456.50 (Indian grouping).
func inr(paise int64) string {
	neg := paise < 0
	if neg {
		paise = -paise
	}
	r, ps := paise/100, paise%100
	s := fmt.Sprint(r)
	if len(s) > 3 {
		head, tail := s[:len(s)-3], s[len(s)-3:]
		var parts []string
		for len(head) > 2 {
			parts = append([]string{head[len(head)-2:]}, parts...)
			head = head[:len(head)-2]
		}
		if head != "" {
			parts = append([]string{head}, parts...)
		}
		s = strings.Join(append(parts, tail), ",")
	}
	if ps != 0 {
		s += fmt.Sprintf(".%02d", ps)
	}
	if neg {
		return "-₹" + s
	}
	return "₹" + s
}

// ---- final charges ------------------------------------------------------------------------

// Actuals are what happened, recorded by the owner after the rental.
type Actuals struct {
	ActualStart    time.Time
	ActualEnd      time.Time
	OperatingHours float64 // hour-meter hours worked (0 = not recorded)
	DamagePaise    int64
	DamageNote     string
}

// FinalCharges are the settled charges for a booking (kind "final").
type FinalCharges struct {
	Kind                string       `json:"kind" doc:"Always final"`
	Currency            string       `json:"currency"`
	PlannedHours        float64      `json:"planned_hours"`
	ActualHours         float64      `json:"actual_hours"`
	OperatingHours      float64      `json:"operating_hours"`
	LateHours           int64        `json:"late_hours"`
	ExtraHours          float64      `json:"extra_hours"`
	Charges             []ChargeLine `json:"charges"`
	Taxes               []TaxLine    `json:"taxes"`
	SubtotalPaise       int64        `json:"subtotal_paise"`
	TaxPaise            int64        `json:"tax_paise"`
	FinalTotalPaise     int64        `json:"final_total_paise"`
	EstimatedTotalPaise int64        `json:"estimated_total_paise"`
	DifferencePaise     int64        `json:"difference_paise" doc:"Final minus estimate"`
	DepositPaise        int64        `json:"deposit_paise" doc:"Refundable security deposit (not included)"`
	Notes               []string     `json:"notes"`
}

// Finalize settles a booking from its estimate (whose rates are frozen) and
// the actuals. The booked period is the minimum charge; returning late adds
// a late fee per started hour after the grace period; worked hours beyond
// what the plans include are extra hours; owner-supplied fuel is charged on
// worked hours.
func Finalize(est Price, start, end time.Time, a Actuals) (FinalCharges, error) {
	if est.Terms == nil {
		return FinalCharges{}, errors.New("this booking was priced before final settlement was supported")
	}
	if !a.ActualEnd.After(a.ActualStart) {
		return FinalCharges{}, errors.New("the actual end must be after the actual start")
	}
	f := *est.Terms
	fc := FinalCharges{
		Kind: "final", Currency: "INR", PlannedHours: end.Sub(start).Hours(), ActualHours: a.ActualEnd.Sub(a.ActualStart).Hours(),
		OperatingHours: a.OperatingHours, EstimatedTotalPaise: est.EstimatedTotalPaise, DepositPaise: est.DepositPaise, Notes: []string{},
	}
	var rental int64
	for _, l := range est.Lines {
		rental += l.Paise
	}

	// Late return, counted from the planned end once the grace is exceeded.
	over := a.ActualEnd.Sub(end)
	var late int64
	billedEnd := end // within the grace period nothing extends
	if over > time.Duration(f.LateGraceMinutes)*time.Minute {
		fc.LateHours = ceilEps(over.Hours())
		late = fc.LateHours * f.LateFeePerHourPaise
		billedEnd = a.ActualEnd
	}

	// Extra hours on the hour meter (else the estimate's expectation stands).
	extra := est.ExtraHoursPaise
	fc.ExtraHours = est.ExtraHours
	if a.OperatingHours > 0 && f.IncludedHours > 0 && f.ExtraHourRatePaise > 0 {
		fc.ExtraHours = math.Max(0, math.Ceil(a.OperatingHours-f.IncludedHours))
		extra = int64(fc.ExtraHours) * f.ExtraHourRatePaise
	}

	// Operator days over the longer of the booked and actual periods.
	var bata int64
	if est.WithOperator {
		days := max(ceilEps(billedEnd.Sub(start).Hours()/24), 1)
		bata = days * f.OperatorBataPerDay
	}

	fuel := est.FuelPaise
	if f.FuelPolicy == "owner" && f.FuelRatePerHourPaise > 0 && a.OperatingHours > 0 {
		fuel = int64(math.Ceil(a.OperatingHours)) * f.FuelRatePerHourPaise
	}

	fee := bps(rental+extra+late, int(f.CustomerFeeBps))
	comps := map[string]int64{
		CompRental: rental, CompExtraHours: extra, CompDelivery: est.DeliveryPaise, CompPickup: est.PickupPaise,
		CompOperator: bata, CompFuel: fuel, CompPlatformFee: fee, CompLateReturn: late, CompDamage: a.DamagePaise,
	}
	fc.Taxes, fc.TaxPaise = taxes(f.Taxes, comps)

	add := func(comp, label string, paise int64, detail string) {
		if paise > 0 {
			fc.Charges = append(fc.Charges, ChargeLine{Component: comp, Label: label, Paise: paise, Detail: detail})
		}
	}
	for _, l := range est.Lines {
		add(CompRental, "Rental", l.Paise, fmt.Sprintf("%d %s × %s (as booked)", l.Count, plural(l.Unit, l.Count), inr(l.RatePaise)))
	}
	add(CompExtraHours, "Extra hours", extra, fmt.Sprintf("%.0f h × %s", fc.ExtraHours, inr(f.ExtraHourRatePaise)))
	add(CompLateReturn, "Late return", late, fmt.Sprintf("%d h × %s", fc.LateHours, inr(f.LateFeePerHourPaise)))
	add(CompDelivery, "Delivery to site", est.DeliveryPaise, "")
	add(CompPickup, "Pickup from site", est.PickupPaise, "")
	add(CompOperator, "Operator", bata, "")
	add(CompFuel, "Fuel", fuel, "")
	add(CompDamage, "Damage recovery", a.DamagePaise, a.DamageNote)
	add(CompPlatformFee, "Platform fee", fee, "")
	for _, t := range fc.Taxes {
		add("tax", fmt.Sprintf("%s %s%%", t.Name, pct(t.RateBps)), t.Paise, "")
	}
	fc.SubtotalPaise = rental + extra + late + est.DeliveryPaise + est.PickupPaise + bata + fuel + a.DamagePaise + fee
	fc.FinalTotalPaise = fc.SubtotalPaise + fc.TaxPaise
	fc.DifferencePaise = fc.FinalTotalPaise - est.EstimatedTotalPaise
	if a.ActualEnd.Before(end) {
		fc.Notes = append(fc.Notes, "Returned early: the booked period is still charged.")
	}
	return fc, nil
}

// ---- compatibility ------------------------------------------------------------------------

// PriceInputs is the single-plan input of the original quote API (kept for
// the documented worked example and older callers).
type PriceInputs struct {
	Unit                    string
	RatePaise               int64
	MinUnits                int32
	IncludedHours           *float64
	ExtraHourRatePaise      *int64
	OperatorBataPerDayPaise int64
	WithOperator            bool
	MobFixedPaise           int64
	MobPerKmPaise           int64
	MobFreeKm               int32
	DistanceKm              float64
	Start, End              time.Time
	ExpectedHours           float64
	CustomerFeeBps          int32
	GSTBps                  int
	AdvanceBps              int
	DepositPaise            int64
}

// AllComponents is every taxable component.
var AllComponents = []string{CompRental, CompExtraHours, CompDelivery, CompPickup, CompOperator, CompFuel, CompPlatformFee, CompLateReturn}

// Quote prices a single-plan rental with GST on everything (one-way
// mobilisation = delivery).
func Quote(in PriceInputs) Price {
	p, err := Estimate(Terms{
		Plans: []PlanRate{{Unit: in.Unit, RatePaise: in.RatePaise, MinUnits: in.MinUnits, IncludedHours: in.IncludedHours,
			ExtraHourRatePaise: in.ExtraHourRatePaise, OperatorBataPerDayPaise: in.OperatorBataPerDayPaise}},
		DeliveryFixedPaise: in.MobFixedPaise, DeliveryPerKmPaise: in.MobPerKmPaise, FreeKm: in.MobFreeKm, LateGraceMinutes: 60,
	}, Options{Unit: in.Unit, WithOperator: in.WithOperator, Delivery: true, DistanceKm: in.DistanceKm, Start: in.Start, End: in.End,
		ExpectedHours: in.ExpectedHours},
		Commercials{CustomerFeeBps: in.CustomerFeeBps, AdvanceBps: in.AdvanceBps, DefaultDepositPaise: in.DepositPaise,
			Taxes: []TaxRule{{Code: "gst", Name: "GST", RateBps: in.GSTBps, AppliesTo: AllComponents}}})
	if err != nil {
		return Price{}
	}
	return p
}

// ---- cancellation ---------------------------------------------------------------------------

// CancellationRule charges FeeBps of the estimate when cancelling at least
// HoursBefore hours before the start.
type CancellationRule struct {
	HoursBefore float64 `json:"hours_before"`
	FeeBps      int     `json:"fee_bps"`
}

// ValidateCancellationRules checks an owner's custom policy: 1 to 6 rules,
// distinct notice periods, fees within 0-100%, and fees that never fall as
// the start approaches.
func ValidateCancellationRules(rules []CancellationRule) error {
	if len(rules) == 0 || len(rules) > 6 {
		return errors.New("give 1 to 6 rules")
	}
	r := slices.Clone(rules)
	sort.Slice(r, func(i, j int) bool { return r[i].HoursBefore > r[j].HoursBefore })
	for i, x := range r {
		if x.HoursBefore < 0 || x.HoursBefore > 720 {
			return errors.New("notice must be between 0 and 720 hours")
		}
		if x.FeeBps < 0 || x.FeeBps > 10000 {
			return errors.New("fees are between 0% and 100%")
		}
		if i > 0 && x.HoursBefore == r[i-1].HoursBefore {
			return errors.New("each notice period may appear once")
		}
		if i > 0 && x.FeeBps < r[i-1].FeeBps {
			return errors.New("the fee cannot fall as the start date approaches")
		}
	}
	if r[len(r)-1].HoursBefore != 0 {
		return errors.New("include a rule for 0 hours (cancelling at the last minute)")
	}
	return nil
}

// CancellationFee applies a policy: the rule with the largest HoursBefore not
// exceeding the notice given wins. No policy or no matching rule = free.
func CancellationFee(rulesJSON json.RawMessage, estimatePaise int64, now, start time.Time) int64 {
	var rules []CancellationRule
	if len(rulesJSON) == 0 || json.Unmarshal(rulesJSON, &rules) != nil || len(rules) == 0 {
		return 0
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].HoursBefore > rules[j].HoursBefore })
	notice := start.Sub(now).Hours()
	for _, r := range rules {
		if notice >= r.HoursBefore {
			return bps(estimatePaise, r.FeeBps)
		}
	}
	// Cancelling after the start: the strictest (last) rule applies.
	return bps(estimatePaise, rules[len(rules)-1].FeeBps)
}

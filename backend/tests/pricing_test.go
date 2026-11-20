package tests

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// pricingSetup publishes a loader with hourly, daily, weekly and monthly
// prices on a fresh database (taxes and blocks stay isolated).
type pricingSetup struct {
	e        *env
	owner    *user
	ops      *user
	customer *user
	site     map[string]float64
	siteID   string
	eq       string
}

func newPricingSetup(t *testing.T, extra map[string]any) pricingSetup {
	e := newIsolatedEnv(t)
	kyc := e.kycAdmin()
	ops := e.opsAdmin()
	owner := e.verifiedOwner(kyc, "Rates Rentals")
	body := map[string]any{"title": "Liugong 835H loader " + randomLetters(5), "dry_hire": true}
	for k, v := range extra {
		body[k] = v
	}
	eq := e.publishAt(owner, ops, body, [2]float64{18.5204, 73.8567},
		price{"hour", 150000, true}, price{"day", 1200000, true}, price{"week", 7000000, true}, price{"month", 24000000, true},
		price{"day", 1000000, false})
	// Mobilisation: ₹3,000 + ₹60/km after 10 km.
	must(t, owner.do(http.MethodPut, "/owner/equipment/"+eq+"/pricing", map[string]any{
		"plans": []map[string]any{
			{"unit": "hour", "with_operator": true, "rate_paise": 150000, "min_units": 4},
			{"unit": "day", "with_operator": true, "rate_paise": 1200000, "included_hours": 8, "extra_hour_rate_paise": 180000, "operator_bata_per_day_paise": 60000},
			{"unit": "week", "with_operator": true, "rate_paise": 7000000},
			{"unit": "month", "with_operator": true, "rate_paise": 24000000},
			{"unit": "day", "with_operator": false, "rate_paise": 1000000},
		},
		"mobilisation": map[string]any{"fixed_paise": 300000, "per_km_paise": 6000, "free_km": 10, "needs_trailer": true},
	}), 200, "pricing")
	cust := e.registerAndVerify("customer", "")
	site := map[string]float64{"lat": 18.5912, "lng": 73.7389} // Hinjewadi, ~14 km away
	addr := must(t, cust.do(http.MethodPost, "/orgs/"+cust.org+"/addresses", map[string]any{
		"label": "Site", "line1": "Plot 12", "city": "Pune", "state_code": "27", "pincode": "411057", "is_site": true, "location": site,
	}), 201, "site")
	return pricingSetup{e: e, owner: owner, ops: ops, customer: cust, site: site, siteID: addr.str("id"), eq: eq}
}

func istDate(d time.Time) string { return d.In(time.FixedZone("IST", 19800)).Format("2006-01-02") }

func (s pricingSetup) estimate(t *testing.T, body map[string]any, want int) resp {
	t.Helper()
	return must(t, s.e.call(http.MethodPost, "/equipment/"+s.eq+"/estimate", "", "", body, nil), want, "estimate")
}

func (s pricingSetup) rules(t *testing.T, body map[string]any, want int) resp {
	t.Helper()
	return must(t, s.owner.do(http.MethodPut, "/owner/equipment/"+s.eq+"/booking-rules", body), want, "booking rules")
}

func sumCharges(r resp) float64 {
	var n float64
	for _, c := range r.list("charges") {
		n += c.(map[string]any)["paise"].(float64)
	}
	return n
}

func TestPricingEstimateEndToEnd(t *testing.T) {
	s := newPricingSetup(t, nil)
	day := time.Now().Add(72 * time.Hour)

	// Local dates (inclusive, from 08:00 IST), auto unit, every option on.
	r := s.estimate(t, map[string]any{
		"site": s.site, "dates": map[string]any{"start_date": istDate(day), "end_date": istDate(day.Add(8 * 24 * time.Hour))},
		"pricing_unit": "auto", "with_operator": true, "expected_hours": 80,
	}, 200)
	if r.str("kind") != "estimate" || r.str("unit") != "auto" || r.str("period.timezone") != "Asia/Kolkata" ||
		!strings.HasSuffix(r.str("period.start_local"), "T08:00:00+05:30") || r.num("period.hours") != 216 {
		t.Fatalf("estimate header: %s", r.raw)
	}
	// 9 days = 1 week + 2 days (₹94,000), cheaper than 9 days or 2 weeks.
	if l := r.list("lines"); len(l) != 2 || r.num("lines.0.count") != 1 || r.str("lines.0.unit") != "week" || r.num("lines.1.count") != 2 {
		t.Fatalf("lines: %v", r.list("lines"))
	}
	// Included hours come only from the day plan (2 × 8 h): 80 expected → 64 extra.
	if r.num("extra_hours") != 64 || r.num("extra_hours_paise") != 64*180000 {
		t.Fatalf("extra hours: %v / %v", r.num("extra_hours"), r.num("extra_hours_paise"))
	}
	km := r.num("mobilisation_km")
	if km < 3 || km > 6 || r.num("delivery_paise") != 300000+km*6000 || r.num("pickup_paise") != 0 {
		t.Fatalf("transport: km=%v delivery=%v pickup=%v", km, r.num("delivery_paise"), r.num("pickup_paise"))
	}
	if r.num("operator_bata_paise") != 9*60000 || r.str("taxes.0.code") != "gst" || r.num("taxes.0.rate_bps") != 1800 {
		t.Fatalf("operator/taxes: %s", r.raw)
	}
	if sumCharges(r) != r.num("estimated_total_paise") || r.num("deposit_paise") != float64(s.e.cfg.Booking.DefaultDepositPaise) ||
		r.body["availability"].(map[string]any)["available"] != true {
		t.Fatalf("totals/availability: %s", r.raw)
	}

	// Collect and return yourself: no site needed, no transport charges.
	self := s.estimate(t, map[string]any{
		"period": map[string]any{"start": day, "end": day.Add(5 * time.Hour)}, "pricing_unit": "auto", "with_operator": true, "delivery": false, "pickup": false,
	}, 200)
	if self.num("mobilisation_paise") != 0 || self.str("lines.0.unit") != "hour" || self.num("rental_paise") != 5*150000 {
		t.Fatalf("self-collect: %s", self.raw)
	}
	// Machine only uses the dry-hire plan.
	dry := s.estimate(t, map[string]any{"period": map[string]any{"start": day, "end": day.Add(48 * time.Hour)}, "pricing_unit": "day",
		"with_operator": false, "delivery": false, "pickup": false}, 200)
	if dry.num("rental_paise") != 2*1000000 || dry.num("operator_bata_paise") != 0 {
		t.Fatalf("dry hire: %s", dry.raw)
	}

	// Validation.
	for name, body := range map[string]map[string]any{
		"no site with delivery": {"period": map[string]any{"start": day, "end": day.Add(24 * time.Hour)}, "pricing_unit": "day"},
		"both period and dates": {"period": map[string]any{"start": day, "end": day.Add(24 * time.Hour)}, "dates": map[string]any{"start_date": istDate(day), "end_date": istDate(day)},
			"pricing_unit": "day", "delivery": false, "pickup": false},
		"in the past": {"period": map[string]any{"start": day.Add(-100 * time.Hour), "end": day}, "pricing_unit": "day", "delivery": false, "pickup": false},
		"too long":    {"period": map[string]any{"start": day, "end": day.Add(181 * 24 * time.Hour)}, "pricing_unit": "auto", "delivery": false, "pickup": false},
		"bad unit":    {"period": map[string]any{"start": day, "end": day.Add(24 * time.Hour)}, "pricing_unit": "fortnight", "delivery": false, "pickup": false},
		"bad date":    {"dates": map[string]any{"start_date": "12/03/2027", "end_date": "2027-03-14"}, "pricing_unit": "day", "delivery": false, "pickup": false},
	} {
		if r := s.e.call(http.MethodPost, "/equipment/"+s.eq+"/estimate", "", "", body, nil); r.code != 400 {
			t.Errorf("%s: %d %s", name, r.code, r.raw)
		}
	}
	// No shift plan on this listing.
	s.estimate(t, map[string]any{"period": map[string]any{"start": day, "end": day.Add(24 * time.Hour)}, "pricing_unit": "shift",
		"delivery": false, "pickup": false}, 422)
}

func TestOwnerBookingRules(t *testing.T) {
	s := newPricingSetup(t, nil)
	day := time.Now().Add(72 * time.Hour)
	period := map[string]any{"start": day, "end": day.Add(48 * time.Hour)}

	got := must(t, s.owner.do(http.MethodGet, "/owner/equipment/"+s.eq+"/booking-rules", nil), 200, "rules")
	if got.str("timezone") != "Asia/Kolkata" || got.num("late_grace_minutes") != 60 || got.str("fuel_policy") != "customer" {
		t.Fatalf("defaults: %s", got.raw)
	}
	presets := must(t, s.e.call(http.MethodGet, "/cancellation-policies", "", "", nil, nil), 200, "presets")
	for _, code := range []string{"flexible", "standard", "strict"} {
		if !strings.Contains(presets.raw, `"code":"`+code+`"`) {
			t.Fatalf("preset %s missing: %s", code, presets.raw)
		}
	}

	rules := map[string]any{
		"min_rental_hours": 24, "max_rental_days": 30, "security_deposit_paise": 5000000, "fuel_policy": "owner",
		"fuel_rate_per_hour_paise": 90000, "late_fee_per_hour_paise": 250000, "late_grace_minutes": 30,
		"pickup_fixed_paise": 200000, "pickup_per_km_paise": 4000, "timezone": "Asia/Kolkata",
		"cancellation": map[string]any{"policy": "custom", "name": "Our policy", "rules": []map[string]any{
			{"hours_before": 48, "fee_bps": 0}, {"hours_before": 12, "fee_bps": 2000}, {"hours_before": 0, "fee_bps": 5000}}},
	}
	saved := s.rules(t, rules, 200)
	if saved.str("cancellation.policy") != "custom" || len(saved.list("cancellation.rules")) != 3 || saved.num("security_deposit_paise") != 5000000 {
		t.Fatalf("saved: %s", saved.raw)
	}
	// The rules drive the estimate.
	est := s.estimate(t, map[string]any{"site": s.site, "period": period, "pricing_unit": "day", "with_operator": true, "expected_hours": 12}, 200)
	if est.num("deposit_paise") != 5000000 || est.num("fuel_paise") != 12*90000 || est.num("pickup_paise") < 200000 {
		t.Fatalf("rules not applied: %s", est.raw)
	}
	short := s.estimate(t, map[string]any{"period": map[string]any{"start": day, "end": day.Add(6 * time.Hour)}, "pricing_unit": "hour",
		"delivery": false, "pickup": false}, 422)
	if !strings.Contains(short.raw, "minimum") {
		t.Fatalf("minimum: %s", short.raw)
	}
	s.estimate(t, map[string]any{"period": map[string]any{"start": day, "end": day.Add(31 * 24 * time.Hour)}, "pricing_unit": "auto",
		"delivery": false, "pickup": false}, 422)

	// Invalid rules.
	for name, change := range map[string]map[string]any{
		"min above max":       {"min_rental_hours": 800, "max_rental_days": 30},
		"bad zone":            {"timezone": "Mars/Olympus"},
		"owner fuel no rate":  {"fuel_rate_per_hour_paise": nil},
		"falling custom fee":  {"cancellation": map[string]any{"policy": "custom", "rules": []map[string]any{{"hours_before": 24, "fee_bps": 5000}, {"hours_before": 0, "fee_bps": 0}}}},
		"unknown preset":      {"cancellation": map[string]any{"policy": "lenient"}},
		"rules with a preset": {"cancellation": map[string]any{"policy": "strict", "rules": []map[string]any{{"hours_before": 0, "fee_bps": 0}}}},
		"max over 180 days":   {"max_rental_days": 365},
	} {
		body := map[string]any{}
		for k, v := range rules {
			body[k] = v
		}
		for k, v := range change {
			body[k] = v
		}
		if r := s.owner.do(http.MethodPut, "/owner/equipment/"+s.eq+"/booking-rules", body); r.code != 400 {
			t.Errorf("%s: %d %s", name, r.code, r.raw)
		}
	}
	// Only the owner.
	other := s.e.verifiedOwner(s.e.kycAdmin(), "Other Owner")
	must(t, other.do(http.MethodPut, "/owner/equipment/"+s.eq+"/booking-rules", rules), 404, "another owner")
	must(t, s.customer.do(http.MethodGet, "/owner/equipment/"+s.eq+"/booking-rules", nil), 403, "customer")

	// A preset policy; bookings keep the policy they were made under.
	s.rules(t, map[string]any{"fuel_policy": "customer", "cancellation": map[string]any{"policy": "strict"}}, 200)
	bk := must(t, s.customer.do(http.MethodPost, "/bookings", map[string]any{
		"equipment_id": s.eq, "site_address_id": s.siteID, "period": period, "pricing_unit": "day", "with_operator": true,
	}, "Idempotency-Key", fmt.Sprintf("rules-%d", rand.Int64())), 201, "book")
	must(t, s.owner.do(http.MethodPost, "/owner/bookings/"+bk.str("id")+"/accept", nil), 200, "accept")
	s.rules(t, map[string]any{"fuel_policy": "customer", "cancellation": map[string]any{"policy": "flexible"}}, 200)
	fee := must(t, s.customer.do(http.MethodPost, "/bookings/"+bk.str("id")+"/cancel?dry_run=true", nil), 200, "fee preview")
	// 72 h notice under "strict" (free from 72 h) costs nothing; under 72 h it is 50%.
	if fee.num("fee_paise") != 0 && fee.num("fee_paise") != float64(int64(bk.num("estimated_total_paise"))*5000/10000) {
		t.Fatalf("strict fee = %s", fee.raw)
	}
}

func TestAvailabilityCalendarAndOverlaps(t *testing.T) {
	s := newPricingSetup(t, map[string]any{"instant_book": true})
	base := time.Now().Add(96 * time.Hour).UTC().Truncate(time.Hour)

	// Owner blocks two days for maintenance.
	must(t, s.owner.do(http.MethodPost, "/owner/equipment/"+s.eq+"/blocks", map[string]any{
		"start": base, "end": base.Add(48 * time.Hour), "reason": "maintenance", "note": "250 h service",
	}), 201, "block")

	check := func(from, to time.Time) resp {
		q := fmt.Sprintf("/equipment/%s/availability/check?start=%s&end=%s", s.eq, from.Format(time.RFC3339), to.Format(time.RFC3339))
		return must(t, s.e.call(http.MethodGet, q, "", "", nil, nil), 200, "check")
	}
	busy := check(base.Add(24*time.Hour), base.Add(72*time.Hour))
	if busy.body["available"] != false || busy.str("conflicts.0.kind") != "blocked" {
		t.Fatalf("blocked: %s", busy.raw)
	}
	next, _ := time.Parse(time.RFC3339, busy.str("next_available_start"))
	if !next.Equal(base.Add(48 * time.Hour)) {
		t.Fatalf("next free start = %v, want %v", next, base.Add(48*time.Hour))
	}
	if free := check(base.Add(48*time.Hour), base.Add(72*time.Hour)); free.body["available"] != true {
		t.Fatalf("free after the block: %s", free.raw)
	}
	// Back-to-back periods ([start, end)) do not overlap.
	if edge := check(base.Add(-24*time.Hour), base); edge.body["available"] != true {
		t.Fatalf("ends exactly at the block start: %s", edge.raw)
	}

	// Booking into the block is refused.
	req := func(from, to time.Time) map[string]any {
		return map[string]any{"equipment_id": s.eq, "site_address_id": s.siteID, "period": map[string]any{"start": from, "end": to},
			"pricing_unit": "day", "with_operator": true, "payment_mode": "offline"}
	}
	clash := must(t, s.customer.do(http.MethodPost, "/bookings", req(base.Add(24*time.Hour), base.Add(72*time.Hour)),
		"Idempotency-Key", fmt.Sprintf("b-%d", rand.Int64())), 409, "book into block")
	if clash.str("code") != "BOOKING_OVERLAP" {
		t.Fatalf("clash = %s", clash.raw)
	}

	// Concurrency: eight customers race for the same slot on an instant-book
	// listing (confirmed at once); exactly one wins.
	slot := [2]time.Time{base.Add(72 * time.Hour), base.Add(120 * time.Hour)}
	customers := make([]*user, 8)
	for i := range customers {
		c := s.e.registerAndVerify("customer", "")
		a := must(t, c.do(http.MethodPost, "/orgs/"+c.org+"/addresses", map[string]any{
			"label": "Site", "line1": "Plot", "city": "Pune", "state_code": "27", "pincode": "411057", "is_site": true, "location": s.site,
		}), 201, "site")
		c.org = c.org + "|" + a.str("id")
		customers[i] = c
	}
	var wg sync.WaitGroup
	codes := make([]int, len(customers))
	for i, c := range customers {
		wg.Add(1)
		go func(i int, c *user) {
			defer wg.Done()
			org, site, _ := strings.Cut(c.org, "|")
			body := req(slot[0], slot[1])
			body["site_address_id"] = site
			r := s.e.call(http.MethodPost, "/bookings", c.token, org, body, map[string]string{"Idempotency-Key": fmt.Sprintf("race-%d-%d", i, rand.Int64())})
			codes[i] = r.code
		}(i, c)
	}
	wg.Wait()
	won, lost := 0, 0
	for _, c := range codes {
		switch c {
		case 201:
			won++
		case 409:
			lost++
		default:
			t.Fatalf("unexpected status %d in %v", c, codes)
		}
	}
	if won != 1 || lost != len(customers)-1 {
		t.Fatalf("race: %v", codes)
	}
	var held int
	if err := s.e.pool.QueryRow(t.Context(), `SELECT count(*) FROM bookings WHERE equipment_id = $1 AND booking_holds_slot(status)`, s.eq).Scan(&held); err != nil || held != 1 {
		t.Fatalf("held bookings = %d (%v)", held, err)
	}
	// The database refuses an overlap even if the API check is bypassed.
	if _, err := s.e.pool.Exec(t.Context(), `UPDATE bookings SET period = tstzrange($2, $3) WHERE equipment_id = $1 AND booking_holds_slot(status)`,
		s.eq, base.Add(24*time.Hour), base.Add(72*time.Hour)); err == nil {
		t.Fatal("moving a held booking into a block succeeded")
	}
	// And an owner cannot block days a booking holds.
	must(t, s.owner.do(http.MethodPost, "/owner/equipment/"+s.eq+"/blocks", map[string]any{
		"start": slot[0].Add(12 * time.Hour), "end": slot[1], "reason": "owner_block",
	}), 409, "block over booking")
	if c := check(slot[0], slot[1]); c.body["available"] != false || c.str("conflicts.0.kind") != "booked" {
		t.Fatalf("booked: %s", c.raw)
	}
}

func TestTimeZoneOfListing(t *testing.T) {
	s := newPricingSetup(t, nil)
	s.rules(t, map[string]any{"fuel_policy": "customer", "timezone": "Asia/Dubai"}, 200)
	day := time.Now().Add(96 * time.Hour)
	r := s.estimate(t, map[string]any{"dates": map[string]any{"start_date": day.Format("2006-01-02"), "end_date": day.Format("2006-01-02"),
		"start_time": "07:00", "end_time": "19:00"}, "pricing_unit": "auto", "delivery": false, "pickup": false}, 200)
	if !strings.HasSuffix(r.str("period.start_local"), "T07:00:00+04:00") || r.num("period.hours") != 12 || r.str("period.timezone") != "Asia/Dubai" {
		t.Fatalf("Dubai dates: %s", r.raw)
	}
	start, _ := time.Parse(time.RFC3339, r.str("period.start"))
	if start.Hour() != 3 || start.Location() != time.UTC {
		t.Fatalf("07:00 Dubai should be 03:00 UTC, got %v", start)
	}
	// 12 hours: a day (₹12,000) is cheaper than 12 × ₹1,500.
	if r.str("lines.0.unit") != "day" {
		t.Fatalf("lines: %v", r.list("lines"))
	}
}

func TestConfigurableTaxesByDate(t *testing.T) {
	s := newPricingSetup(t, nil)
	admin := s.e.login(randomPhone())
	s.e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_finance')`, admin.id)
	must(t, s.customer.do(http.MethodGet, "/admin/tax-rates", nil), 403, "customer")
	list := must(t, admin.do(http.MethodGet, "/admin/tax-rates", nil), 200, "taxes")
	if list.str("data.0.code") != "gst" || list.num("data.0.rate_bps") != 1800 {
		t.Fatalf("seeded GST: %s", list.raw)
	}
	switchDay := time.Now().Add(20 * 24 * time.Hour)
	must(t, admin.do(http.MethodPut, "/admin/tax-rates", map[string]any{
		"code": "infra_cess", "name": "Infrastructure cess", "rate_bps": 100, "applies_to": []string{"rental"},
		"valid_from": istDate(switchDay),
	}), 403, "admins cannot change tax rates (read-only)")
}

func TestFinalChargesSettlement(t *testing.T) {
	s := newPricingSetup(t, nil)
	s.rules(t, map[string]any{"fuel_policy": "owner", "fuel_rate_per_hour_paise": 90000, "late_fee_per_hour_paise": 250000,
		"late_grace_minutes": 30, "security_deposit_paise": 3000000}, 200)
	day := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Hour)
	bk := must(t, s.customer.do(http.MethodPost, "/bookings", map[string]any{
		"equipment_id": s.eq, "site_address_id": s.siteID, "period": map[string]any{"start": day, "end": day.Add(72 * time.Hour)},
		"pricing_unit": "day", "with_operator": true, "expected_hours": 24, "payment_mode": "offline",
	}, "Idempotency-Key", fmt.Sprintf("fin-%d", rand.Int64())), 201, "book")
	id := bk.str("id")
	if bk.str("price.kind") != "estimate" || bk.body["final_charges"] != nil || bk.body["delivery"] != true {
		t.Fatalf("booking: %s", bk.raw)
	}

	body := map[string]any{"actual_start": day, "actual_end": day.Add(72 * time.Hour), "operating_hours": 24}
	must(t, s.owner.do(http.MethodPost, "/owner/bookings/"+id+"/final-charges?dry_run=true", body), 409, "before the rental")

	// Move the rental into the past and run it (the trigger checks every step).
	for _, st := range []string{"accepted", "confirmed", "dispatched", "on_site", "active", "return_initiated", "returned"} {
		s.e.exec(`UPDATE bookings SET status = $2 WHERE id = $1`, id, st)
	}
	past := time.Now().Add(-100 * time.Hour).UTC().Truncate(time.Hour)
	s.e.exec(`UPDATE bookings SET period = tstzrange($2, $3) WHERE id = $1`, id, past, past.Add(72*time.Hour))
	body = map[string]any{"actual_start": past, "actual_end": past.Add(72*time.Hour + 2*time.Hour + 5*time.Minute),
		"operating_hours": 30}
	withDamage := map[string]any{"actual_start": past, "actual_end": past.Add(72*time.Hour + 2*time.Hour + 5*time.Minute), "operating_hours": 30, "damage_paise": 1500000, "damage_note": "Broken mirror"}
	must(t, s.owner.do(http.MethodPost, "/owner/bookings/"+id+"/final-charges?dry_run=true", withDamage), 422, "damage needs an admin decision")

	must(t, s.customer.do(http.MethodPost, "/owner/bookings/"+id+"/final-charges?dry_run=true", body), 403, "customer cannot settle")
	preview := must(t, s.owner.do(http.MethodPost, "/owner/bookings/"+id+"/final-charges?dry_run=true", body), 200, "preview")
	if preview.str("kind") != "final" || preview.num("late_hours") != 3 || preview.num("extra_hours") != 6 || preview.body["saved"] != false {
		t.Fatalf("preview: %s", preview.raw)
	}
	byComp := map[string]float64{}
	for _, c := range preview.list("charges") {
		m := c.(map[string]any)
		byComp[m["component"].(string)] += m["paise"].(float64)
	}
	if byComp["late_return"] != 3*250000 || byComp["fuel"] != 30*90000 || byComp["extra_hours"] != 6*180000 {
		t.Fatalf("components: %v", byComp)
	}
	if sumCharges(preview) != preview.num("final_total_paise") ||
		preview.num("difference_paise") != preview.num("final_total_paise")-bk.num("estimated_total_paise") || preview.num("deposit_paise") != 3000000 {
		t.Fatalf("totals: %s", preview.raw)
	}
	if g := must(t, s.customer.do(http.MethodGet, "/bookings/"+id, nil), 200, "after preview"); g.body["final_charges"] != nil {
		t.Fatal("a dry run saved")
	}

	saved := must(t, s.owner.do(http.MethodPost, "/owner/bookings/"+id+"/final-charges", body), 200, "settle")
	if saved.body["saved"] != true || saved.num("final_total_paise") != preview.num("final_total_paise") {
		t.Fatalf("saved: %s", saved.raw)
	}
	must(t, s.owner.do(http.MethodPost, "/owner/bookings/"+id+"/final-charges", body), 409, "settle twice")

	// The customer sees the estimate and the final charges side by side.
	g := must(t, s.customer.do(http.MethodGet, "/bookings/"+id, nil), 200, "booking")
	if g.str("price.kind") != "estimate" || g.str("final_charges.kind") != "final" || g.num("final_total_paise") != saved.num("final_total_paise") ||
		g.num("estimated_total_paise") != bk.num("estimated_total_paise") || g.str("finalized_at") == "" {
		t.Fatalf("booking view: %s", g.raw)
	}
	// Changing the listing's prices afterwards does not touch the booking.
	must(t, s.owner.do(http.MethodPut, "/owner/equipment/"+s.eq+"/pricing", map[string]any{
		"plans": []map[string]any{{"unit": "day", "with_operator": true, "rate_paise": 9900000}},
	}), 200, "reprice")
	if again := must(t, s.customer.do(http.MethodGet, "/bookings/"+id, nil), 200, "booking"); again.num("final_total_paise") != g.num("final_total_paise") ||
		again.num("price.rental_paise") != g.num("price.rental_paise") {
		t.Fatal("stored charges changed with the listing")
	}
}

func TestBookingRecalculatesPriceServerSide(t *testing.T) {
	s := newPricingSetup(t, nil)
	day := time.Now().Add(72 * time.Hour)
	// Unknown fields such as a client-sent total are ignored: the server prices.
	bk := must(t, s.customer.do(http.MethodPost, "/bookings", map[string]any{
		"equipment_id": s.eq, "site_address_id": s.siteID, "period": map[string]any{"start": day, "end": day.Add(24 * time.Hour)},
		"pricing_unit": "auto", "with_operator": true, "estimated_total_paise": 1, "rate_paise": 1, "delivery": true, "pickup": false,
	}, "Idempotency-Key", fmt.Sprintf("srv-%d", rand.Int64())), 201, "book")
	est := s.estimate(t, map[string]any{"site": s.site, "period": map[string]any{"start": day, "end": day.Add(24 * time.Hour)},
		"pricing_unit": "auto", "with_operator": true, "pickup": false}, 200)
	if bk.num("estimated_total_paise") != est.num("estimated_total_paise") || bk.num("estimated_total_paise") < 1000000 ||
		bk.str("pricing_unit") != "day" || bk.str("price.unit") != "auto" || bk.body["pickup"] != false {
		t.Fatalf("server price %v vs estimate %v: %s", bk.num("estimated_total_paise"), est.num("estimated_total_paise"), bk.raw)
	}
	var stored int64
	if err := s.e.pool.QueryRow(t.Context(), `SELECT estimated_total_paise FROM bookings WHERE id = $1`, bk.str("id")).Scan(&stored); err != nil ||
		float64(stored) != est.num("estimated_total_paise") {
		t.Fatalf("stored %d (%v)", stored, err)
	}
}

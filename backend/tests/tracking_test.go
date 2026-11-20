package tests

// Location and tracking through the real router and database: consented
// tracking sessions that stop with the booking, phone vs tracker sources,
// who may see locations (and the access log), history, ETA, hardware
// trackers and the live WebSocket stream.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// dispatchedJob is a wet-hire rental whose operator has accepted and left
// for the site.
func dispatchedJob(t *testing.T, e *env) *operatorRental {
	t.Helper()
	o := newOperatorRental(t, e)
	must(t, o.owner.do(http.MethodPost, "/owner/bookings/"+o.id+"/assign-operator", map[string]any{"operator_user_id": o.op1.id}), 200, "assign")
	must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/accept", nil), 200, "accept")
	o.op1.checklist(t, o.id, "handover", 2150)
	must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/dispatch", nil), 200, "dispatch")
	return o
}

func (o *operatorRental) startSharing(t *testing.T) resp {
	t.Helper()
	return must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/tracking/session", map[string]any{
		"consent": true, "policy_version": o.e.cfg.Tracking.PolicyVersion, "background": true, "platform": "android",
	}), 200, "start sharing")
}

func (o *operatorRental) upload(u *user, points ...map[string]any) resp {
	return u.do(http.MethodPost, "/operator/jobs/"+o.id+"/tracking/batch", map[string]any{"points": points})
}

func fix(lat, lng float64, ago time.Duration) map[string]any {
	return map[string]any{"lat": lat, "lng": lng, "recorded_at": time.Now().Add(-ago).UTC(), "accuracy_m": 8, "speed_mps": 11}
}

func TestTrackingSessionsConsentAndAutoStop(t *testing.T) {
	e := newEnv(t)
	o := dispatchedJob(t, e)

	// The notice is public; sharing needs consent to its current version.
	pol := must(t, e.call(http.MethodGet, "/tracking/policy", "", "", nil, nil), 200, "policy")
	if pol.str("version") != e.cfg.Tracking.PolicyVersion || len(pol.list("points")) < 5 {
		t.Fatalf("policy = %s", pol.raw)
	}
	if r := o.upload(o.op1, fix(18.53, 73.84, time.Minute)); r.code != 403 || !strings.Contains(r.raw, "TRACKING_CONSENT_REQUIRED") {
		t.Fatalf("upload without consent = %d %s", r.code, r.raw)
	}
	must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/tracking/session", map[string]any{"consent": false, "policy_version": e.cfg.Tracking.PolicyVersion}), 400, "no consent")
	must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/tracking/session", map[string]any{"consent": true, "policy_version": "old"}), 409, "outdated notice")
	must(t, o.customer.do(http.MethodPost, "/bookings/"+o.id+"/tracking/session", map[string]any{"consent": true, "policy_version": e.cfg.Tracking.PolicyVersion}), 403, "customer cannot share for the machine")
	must(t, o.op2.do(http.MethodPost, "/operator/jobs/"+o.id+"/tracking/session", map[string]any{"consent": true, "policy_version": e.cfg.Tracking.PolicyVersion}), 404, "another operator")

	sess := o.startSharing(t)
	if sess.body["active"] != true || sess.str("kind") != "mobilisation" || sess.num("interval_seconds") <= 0 {
		t.Fatalf("session = %s", sess.raw)
	}
	second := fix(18.5500, 73.8000, 2*time.Minute)
	up := must(t, o.upload(o.op1, fix(18.5300, 73.8400, 3*time.Minute), second), 200, "upload")
	if up.num("accepted") != 2 {
		t.Fatalf("upload = %s", up.raw)
	}
	if r := must(t, o.upload(o.op1, second), 200, "offline replay"); r.num("duplicates") != 1 {
		t.Fatalf("replay = %s", r.raw)
	}
	// Simulated points are accepted in development and marked as such.
	sim := map[string]any{"points": []map[string]any{fix(18.5600, 73.7900, time.Minute)}, "simulated": true}
	must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/tracking/batch", sim), 200, "simulated upload")

	// The customer sees the machine on its way: positions, source, route, ETA.
	view := must(t, o.customer.do(http.MethodGet, "/bookings/"+o.id+"/track", nil), 200, "track")
	if view.body["hidden"] != false || view.str("equipment_position.source") != "simulated" || view.str("operator_position.source") != "simulated" ||
		len(view.list("route")) != 3 || view.str("eta.method") != "straight_line" || view.num("eta.duration_s") <= 0 ||
		view.str("destination.lat") == "" || view.body["session"].(map[string]any)["active"] != true {
		t.Fatalf("track view = %s", view.raw)
	}
	if !strings.Contains(view.str("source_note"), "Simulated") {
		t.Fatalf("source note = %s", view.str("source_note"))
	}
	// History of the session (thinned, timestamped).
	tripID := view.str("trips.0.id")
	hist := must(t, o.owner.do(http.MethodGet, "/bookings/"+o.id+"/track/history?trip_id="+tripID, nil), 200, "history")
	if len(hist.list("points")) != 3 || hist.str("points.0.recorded_at") == "" {
		t.Fatalf("history = %s", hist.raw)
	}

	// Stopping: consent withdrawn, session ended, uploads refused.
	stop := must(t, o.op1.do(http.MethodDelete, "/operator/jobs/"+o.id+"/tracking/session", nil), 200, "stop")
	if stop.body["active"] != false || stop.str("end_reason") != "stopped_by_user" {
		t.Fatalf("stop = %s", stop.raw)
	}
	must(t, o.upload(o.op1, fix(18.57, 73.78, 30*time.Second)), 403, "upload after stopping")
	o.startSharing(t) // a new session can start again
	must(t, o.upload(o.op1, fix(18.5800, 73.7700, 10*time.Second)), 200, "second session")

	// The booking moves on to the site, the rental, the return: sharing
	// stops by itself when the machine is handed back.
	o.e.exec(`UPDATE bookings SET status = 'on_site' WHERE id = $1`, o.id)
	o.e.exec(`UPDATE bookings SET status = 'active' WHERE id = $1`, o.id)
	o.e.exec(`UPDATE bookings SET status = 'return_initiated' WHERE id = $1`, o.id)
	o.e.exec(`UPDATE bookings SET status = 'returned' WHERE id = $1`, o.id)
	after := must(t, o.owner.do(http.MethodGet, "/bookings/"+o.id+"/track", nil), 200, "owner after return")
	if after.body["session"].(map[string]any)["active"] != false {
		t.Fatalf("session still open after return: %s", after.raw)
	}
	reasons := map[string]bool{}
	for _, tr := range after.list("trips") {
		if r, ok := tr.(map[string]any)["end_reason"].(string); ok {
			reasons[r] = true
		}
	}
	if !reasons["stopped_by_user"] || !reasons["returned"] {
		t.Fatalf("end reasons = %v", reasons)
	}
	var active int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM tracking_consents WHERE booking_id = $1 AND withdrawn_at IS NULL`, o.id).Scan(&active)
	if active != 0 {
		t.Fatal("consents must lapse when the booking leaves the tracked stages")
	}
	must(t, o.upload(o.op1, fix(18.59, 73.74, 5*time.Second)), 409, "upload after the return")
	must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/tracking/session", map[string]any{"consent": true, "policy_version": e.cfg.Tracking.PolicyVersion}), 409, "new session after the return")

	// Customers no longer see the machine; the owner side still does.
	cust := must(t, o.customer.do(http.MethodGet, "/bookings/"+o.id+"/track", nil), 200, "customer after return")
	if cust.body["hidden"] != true || cust.body["equipment_position"] != nil || cust.body["route"] == nil {
		t.Fatalf("customer view after return = %s", cust.raw)
	}
	must(t, o.customer.do(http.MethodGet, "/bookings/"+o.id+"/track/history?trip_id="+tripID, nil), 403, "customer history after return")
	must(t, o.customer.do(http.MethodPost, "/bookings/"+o.id+"/track/ticket", nil), 403, "customer stream after return")
	if after.body["equipment_position"] == nil {
		t.Fatal("the owner keeps the machine's last known location")
	}
}

func TestLocationAuthorizationAndAudit(t *testing.T) {
	e := newEnv(t)
	o := dispatchedJob(t, e)
	o.startSharing(t)
	must(t, o.upload(o.op1, fix(18.53, 73.84, time.Minute)), 200, "upload")

	outsider := e.registerAndVerify("customer", "")
	fm := e.addMember(t, o.owner, "fleet_manager", "Fleet Manager Elsewhere")
	view := must(t, o.owner.do(http.MethodGet, "/bookings/"+o.id+"/track", nil), 200, "owner view")
	tripID := view.str("trips.0.id")
	for _, u := range []*user{outsider, fm} {
		must(t, u.do(http.MethodGet, "/bookings/"+o.id+"/track", nil), 404, "track")
		must(t, u.do(http.MethodGet, "/bookings/"+o.id+"/track/history?trip_id="+tripID, nil), 404, "history")
		must(t, u.do(http.MethodPost, "/bookings/"+o.id+"/track/ticket", nil), 404, "ticket")
		must(t, u.do(http.MethodPost, "/operator/jobs/"+o.id+"/tracking/batch", map[string]any{"points": []map[string]any{fix(1, 1, time.Second)}}), 404, "upload")
	}
	must(t, o.owner.do(http.MethodGet, "/bookings/"+o.id+"/track/history?trip_id="+outsider.id, nil), 404, "a trip of another booking")
	// A fleet manager whose fleet includes the machine may watch it.
	fl := must(t, o.owner.do(http.MethodPost, "/owner/fleets", map[string]any{"name": "Tracking fleet " + randomLetters(4)}), 201, "fleet").str("id")
	must(t, o.owner.do(http.MethodPost, "/owner/fleets/"+fl+"/equipment", map[string]any{"equipment_ids": []string{o.eq}}), 200, "machine")
	must(t, o.owner.do(http.MethodPost, "/owner/fleets/"+fl+"/managers", map[string]any{"user_id": fm.id}), 200, "manager")
	must(t, fm.do(http.MethodGet, "/bookings/"+o.id+"/track", nil), 200, "fleet manager in scope")

	// Every view is in the location access log.
	must(t, o.customer.do(http.MethodGet, "/bookings/"+o.id+"/track", nil), 200, "customer view")
	must(t, o.customer.do(http.MethodGet, "/bookings/"+o.id+"/track", nil), 200, "customer view again")
	support := e.supportAdmin()
	log := must(t, support.do(http.MethodGet, "/admin/location-access?subject_id="+o.id, nil), 200, "access log")
	seen := map[string]float64{}
	for _, row := range log.list("data") {
		m := row.(map[string]any)
		seen[m["user_id"].(string)+":"+m["access_kind"].(string)] += m["count"].(float64)
	}
	if seen[o.customer.id+":live"] != 2 || seen[o.owner.id+":history"] != 0 && seen[o.owner.id+":live"] < 1 || seen[fm.id+":live"] != 1 {
		t.Fatalf("access log = %v\n%s", seen, log.raw)
	}
	must(t, o.owner.do(http.MethodGet, "/admin/location-access", nil), 403, "owners do not read the access log")
}

func TestTrackingIdleSessionsClose(t *testing.T) {
	e := newEnv(t)
	o := dispatchedJob(t, e)
	o.startSharing(t)
	must(t, o.upload(o.op1, fix(18.53, 73.84, time.Minute)), 200, "upload")
	// Nothing heard for longer than the idle timeout.
	e.exec(`UPDATE trips SET started_at = now() - interval '5 hours' WHERE booking_id = $1`, o.id)
	e.exec(`UPDATE location_pings SET received_at = now() - interval '4 hours' WHERE trip_id IN (SELECT id FROM trips WHERE booking_id = $1)`, o.id)
	e.runJob(t, "tracking.close_idle_sessions")
	view := must(t, o.owner.do(http.MethodGet, "/bookings/"+o.id+"/track", nil), 200, "view")
	if view.body["session"].(map[string]any)["active"] != false || view.str("trips.0.end_reason") != "idle_timeout" {
		t.Fatalf("idle session = %s", view.raw)
	}
	must(t, o.upload(o.op1, fix(18.54, 73.83, time.Second)), 409, "no session to upload into")
}

func TestHardwareTrackers(t *testing.T) {
	e := newEnv(t)
	o := dispatchedJob(t, e)
	fm := e.addMember(t, o.owner, "fleet_manager", "Fleet Manager No Scope")
	must(t, fm.do(http.MethodPost, "/owner/equipment/"+o.eq+"/trackers", map[string]any{"provider": "generic", "external_id": "IMEI-X"}), 404, "out-of-scope fleet manager")
	must(t, o.owner.do(http.MethodPost, "/owner/equipment/"+o.eq+"/trackers", map[string]any{"provider": "acme", "external_id": "IMEI-X"}), 400, "unknown provider")
	imei := "IMEI-" + randomLetters(8)
	reg := must(t, o.owner.do(http.MethodPost, "/owner/equipment/"+o.eq+"/trackers", map[string]any{"provider": "generic", "external_id": imei, "label": "Teltonika FMB920"}), 201, "register")
	token := reg.str("token")
	if !strings.HasPrefix(token, "trk_") || reg.str("device.status") != "active" {
		t.Fatalf("register = %s", reg.raw)
	}
	must(t, o.owner.do(http.MethodPost, "/owner/equipment/"+o.eq+"/trackers", map[string]any{"provider": "generic", "external_id": "IMEI-other"}), 409, "second active tracker")
	if list := must(t, o.owner.do(http.MethodGet, "/owner/equipment/"+o.eq+"/trackers", nil), 200, "list"); strings.Contains(list.raw, token) {
		t.Fatal("tokens are never listed")
	}

	post := func(provider, tok, body string) resp {
		return e.call(http.MethodPost, "/telematics/"+provider+"/positions", "", "", json.RawMessage(body), map[string]string{"Authorization": "Bearer " + tok})
	}
	payload := func(device string, lat float64) string {
		return `{"device_id":"` + device + `","positions":[{"lat":` + strings.TrimRight(strings.TrimRight(jsonFloat(lat), "0"), ".") + `,"lng":73.80,"recorded_at":"` + time.Now().Add(-30*time.Second).UTC().Format(time.RFC3339) + `"}]}`
	}
	must(t, post("generic", "trk_wrong", payload(imei, 18.55)), 401, "bad token")
	must(t, post("simulated", token, payload(imei, 18.55)), 401, "token of another provider")
	must(t, post("generic", token, payload("IMEI-someone-else", 18.55)), 403, "payload for another device")
	must(t, post("generic", token, `{"device_id":"`+imei+`","positions":[]}`), 400, "empty payload")
	got := must(t, post("generic", token, payload(imei, 18.55)), 200, "tracker positions")
	// Dispatch opened the delivery trip: the machine's own tracker joins it
	// (no personal consent is involved; the operator's phone still needs it).
	if got.num("accepted") != 1 || got.body["in_trip"] != true {
		t.Fatalf("tracker during delivery = %s", got.raw)
	}
	if r := o.upload(o.op1, fix(18.40, 73.90, 20*time.Second)); r.code != 403 {
		t.Fatalf("phone upload without consent = %d", r.code)
	}
	// The machine's tracker outranks the operator's phone.
	o.startSharing(t)
	must(t, post("generic", token, payload(imei, 18.56)), 200, "tracker in session")
	must(t, o.upload(o.op1, fix(18.40, 73.90, 10*time.Second)), 200, "phone upload")
	view := must(t, o.customer.do(http.MethodGet, "/bookings/"+o.id+"/track", nil), 200, "view")
	if view.str("equipment_position.source") != "telematics" || view.str("operator_position.source") != "app" ||
		!strings.Contains(view.str("source_note"), "tracker fitted to the machine") {
		t.Fatalf("view with tracker = %s", view.raw)
	}
	// Revoked trackers are refused.
	must(t, o.owner.do(http.MethodDelete, "/owner/equipment/"+o.eq+"/trackers/"+reg.str("device.id"), nil), 204, "revoke")
	must(t, post("generic", token, payload(imei, 18.57)), 401, "revoked token")
}

func jsonFloat(f float64) string { b, _ := json.Marshal(f); return string(b) + "0" }

func TestLiveLocationStream(t *testing.T) {
	e := newEnv(t)
	o := dispatchedJob(t, e)
	o.startSharing(t)
	must(t, o.upload(o.op1, fix(18.53, 73.84, time.Minute)), 200, "first fix")
	srv := httptest.NewServer(e.router)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws/track?ticket="
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Bad tickets never upgrade.
	if _, resp, err := websocket.Dial(ctx, wsURL+"forged", nil); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged ticket: %v %v", resp, err)
	}
	ticket := must(t, o.customer.do(http.MethodPost, "/bookings/"+o.id+"/track/ticket", nil), 200, "ticket")
	conn, _, err := websocket.Dial(ctx, wsURL+ticket.str("ticket"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	read := func() map[string]any {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		return m
	}
	if m := read(); m["type"] != "snapshot" || m["lat"].(float64) != 18.53 {
		t.Fatalf("snapshot = %v", m)
	}
	must(t, o.upload(o.op1, fix(18.5555, 73.8111, time.Second)), 200, "next fix")
	if m := read(); m["type"] != "position" || m["lat"].(float64) != 18.5555 || m["source"] != "app" || m["booking_id"] != o.id {
		t.Fatalf("live update = %v", m)
	}
	// The operator stops sharing: the stream says so and closes.
	must(t, o.op1.do(http.MethodDelete, "/operator/jobs/"+o.id+"/tracking/session", nil), 200, "stop")
	if m := read(); m["type"] != "session_ended" {
		t.Fatalf("end = %v", m)
	}
	if _, _, err := conn.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("stream should close normally: %v", err)
	}

	// Fleet stream: owners get their machines; an unscoped fleet manager none.
	ft := must(t, o.owner.do(http.MethodPost, "/fleet/"+o.owner.org+"/live/ticket", nil), 200, "fleet ticket")
	fc, _, err := websocket.Dial(ctx, wsURL+ft.str("ticket"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fc.CloseNow()
	if _, data, err := fc.Read(ctx); err != nil || !strings.Contains(string(data), o.eq) {
		t.Fatalf("fleet snapshot: %s %v", data, err)
	}
	if e.app.Tracking.Hub().Subscribers(uuidOf(t, o.eq)) < 1 {
		t.Fatal("owner fleet stream subscribes the machine")
	}
	fm := e.addMember(t, o.owner, "fleet_manager", "Fleet Manager Stream")
	before := e.app.Tracking.Hub().Subscribers(uuidOf(t, o.eq))
	fmTicket := must(t, fm.do(http.MethodPost, "/fleet/"+o.owner.org+"/live/ticket", nil), 200, "fm ticket")
	fmc, _, err := websocket.Dial(ctx, wsURL+fmTicket.str("ticket"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fmc.CloseNow()
	time.Sleep(100 * time.Millisecond)
	if e.app.Tracking.Hub().Subscribers(uuidOf(t, o.eq)) != before {
		t.Fatal("a fleet manager without the machine must not subscribe to it")
	}
	must(t, o.customer.do(http.MethodPost, "/fleet/"+o.owner.org+"/live/ticket", nil), 404, "customer fleet ticket")
}

func uuidOf(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

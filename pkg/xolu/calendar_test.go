package xolu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// calFake serves xolu's /api/v2/cal routes with the shapes of xolu v0.30.44
// and records every request, so a test can tell what molu sent and what it
// did not send. The calendars "free-1" and "free-2" are free, "busy" is
// occupied, and any other calendar does not exist.
type calFake struct {
	mu   sync.Mutex
	reqs []calReq
}

type calReq struct {
	path string
	body map[string]any
}

func (f *calFake) client(t *testing.T) *HTTPXoluClient {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.reqs = append(f.reqs, calReq{path: r.URL.Path, body: body})
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		id, _ := body["calendar_id"].(string)
		if id != "free-1" && id != "free-2" && id != "busy" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"XOLU-CAL004","message":"calendar not found: ` + id + `","status":404}}`))
			return
		}
		switch r.URL.Path {
		case "/api/v2/cal/check":
			if id == "busy" {
				_ = json.NewEncoder(w).Encode(map[string]any{"feasible": false, "nearest_openings": []any{
					map[string]any{"start": "2026-10-27T11:00:00Z", "end": "2026-10-27T12:00:00Z"}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"feasible": true, "nearest_openings": []any{}})
		case "/api/v2/cal/openings":
			_ = json.NewEncoder(w).Encode(map[string]any{"objective": "earliest", "openings": []any{
				map[string]any{"start": "2026-10-27T08:00:00Z", "end": "2026-10-27T10:00:00Z", "margin_ms": 7200000}}})
		case "/api/v2/cal/propose":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"booking_id": body["booking_id"], "calendar_id": id, "state": "proposed"})
		case "/api/v2/cal/confirm":
			_ = json.NewEncoder(w).Encode(map[string]any{"booking_id": body["booking_id"], "calendar_id": id, "state": "binding"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return NewHTTPXoluClient(ts.URL, 2*time.Second)
}

func (f *calFake) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.reqs {
		out = append(out, r.path+"?"+asString(r.body["calendar_id"]))
	}
	return out
}

func (f *calFake) last() calReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func exec(t *testing.T, c *HTTPXoluClient, primitive string, args map[string]any) (map[string]any, error) {
	t.Helper()
	return c.Execute(context.Background(), primitive, args)
}

// requireViolation checks that err is a CONTRACT_VIOLATION naming field.
func requireViolation(t *testing.T, err error, field string) {
	t.Helper()
	var te *taxonomy.TypedError
	if !errors.As(err, &te) || !strings.Contains(te.Code, string(taxonomy.CodeContractViolation)) {
		t.Fatalf("expected CONTRACT_VIOLATION, got %v", err)
	}
	fields, _ := te.Details["fields"].([]string)
	for _, f := range fields {
		if f == field {
			return
		}
	}
	t.Errorf("expected the violation to name %q, got %v", field, te.Details)
}

func TestCalConfirm_NeedsTheCalendarAndNeverGuessesOne(t *testing.T) {
	f := &calFake{}
	_, err := exec(t, f.client(t), "cal_confirm", map[string]any{"booking_id": "bk-1"})
	requireViolation(t, err, "calendar_id")
	if len(f.reqs) != 0 {
		t.Errorf("nothing may be sent to xolu without a calendar, got %v", f.paths())
	}
}

func TestCalConfirm_SendsTheGivenCalendarAndBooking(t *testing.T) {
	f := &calFake{}
	res, err := exec(t, f.client(t), "cal_confirm", map[string]any{"calendar_id": "free-2", "booking_id": float64(77)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := f.last().body; got["calendar_id"] != "free-2" || got["booking_id"] != "77" {
		t.Errorf("unexpected body %v", got)
	}
	if res["state"] != "binding" {
		t.Errorf("expected the confirmed booking, got %v", res)
	}
}

func TestCalConfirm_AcceptsASingleCalendarInCalendars(t *testing.T) {
	f := &calFake{}
	c := f.client(t)
	if _, err := exec(t, c, "cal_confirm", map[string]any{"calendars": []any{"free-1"}, "booking_id": "bk-1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.last().body["calendar_id"] != "free-1" {
		t.Errorf("unexpected body %v", f.last().body)
	}
	_, err := exec(t, c, "cal_confirm", map[string]any{"calendars": []any{"free-1", "free-2"}, "booking_id": "bk-1"})
	requireViolation(t, err, "calendars")
}

func TestCalOpenings_DurationIsISO8601(t *testing.T) {
	cases := map[string]float64{
		"PT30M":   30 * 60 * 1000,
		"PT15M":   15 * 60 * 1000,
		"PT1H":    60 * 60 * 1000,
		"PT1H30M": 90 * 60 * 1000,
		"PT45S":   45 * 1000,
		"P1D":     24 * 60 * 60 * 1000,
		"P1DT2H":  26 * 60 * 60 * 1000,
		"30m":     30 * 60 * 1000, // a Go duration was accepted before; keep it
	}
	for in, want := range cases {
		f := &calFake{}
		_, err := exec(t, f.client(t), "cal_openings", map[string]any{
			"calendars": []any{"free-1"}, "duration": in,
			"from": "2026-10-27T08:00:00Z", "to": "2026-10-27T18:00:00Z"})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", in, err)
		}
		if got := f.last().body["duration_ms"]; got != want {
			t.Errorf("%s: expected duration_ms %v, got %v", in, want, got)
		}
	}
}

func TestCalOpenings_AnUnreadableOrMissingDurationIsAViolationNotOneHour(t *testing.T) {
	for _, args := range []map[string]any{
		{"duration": "banana"},
		{"duration": "PT"},
		{"duration": "P"},
		{}, // no duration at all
	} {
		f := &calFake{}
		args["calendars"] = []any{"free-1"}
		args["from"], args["to"] = "2026-10-27T08:00:00Z", "2026-10-27T18:00:00Z"
		_, err := exec(t, f.client(t), "cal_openings", args)
		requireViolation(t, err, "duration")
		if len(f.reqs) != 0 {
			t.Errorf("%v: nothing may be sent to xolu, got %v", args, f.paths())
		}
	}
}

func TestCalOpeningsAndPropose_RefuseSeveralCalendarsInsteadOfIgnoringThem(t *testing.T) {
	f := &calFake{}
	c := f.client(t)
	cals := []any{"free-1", "free-2"}
	_, err := exec(t, c, "cal_openings", map[string]any{"calendars": cals, "duration": "PT1H",
		"from": "2026-10-27T08:00:00Z", "to": "2026-10-27T18:00:00Z"})
	requireViolation(t, err, "calendars")
	_, err = exec(t, c, "cal_propose", map[string]any{"calendars": cals,
		"start": "2026-10-27T10:00:00Z", "end": "2026-10-27T11:00:00Z"})
	requireViolation(t, err, "calendars")
	if len(f.reqs) != 0 {
		t.Errorf("nothing may be sent to xolu, got %v", f.paths())
	}
}

func TestCalPropose_UsesTheGivenCalendarAndSpan(t *testing.T) {
	f := &calFake{}
	res, err := exec(t, f.client(t), "cal_propose", map[string]any{"calendars": []any{"free-2"},
		"start": "2026-10-27T10:00:00Z", "end": "2026-10-27T11:00:00Z", "booking_id": "bk-9"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := f.last().body
	span, _ := body["span"].(map[string]any)
	if body["calendar_id"] != "free-2" || body["booking_id"] != "bk-9" || span["start"] != "2026-10-27T10:00:00Z" {
		t.Errorf("unexpected body %v", body)
	}
	if res["state"] != "proposed" {
		t.Errorf("expected the proposed booking, got %v", res)
	}
}

func TestCalCheck_OneCalendarReturnsXolusAnswer(t *testing.T) {
	f := &calFake{}
	res, err := exec(t, f.client(t), "cal_check", map[string]any{"calendars": []any{"busy"},
		"start": "2026-10-27T10:00:00Z", "end": "2026-10-27T11:00:00Z"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["feasible"] != false {
		t.Errorf("expected feasible false, got %v", res)
	}
	if _, ok := res["nearest_openings"]; !ok {
		t.Errorf("xolu's answer must be kept, got %v", res)
	}
}

func TestCalCheck_SeveralCalendarsAreIntersectedAndTheBlockersNamed(t *testing.T) {
	f := &calFake{}
	c := f.client(t)
	args := func(cals ...any) map[string]any {
		return map[string]any{"calendars": cals, "start": "2026-10-27T10:00:00Z", "end": "2026-10-27T11:00:00Z"}
	}

	res, err := exec(t, c, "cal_check", args("free-1", "free-2"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["feasible"] != true {
		t.Errorf("both calendars are free, got %v", res)
	}

	res, err = exec(t, c, "cal_check", args("free-1", "busy", "free-2"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["feasible"] != false {
		t.Errorf("one calendar is busy, the intersection is not feasible: %v", res)
	}
	blocking, _ := res["blocking_calendars"].([]string)
	if len(blocking) != 1 || blocking[0] != "busy" {
		t.Errorf("expected busy as the only blocker, got %v", res["blocking_calendars"])
	}
	if per, _ := res["calendars"].([]any); len(per) != 3 {
		t.Errorf("expected the answer of each of the 3 calendars, got %v", res["calendars"])
	}
}

func TestCalCheck_AnUnknownCalendarIsAHTTP404ForTheServerToMapToNotFound(t *testing.T) {
	f := &calFake{}
	_, err := exec(t, f.client(t), "cal_check", map[string]any{"calendars": []any{"free-1", "nope"},
		"start": "2026-10-27T10:00:00Z", "end": "2026-10-27T11:00:00Z"})
	var he *HTTPError
	if !errors.As(err, &he) || he.HTTPStatus() != http.StatusNotFound {
		t.Fatalf("expected a 404 HTTPError, got %v", err)
	}
}

func TestCal_MissingSpanIsAViolation(t *testing.T) {
	f := &calFake{}
	c := f.client(t)
	_, err := exec(t, c, "cal_check", map[string]any{"calendars": []any{"free-1"}})
	requireViolation(t, err, "start")
	_, err = exec(t, c, "cal_propose", map[string]any{"calendars": []any{"free-1"}, "start": "2026-10-27T10:00:00Z"})
	requireViolation(t, err, "end")
	_, err = exec(t, c, "cal_check", map[string]any{"start": "2026-10-27T10:00:00Z", "end": "2026-10-27T11:00:00Z"})
	requireViolation(t, err, "calendars")
}

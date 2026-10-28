package xolu

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// executeCalendar serves cal_check, cal_openings, cal_propose and cal_confirm
// against xolu's /api/v2/cal routes. xolu works on one calendar per request,
// and molu does not guess what the agent left out:
//
//   - A missing or unreadable argument is a CONTRACT_VIOLATION naming it,
//     without a request to xolu. In particular there is no default calendar,
//     and a duration that cannot be read is not turned into one hour.
//   - cal_check with several calendars asks each one and intersects the
//     answers, naming the calendars that block. cal_openings and cal_propose
//     act on one calendar only, so several are refused instead of the extra
//     ones being ignored.
//   - An unknown calendar is xolu's 404 (an *HTTPError), which the server maps
//     to NOT_FOUND.
func (c *HTTPXoluClient) executeCalendar(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
	switch primitive {
	case "cal_check":
		return c.calCheck(ctx, args)
	case "cal_openings":
		return c.calOpenings(ctx, args)
	case "cal_propose":
		return c.calPropose(ctx, args)
	default:
		return c.calConfirm(ctx, args)
	}
}

func (c *HTTPXoluClient) calCheck(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	calendars := calendarsArg(args)
	missing := missingArgs(args, "start", "end")
	if len(calendars) == 0 {
		missing = append([]string{"calendars"}, missing...)
	}
	if len(missing) > 0 {
		return nil, taxonomy.NewContractViolation("", "cal_check", missing)
	}

	answers := make([]map[string]interface{}, 0, len(calendars))
	for _, id := range calendars {
		var res map[string]interface{}
		body := map[string]interface{}{
			"calendar_id": id,
			"span":        map[string]interface{}{"start": args["start"], "end": args["end"]},
		}
		if _, err := c.doRequest(ctx, "POST", "/api/v2/cal/check", body, &res); err != nil {
			return nil, err
		}
		answers = append(answers, res)
	}
	if len(answers) == 1 {
		return answers[0], nil
	}

	blocking := []string{}
	per := make([]interface{}, 0, len(answers))
	for i, res := range answers {
		res["calendar_id"] = calendars[i]
		if res["feasible"] != true {
			blocking = append(blocking, calendars[i])
		}
		per = append(per, res)
	}
	return map[string]interface{}{
		"feasible":           len(blocking) == 0,
		"blocking_calendars": blocking,
		"calendars":          per,
	}, nil
}

func (c *HTTPXoluClient) calOpenings(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	calendar, bad := oneCalendar(args, "calendars")
	missing := missingArgs(args, "from", "to")
	if bad != "" {
		missing = append([]string{bad}, missing...)
	}
	durationMs, ok := durationArg(args)
	if !ok {
		missing = append(missing, "duration")
	}
	if len(missing) > 0 {
		return nil, taxonomy.NewContractViolation("", "cal_openings", missing)
	}

	body := map[string]interface{}{
		"calendar_id": calendar,
		"from":        args["from"],
		"to":          args["to"],
		"duration_ms": durationMs,
	}
	if obj, ok := args["objective"].(string); ok && obj != "" {
		body["objective"] = obj
	}
	var res map[string]interface{}
	_, err := c.doRequest(ctx, "POST", "/api/v2/cal/openings", body, &res)
	return res, err
}

func (c *HTTPXoluClient) calPropose(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	calendar, bad := oneCalendar(args, "calendars")
	missing := missingArgs(args, "start", "end")
	if bad != "" {
		missing = append([]string{bad}, missing...)
	}
	if len(missing) > 0 {
		return nil, taxonomy.NewContractViolation("", "cal_propose", missing)
	}

	bookingID := fmt.Sprintf("bk-%d", time.Now().UnixNano())
	if bid, ok := args["booking_id"]; ok && fmt.Sprint(bid) != "" {
		bookingID = fmt.Sprint(bid)
	}
	// xolu only lets a booking become binding when it has a non-zero bearer.
	bearer := uint64(1)
	if b, ok := args["bearer"].(float64); ok {
		bearer = uint64(b)
	} else if b, ok := args["bearer"].(int); ok {
		bearer = uint64(b)
	}
	body := map[string]interface{}{
		"calendar_id": calendar,
		"booking_id":  bookingID,
		"bearer":      bearer,
		"span":        map[string]interface{}{"start": args["start"], "end": args["end"]},
	}
	var res map[string]interface{}
	_, err := c.doRequest(ctx, "POST", "/api/v2/cal/propose", body, &res)
	return res, err
}

func (c *HTTPXoluClient) calConfirm(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	var missing []string
	calendar := ""
	if cid, ok := args["calendar_id"]; ok && fmt.Sprint(cid) != "" {
		calendar = fmt.Sprint(cid)
	} else if cals := calendarsArg(args); len(cals) == 1 {
		calendar = cals[0]
	} else if len(cals) > 1 {
		missing = append(missing, "calendars")
	} else {
		missing = append(missing, "calendar_id")
	}
	if bid, ok := args["booking_id"]; !ok || bid == nil || fmt.Sprint(bid) == "" {
		missing = append(missing, "booking_id")
	}
	if len(missing) > 0 {
		return nil, taxonomy.NewContractViolation("", "cal_confirm", missing)
	}

	body := map[string]interface{}{
		"calendar_id": calendar,
		"booking_id":  fmt.Sprint(args["booking_id"]),
	}
	var res map[string]interface{}
	_, err := c.doRequest(ctx, "POST", "/api/v2/cal/confirm", body, &res)
	return res, err
}

// calendarsArg reads the calendars the agent named: calendars[], or a single
// calendar_id.
func calendarsArg(args map[string]interface{}) []string {
	var out []string
	switch v := args["calendars"].(type) {
	case []interface{}:
		for _, item := range v {
			if s := fmt.Sprint(item); s != "" && item != nil {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
	}
	if len(out) == 0 {
		if cid, ok := args["calendar_id"]; ok && cid != nil && fmt.Sprint(cid) != "" {
			out = append(out, fmt.Sprint(cid))
		}
	}
	return out
}

// oneCalendar returns the only calendar named, or the name of the argument at
// fault when there are none or several.
func oneCalendar(args map[string]interface{}, field string) (calendar, bad string) {
	cals := calendarsArg(args)
	if len(cals) != 1 {
		return "", field
	}
	return cals[0], ""
}

// missingArgs lists the arguments that are absent or empty.
func missingArgs(args map[string]interface{}, names ...string) []string {
	var out []string
	for _, n := range names {
		if v, ok := args[n]; !ok || v == nil || fmt.Sprint(v) == "" {
			out = append(out, n)
		}
	}
	return out
}

var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(\d+(?:\.\d+)?S)?)?$`)

// durationArg reads the duration of cal_openings in milliseconds from an
// ISO 8601 duration (PT30M, PT1H30M, P1D, P1DT2H), a Go duration ("30m"), or
// duration_ms. It reports false when none is given or it cannot be read.
func durationArg(args map[string]interface{}) (int64, bool) {
	if s, ok := args["duration"].(string); ok && s != "" {
		if d, ok := parseISODuration(s); ok {
			return d.Milliseconds(), true
		}
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d.Milliseconds(), true
		}
		return 0, false
	}
	switch v := args["duration_ms"].(type) {
	case float64:
		return int64(v), v > 0
	case int:
		return int64(v), v > 0
	case int64:
		return v, v > 0
	}
	return 0, false
}

// parseISODuration reads the day and time parts of an ISO 8601 duration.
// Years, months and weeks are not fixed lengths and are not accepted.
func parseISODuration(s string) (time.Duration, bool) {
	m := isoDuration.FindStringSubmatch(s)
	if m == nil || (m[1] == "" && m[2] == "" && m[3] == "" && m[4] == "") {
		return 0, false
	}
	var d time.Duration
	units := []time.Duration{24 * time.Hour, time.Hour, time.Minute}
	for i, unit := range units {
		if m[i+1] != "" {
			n, err := strconv.Atoi(m[i+1])
			if err != nil {
				return 0, false
			}
			d += time.Duration(n) * unit
		}
	}
	if m[4] != "" {
		secs, err := strconv.ParseFloat(m[4][:len(m[4])-1], 64)
		if err != nil {
			return 0, false
		}
		d += time.Duration(secs * float64(time.Second))
	}
	return d, d > 0
}

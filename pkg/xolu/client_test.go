package xolu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

func TestHTTPXoluClient_Execute_Entities(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/asset/ast-1":
			json.NewEncoder(w).Encode(map[string]interface{}{"id": "ast-1", "name": "Pump"})
		case r.Method == "GET" && r.URL.Path == "/api/v1/asset":
			json.NewEncoder(w).Encode(map[string]interface{}{"items": []interface{}{map[string]interface{}{"id": "ast-1"}}})
		case r.Method == "POST" && r.URL.Path == "/api/v1/asset":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]interface{}{"id": "ast-2", "created": true})
		case r.Method == "PATCH" && r.URL.Path == "/api/v1/asset/ast-1":
			json.NewEncoder(w).Encode(map[string]interface{}{"id": "ast-1", "updated": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := NewHTTPXoluClient(ts.URL, 2*time.Second)
	ctx := context.Background()

	// 1. get
	res, err := client.Execute(ctx, "get", map[string]interface{}{"entity_type": "asset", "id": "ast-1"})
	if err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}
	if res["name"] != "Pump" {
		t.Errorf("expected Pump, got %v", res["name"])
	}

	// 2. list
	res, err = client.Execute(ctx, "list", map[string]interface{}{"entity_type": "asset"})
	if err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if _, ok := res["items"]; !ok {
		t.Errorf("expected items in list response")
	}

	// 3. create
	res, err = client.Execute(ctx, "create", map[string]interface{}{"entity_type": "asset", "data": map[string]interface{}{"name": "Valve"}})
	if err != nil {
		t.Fatalf("unexpected error on create: %v", err)
	}
	if res["created"] != true {
		t.Errorf("expected created true, got %v", res["created"])
	}

	// 4. update
	res, err = client.Execute(ctx, "update", map[string]interface{}{"entity_type": "asset", "id": "ast-1", "changes": map[string]interface{}{"name": "Pump Updated"}})
	if err != nil {
		t.Fatalf("unexpected error on update: %v", err)
	}
	if res["updated"] != true {
		t.Errorf("expected updated true, got %v", res["updated"])
	}
}

func TestHTTPXoluClient_Execute_FSMAndQuery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v2/fsm/machine/m-1/walk":
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "RUNNING", "output": map[string]interface{}{"ok": true}})
		case r.Method == "GET" && r.URL.Path == "/api/v2/fsm/machine/m-1/state":
			json.NewEncoder(w).Encode(map[string]interface{}{"state": "IDLE", "terminal": false})
		case r.Method == "GET" && r.URL.Path == "/api/v2/fsm/machine/m-1/history":
			json.NewEncoder(w).Encode(map[string]interface{}{"history": []interface{}{map[string]interface{}{"transition": "START"}}})
		case r.Method == "POST" && r.URL.Path == "/api/v1/oql/query":
			json.NewEncoder(w).Encode(map[string]interface{}{"rows": []interface{}{map[string]interface{}{"count": 1}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := NewHTTPXoluClient(ts.URL, 2*time.Second)
	ctx := context.Background()

	// 1. walk
	res, err := client.Execute(ctx, "walk", map[string]interface{}{"machine_id": "m-1", "input": "START"})
	if err != nil {
		t.Fatalf("unexpected error on walk: %v", err)
	}
	if res["status"] != "RUNNING" {
		t.Errorf("expected RUNNING, got %v", res["status"])
	}

	// 2. machine_state
	res, err = client.Execute(ctx, "machine_state", map[string]interface{}{"machine_id": "m-1"})
	if err != nil {
		t.Fatalf("unexpected error on machine_state: %v", err)
	}
	if res["state"] != "IDLE" {
		t.Errorf("expected IDLE, got %v", res["state"])
	}

	// 3. machine_history
	res, err = client.Execute(ctx, "machine_history", map[string]interface{}{"machine_id": "m-1"})
	if err != nil {
		t.Fatalf("unexpected error on machine_history: %v", err)
	}
	if _, ok := res["history"]; !ok {
		t.Errorf("expected history field in response")
	}

	// 4. query
	res, err = client.Execute(ctx, "query", map[string]interface{}{"language": "oql", "query": "SELECT * FROM asset"})
	if err != nil {
		t.Fatalf("unexpected error on query: %v", err)
	}
	if _, ok := res["rows"]; !ok {
		t.Errorf("expected rows field in response")
	}
}

func TestHTTPXoluClient_Execute_CalendarsAndDescribe(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/schemas":
			json.NewEncoder(w).Encode(map[string]interface{}{"entities": []interface{}{"asset"}})
		case r.Method == "POST" && r.URL.Path == "/api/v2/cal/check":
			json.NewEncoder(w).Encode(map[string]interface{}{"available": true})
		case r.Method == "POST" && r.URL.Path == "/api/v2/cal/openings":
			json.NewEncoder(w).Encode(map[string]interface{}{"openings": []interface{}{map[string]interface{}{"start": "2026-10-23T10:00:00Z"}}})
		case r.Method == "POST" && r.URL.Path == "/api/v2/cal/propose":
			json.NewEncoder(w).Encode(map[string]interface{}{"booking_id": "bk-456"})
		case r.Method == "POST" && r.URL.Path == "/api/v2/cal/confirm":
			json.NewEncoder(w).Encode(map[string]interface{}{"confirmed": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := NewHTTPXoluClient(ts.URL, 2*time.Second)
	ctx := context.Background()

	// 1. describe
	res, err := client.Execute(ctx, "describe", map[string]interface{}{"scope": "all"})
	if err != nil {
		t.Fatalf("unexpected error on describe: %v", err)
	}
	if _, ok := res["entities"]; !ok {
		t.Errorf("expected entities in describe response")
	}

	// 2. cal_check
	res, err = client.Execute(ctx, "cal_check", map[string]interface{}{"calendars": []interface{}{"c1"}, "start": "2026-10-23T10:00:00Z", "end": "2026-10-23T11:00:00Z"})
	if err != nil {
		t.Fatalf("unexpected error on cal_check: %v", err)
	}
	if res["available"] != true {
		t.Errorf("expected available true, got %v", res["available"])
	}

	// 3. cal_openings
	res, err = client.Execute(ctx, "cal_openings", map[string]interface{}{"calendars": []interface{}{"c1"}, "duration": "PT1H", "from": "2026-10-23T10:00:00Z", "to": "2026-10-23T18:00:00Z"})
	if err != nil {
		t.Fatalf("unexpected error on cal_openings: %v", err)
	}
	if _, ok := res["openings"]; !ok {
		t.Errorf("expected openings in response")
	}

	// 4. cal_propose
	res, err = client.Execute(ctx, "cal_propose", map[string]interface{}{"calendars": []interface{}{"c1"}, "start": "2026-10-23T10:00:00Z", "end": "2026-10-23T11:00:00Z"})
	if err != nil {
		t.Fatalf("unexpected error on cal_propose: %v", err)
	}
	if res["booking_id"] != "bk-456" {
		t.Errorf("expected booking_id bk-456, got %v", res["booking_id"])
	}

	// 5. cal_confirm
	res, err = client.Execute(ctx, "cal_confirm", map[string]interface{}{"booking_id": "bk-456"})
	if err != nil {
		t.Fatalf("unexpected error on cal_confirm: %v", err)
	}
	if res["confirmed"] != true {
		t.Errorf("expected confirmed true, got %v", res["confirmed"])
	}
}

func TestHTTPXoluClient_Execute_Errors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/asset/ast-404":
			http.NotFound(w, r)
		case "/api/v2/fsm/machine/m-failed/walk":
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code":    "XOLU-INVALID_TRANSITION",
				"message": "guard failed: temperature limit exceeded",
			})
		case "/api/v1/asset/ast-500":
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	client := NewHTTPXoluClient(ts.URL, 2*time.Second)
	ctx := context.Background()

	// 1. 404
	_, err := client.Execute(ctx, "get", map[string]interface{}{"entity_type": "asset", "id": "ast-404"})
	if err == nil {
		t.Fatalf("expected error on 404, got nil")
	}

	// 2. TypedError (verbatim invalid transition)
	_, err = client.Execute(ctx, "walk", map[string]interface{}{"machine_id": "m-failed", "input": "HEAT"})
	if err == nil {
		t.Fatalf("expected error on guard failure, got nil")
	}
	te, ok := err.(*taxonomy.TypedError)
	if !ok {
		t.Fatalf("expected *taxonomy.TypedError, got %T: %v", err, err)
	}
	if te.Code != "XOLU-INVALID_TRANSITION" {
		t.Errorf("expected XOLU-INVALID_TRANSITION, got %s", te.Code)
	}

	// 3. 500 SubstrateUnavailable
	_, err = client.Execute(ctx, "get", map[string]interface{}{"entity_type": "asset", "id": "ast-500"})
	if err == nil {
		t.Fatalf("expected error on 500, got nil")
	}
	te, ok = err.(*taxonomy.TypedError)
	if !ok || te.Code != string(taxonomy.PrefixMolu+taxonomy.CodeSubstrateUnavailable) {
		t.Errorf("expected SUBSTRATE_UNAVAILABLE on 500, got %v", err)
	}
}

// Real xolu v0.30.44 wraps errors as {"error":{"code","message","status"}}.
func TestHTTPXoluClient_Execute_NestedErrorEnvelope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/fsm/machine/m-404/walk", "/api/v2/fsm/machine/m-404/history":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":"XOLU-FSM002","message":"machine not found","status":404}}`))
		case "/api/v2/cal/check":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"XOLU-CAL004","message":"calendar_id is required","status":400}}`))
		}
	}))
	defer ts.Close()

	client := NewHTTPXoluClient(ts.URL, 2*time.Second)
	ctx := context.Background()

	for _, prim := range []string{"walk", "machine_history"} {
		_, err := client.Execute(ctx, prim, map[string]interface{}{"machine_id": "m-404", "input": "GO"})
		var se interface{ HTTPStatus() int }
		if !errors.As(err, &se) || se.HTTPStatus() != 404 {
			t.Fatalf("%s: expected an error exposing HTTP status 404, got %T: %v", prim, err, err)
		}
		if !strings.Contains(err.Error(), "XOLU-FSM002") || !strings.Contains(err.Error(), "machine not found") {
			t.Errorf("%s: error must keep xolu's code and message, got %q", prim, err.Error())
		}
	}

	_, err := client.Execute(ctx, "cal_check", map[string]interface{}{"calendars": []interface{}{"a"}})
	te, ok := err.(*taxonomy.TypedError)
	if !ok {
		t.Fatalf("expected *taxonomy.TypedError for a 400 with a nested code, got %T: %v", err, err)
	}
	if te.Code != "XOLU-CAL004" || te.Message != "calendar_id is required" {
		t.Errorf("xolu code and message must pass verbatim, got %s / %q", te.Code, te.Message)
	}
}

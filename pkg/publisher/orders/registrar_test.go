package orders

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/bridge"
	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

func fastBackoff() Backoff {
	return Backoff{Initial: time.Millisecond, Max: 5 * time.Millisecond, MaxAttempts: 6}
}

func newRealHub(t *testing.T) (*httptest.Server, *hub.Registry) {
	t.Helper()
	reg := hub.NewRegistry()
	provider, err := hub.NewRPIProvider("admit-all", "", "")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(hub.NewHubHTTPServer(reg, hub.NewDiscovery(reg, provider)).Handler())
	t.Cleanup(srv.Close)
	return srv, reg
}

func TestOrderRegistrar_RegistersFourNamespacedTools(t *testing.T) {
	srv, reg := newRealHub(t)
	r := NewRegistrar(srv.URL, "http://publisher:8092/", fastBackoff())

	if err := r.Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	tools := reg.List(PublisherID)
	if len(tools) != 30 {
		t.Fatalf("registered %d tools, want 30: %+v", len(tools), tools)
	}
	byName := map[string]hub.ToolRegistration{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	want := map[string]string{
		"work_orders.CreateOrder":            "http://publisher:8092/api/v1/orders/create",
		"work_orders.GetOrder":               "http://publisher:8092/api/v1/orders/get",
		"work_orders.AdvanceOrder":           "http://publisher:8092/api/v1/orders/advance",
		"work_orders.AssignOrder":            "http://publisher:8092/api/v1/orders/assign",
		"work_orders.RegisterMovement":       "http://publisher:8092/api/v1/orders/movements/register",
		"work_orders.ConsumeFromOrder":       "http://publisher:8092/api/v1/orders/consume",
		"work_orders.ReturnLeftovers":        "http://publisher:8092/api/v1/orders/return-leftovers",
		"work_orders.CreateCrew":             "http://publisher:8092/api/v1/orders/crews/create",
		"work_orders.UpdateCrew":             "http://publisher:8092/api/v1/orders/crews/update",
		"work_orders.CreateOutsourced":       "http://publisher:8092/api/v1/orders/outsourced/create",
		"work_orders.UpdateOutsourced":       "http://publisher:8092/api/v1/orders/outsourced/update",
		"work_orders.CreateWarehouse":        "http://publisher:8092/api/v1/orders/warehouses/create",
		"work_orders.UpdateWarehouse":        "http://publisher:8092/api/v1/orders/warehouses/update",
		"work_orders.CreateConsumable":       "http://publisher:8092/api/v1/orders/consumables/create",
		"work_orders.UpdateConsumable":       "http://publisher:8092/api/v1/orders/consumables/update",
		"work_orders.CreateTemplate":         "http://publisher:8092/api/v1/orders/templates/create",
		"work_orders.PublishTemplateVersion": "http://publisher:8092/api/v1/orders/templates/publish-version",
		"work_orders.ListTemplates":          "http://publisher:8092/api/v1/orders/templates",
	}
	for name, endpoint := range want {
		tl, ok := byName[name]
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tl.Namespace != Namespace || tl.Primitive || tl.Description == "" || tl.InputSchema["type"] != "object" {
			t.Errorf("%s: bad registration %+v", name, tl)
		}
		if tl.Metadata["endpoint"] != endpoint {
			t.Errorf("%s endpoint = %v, want %s", name, tl.Metadata["endpoint"], endpoint)
		}
		auth, _ := tl.Metadata["auth"].(map[string]any)
		if auth["mode"] != "bearer" || auth["token_ref"] != TokenRef {
			t.Errorf("%s auth = %v", name, auth)
		}
	}
}

func TestOrderRegistrar_RegisterIsIdempotent(t *testing.T) {
	srv, reg := newRealHub(t)
	r := NewRegistrar(srv.URL, "http://p", fastBackoff())
	for i := 0; i < 2; i++ {
		if err := r.Register(context.Background()); err != nil {
			t.Fatalf("Register #%d: %v", i, err)
		}
	}
	if n := len(reg.List(PublisherID)); n != 30 {
		t.Fatalf("%d tools after re-register, want 30", n)
	}
}

func TestOrderRegistrar_RetriesUntilHubIsUp(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := NewRegistrar(srv.URL, "http://p", fastBackoff()).Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if calls.Load() < 5 { // 2 failures + 3 tools
		t.Fatalf("only %d calls; backoff did not retry", calls.Load())
	}
}

func TestOrderRegistrar_ContractRejectionIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "contract violation", http.StatusBadRequest)
	}))
	defer srv.Close()

	if err := NewRegistrar(srv.URL, "http://p", fastBackoff()).Register(context.Background()); err == nil {
		t.Fatal("want error on 400")
	}
	if calls.Load() != 1 {
		t.Fatalf("%d calls, a 400 must not be retried", calls.Load())
	}
}

func TestOrderRegistrar_GivesUpAfterMaxAttempts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	b := fastBackoff()
	b.MaxAttempts = 3
	if err := NewRegistrar(srv.URL, "http://p", b).Register(context.Background()); err == nil {
		t.Fatal("want error after exhausting attempts")
	}
}

func TestOrderRegistrar_StopsWhenContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	b := Backoff{Initial: 50 * time.Millisecond, Max: 50 * time.Millisecond, MaxAttempts: 100}
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()

	start := time.Now()
	err := NewRegistrar(srv.URL, "http://p", b).Register(ctx)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("err=%v after %v; expected prompt cancellation", err, time.Since(start))
	}
}

func TestOrderRegistrar_UnregisterRemovesAllTools(t *testing.T) {
	srv, reg := newRealHub(t)
	r := NewRegistrar(srv.URL, "http://p", fastBackoff())
	if err := r.Register(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Unregister(context.Background()); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if n := len(reg.List(PublisherID)); n != 0 {
		t.Fatalf("%d tools left after shutdown", n)
	}
}

func TestOrderRegistrar_UnregisterWithoutPriorRegisterStillReportsErrors(t *testing.T) {
	srv, _ := newRealHub(t)
	if err := NewRegistrar(srv.URL, "http://p", fastBackoff()).Unregister(context.Background()); err == nil {
		t.Fatal("hub answers 404 for unknown tools; Unregister should surface it")
	}
}

// TestOrderRegistrar_AllThirtyToolsContractAudit verifies RF-6 and RF-7 across
// every single registered tool: exact names and count (30), namespace and auth
// metadata (token_ref), non-empty domain-neutral descriptions, JSON Schema
// compilation under the Execution Bridge validator (accepting valid input and
// rejecting unknown properties), and live route wiring on orders.Server.
func TestOrderRegistrar_AllThirtyToolsContractAudit(t *testing.T) {
	pubSrv := newTestServer(newFake())
	defer pubSrv.Close()

	hubSrv, reg := newRealHub(t)
	r := NewRegistrar(hubSrv.URL, pubSrv.URL, fastBackoff())
	if err := r.Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	validSamples := map[string]map[string]any{
		"work_orders.CreateOrder":   {"title": "Revisar bomba"},
		"work_orders.GetOrder":      {"order_id": "1"},
		"work_orders.AdvanceOrder":  {"order_id": "1", "input": "start"},
		"work_orders.ListOrders":    {},
		"work_orders.ClaimOrder":    {"order_id": "1", "user_id": 9},
		"work_orders.AssignOrder":   {"order_id": "1", "responsible_type": "crew", "responsible_id": 7},
		"work_orders.ListTemplates": {},
		"work_orders.GetTemplate":   {"template_id": 1},
		"work_orders.CreateTemplate": {
			"name": "T", "workflow_states": []any{"A", "B"},
			"header_items":        []any{map[string]any{"tag": "p", "type": "number", "filled_at": "upon_creating", "required": true}},
			"blocks":              []any{map[string]any{"name": "B1", "responsible_type": "crew", "lines": []any{map[string]any{"name": "ok", "type": "boolean"}}}},
			"consumables_enabled": true,
			"consumables":         []any{map[string]any{"consumable_id": "1", "quantity": 2}},
			"actions":             []any{map[string]any{"action_id": "3", "trigger_point": "after_block", "block_reference": "B1", "mapping_values": []any{map[string]any{"parameter": "x", "source": "fixed", "fixed_value": "1"}}}},
			"numbering":           map[string]any{"prefix": "WO-", "sequence_length": 4, "reset_policy": "yearly"},
		},
		"work_orders.PublishTemplateVersion": {"previous_template_id": 1, "name": "T", "workflow_states": []any{"A", "B"}},
		"work_orders.ListCrews":              {},
		"work_orders.GetCrew":                {"crew_id": 1},
		"work_orders.CreateCrew":             {"name": "C", "responsible_user_id": 1, "member_user_ids": []any{1, 2}},
		"work_orders.UpdateCrew":             {"crew_id": 1, "name": "C2"},
		"work_orders.ListOutsourced":         {},
		"work_orders.GetOutsourced":          {"outsourced_id": 1},
		"work_orders.CreateOutsourced":       {"name": "P"},
		"work_orders.UpdateOutsourced":       {"outsourced_id": 1, "name": "P2"},
		"work_orders.ListWarehouses":         {},
		"work_orders.GetWarehouse":           {"warehouse_id": 1},
		"work_orders.CreateWarehouse":        {"name": "W", "location": "0,0", "responsible_user_id": 1},
		"work_orders.UpdateWarehouse":        {"warehouse_id": 1, "name": "W2"},
		"work_orders.ListConsumables":        {},
		"work_orders.GetConsumable":          {"consumable_id": 1},
		"work_orders.CreateConsumable":       {"name": "K", "unit_id": 1},
		"work_orders.UpdateConsumable":       {"consumable_id": 1, "name": "K2"},
		"work_orders.ListLowStock":           {},
		"work_orders.RegisterMovement":       {"type": "entry", "consumable_id": 1, "user_id": 1, "target_warehouse_id": 1, "quantity": 2.5, "reason": "Alta"},
		"work_orders.ConsumeFromOrder":       {"order_id": "1", "consumable_id": 1, "user_id": 1, "quantity": 1.5},
		"work_orders.ReturnLeftovers":        {"order_id": "1", "consumable_id": 1, "target_warehouse_id": 1, "user_id": 1, "quantity": 1.0},
	}

	tools := reg.List(PublisherID)
	if len(tools) != len(validSamples) {
		t.Fatalf("registered %d tools, want %d", len(tools), len(validSamples))
	}

	seen := map[string]bool{}
	for _, tl := range tools {
		sample, expected := validSamples[tl.Name]
		if !expected {
			t.Errorf("unexpected tool registered: %s", tl.Name)
			continue
		}
		seen[tl.Name] = true

		if tl.Namespace != Namespace || tl.Primitive || !strings.HasPrefix(tl.Name, Namespace+".") {
			t.Errorf("%s: bad namespace/primitive metadata: %+v", tl.Name, tl)
		}
		if len(strings.TrimSpace(tl.Description)) < 20 {
			t.Errorf("%s: description too short: %q", tl.Name, tl.Description)
		}
		if strings.Contains(strings.ToLower(tl.Description), "seam") {
			t.Errorf("%s: description violates domain neutrality: %q", tl.Name, tl.Description)
		}

		// Auth metadata check.
		auth, ok := tl.Metadata["auth"].(map[string]any)
		if !ok || auth["mode"] != "bearer" || auth["header"] != "Authorization" || auth["token_ref"] != TokenRef {
			t.Errorf("%s: bad auth metadata %v", tl.Name, tl.Metadata["auth"])
		}

		// Endpoint reachability check against real orders.Server (must not be 404 route-not-found or 405).
		ep, _ := tl.Metadata["endpoint"].(string)
		if !strings.HasPrefix(ep, pubSrv.URL+"/api/v1/orders/") {
			t.Errorf("%s: unexpected endpoint %q", tl.Name, ep)
		} else {
			req, _ := http.NewRequest(http.MethodPost, ep, bytes.NewReader([]byte(`{}`)))
			req.Header.Set("Authorization", "Bearer "+testToken)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("%s: POST %s failed: %v", tl.Name, ep, err)
			} else {
				_ = resp.Body.Close()
				// 405 means route doesn't accept POST; 404 with plain text "404 page not found" means unregistered route.
				if resp.StatusCode == http.StatusMethodNotAllowed {
					t.Errorf("%s: endpoint %s returned 405", tl.Name, ep)
				}
				if resp.StatusCode == http.StatusNotFound && !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
					t.Errorf("%s: endpoint %s is not mounted on orders.Server (non-JSON 404)", tl.Name, ep)
				}
			}
		}

		// Round-trip InputSchema through JSON (as the Hub HTTP API delivers it)
		// and validate with Execution Bridge's real JSON Schema validator.
		rawSchema, err := json.Marshal(tl.InputSchema)
		if err != nil {
			t.Errorf("%s: marshal schema: %v", tl.Name, err)
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(rawSchema, &schema); err != nil {
			t.Errorf("%s: unmarshal schema: %v", tl.Name, err)
			continue
		}

		if err := bridge.ValidateDomainInput(schema, sample); err != nil {
			t.Errorf("%s: valid sample rejected by schema: %v", tl.Name, err)
		}

		bad := make(map[string]any, len(sample)+1)
		for k, v := range sample {
			bad[k] = v
		}
		bad["__unexpected_extra_field__"] = true
		err = bridge.ValidateDomainInput(schema, bad)
		var te *taxonomy.TypedError
		if !errors.As(err, &te) || taxonomy.StripPrefix(te.Code) != taxonomy.CodeContractViolation {
			t.Errorf("%s: unknown field should trigger CONTRACT_VIOLATION, got %T %v", tl.Name, err, err)
		}
	}

	for name := range validSamples {
		if !seen[name] {
			t.Errorf("missing expected tool registration: %s", name)
		}
	}

	// Verify exclusiveMinimum: 0 actually rejects zero quantity under the bridge validator.
	for _, toolName := range []string{"work_orders.ConsumeFromOrder", "work_orders.ReturnLeftovers"} {
		tl, ok := reg.Lookup(toolName)
		if !ok {
			t.Fatalf("%s not in registry", toolName)
		}
		raw, _ := json.Marshal(tl.InputSchema)
		var schema map[string]any
		_ = json.Unmarshal(raw, &schema)
		zeroSample := map[string]any{"order_id": "1", "consumable_id": 1, "user_id": 1, "quantity": 0}
		if toolName == "work_orders.ReturnLeftovers" {
			zeroSample["target_warehouse_id"] = 1
		}
		err := bridge.ValidateDomainInput(schema, zeroSample)
		var te *taxonomy.TypedError
		if !errors.As(err, &te) || taxonomy.StripPrefix(te.Code) != taxonomy.CodeContractViolation {
			t.Errorf("%s: quantity=0 should trigger CONTRACT_VIOLATION, got %T %v", toolName, err, err)
		}
	}
}

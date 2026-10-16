package rpi

import (
	"encoding/json"
	"testing"
)

func TestToolCandidateJSONFields(t *testing.T) {
	tc := ToolCandidate{
		Name:        "work_orders.CreateOrder",
		Description: "creates an order",
		InputSchema: map[string]any{"type": "object"},
		Primitive:   false,
		Namespace:   "work_orders",
	}

	got := marshalToMap(t, tc)

	for _, key := range []string{"name", "description", "inputSchema", "primitive", "namespace"} {
		if _, ok := got[key]; !ok {
			t.Errorf("ToolCandidate JSON missing key %q: %v", key, got)
		}
	}
	if got["name"] != "work_orders.CreateOrder" || got["namespace"] != "work_orders" || got["primitive"] != false {
		t.Errorf("unexpected ToolCandidate JSON values: %v", got)
	}
}

func TestAdmitContextJSONFields(t *testing.T) {
	actx := AdmitContext{Tenant: "acme", Roles: []string{"operator", "viewer"}, Query: "orders"}

	got := marshalToMap(t, actx)

	if got["tenant"] != "acme" || got["query"] != "orders" {
		t.Errorf("unexpected AdmitContext JSON values: %v", got)
	}
	roles, ok := got["roles"].([]any)
	if !ok || len(roles) != 2 || roles[0] != "operator" || roles[1] != "viewer" {
		t.Errorf("unexpected AdmitContext roles: %v", got["roles"])
	}

	// query is optional and omitted when empty.
	noQuery := marshalToMap(t, AdmitContext{Tenant: "acme"})
	if _, ok := noQuery["query"]; ok {
		t.Errorf("empty query should be omitted: %v", noQuery)
	}
}

func TestScoredToolEmbedsCandidate(t *testing.T) {
	st := ScoredTool{
		ToolCandidate: ToolCandidate{Name: "walk", Primitive: true},
		Score:         1.0,
		Rationale:     "admitted",
	}

	// Candidate fields are promoted to the ScoredTool.
	if st.Name != "walk" || !st.Primitive {
		t.Errorf("candidate fields not promoted: %+v", st)
	}

	got := marshalToMap(t, st)
	if got["name"] != "walk" || got["primitive"] != true || got["score"] != 1.0 || got["rationale"] != "admitted" {
		t.Errorf("unexpected ScoredTool JSON values: %v", got)
	}

	// rationale is optional and omitted when empty.
	noRationale := marshalToMap(t, ScoredTool{ToolCandidate: ToolCandidate{Name: "get"}, Score: 0.5})
	if _, ok := noRationale["rationale"]; ok {
		t.Errorf("empty rationale should be omitted: %v", noRationale)
	}
}

func marshalToMap(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

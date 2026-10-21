package rpi_integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/rpi/providers"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

const rulesJSON = `{
	"acme:operator": ["walk", "work_orders.CreateOrder"],
	"acme:viewer":   ["walk"]
}`

// newCatalog publishes the tools used by these tests to a real Hub registry.
func newCatalog(t *testing.T) *hub.Registry {
	t.Helper()
	reg := hub.NewRegistry()
	for _, r := range []hub.ToolRegistration{
		{PublisherID: "pub-1", Name: "walk", Primitive: true},
		{PublisherID: "pub-1", Name: "work_orders.CreateOrder", Namespace: "work_orders"},
	} {
		if err := reg.Register(r); err != nil {
			t.Fatalf("Register(%s) error: %v", r.Name, err)
		}
	}
	return reg
}

// lookupTool returns the catalog entry for name, failing if it does not exist.
func lookupTool(t *testing.T, reg *hub.Registry, name string) rpi.ToolCandidate {
	t.Helper()
	r, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("tool %q is not in the catalog", name)
	}
	return rpi.ToolCandidate{
		Name:        r.Name,
		Description: r.Description,
		InputSchema: r.InputSchema,
		Primitive:   r.Primitive,
		Namespace:   r.Namespace,
	}
}

func newStaticRules(t *testing.T) rpi.RPIProvider {
	t.Helper()
	rules, err := providers.ParseStaticContextRules([]byte(rulesJSON))
	if err != nil {
		t.Fatalf("ParseStaticContextRules error: %v", err)
	}
	return providers.NewStaticContextRulesProvider(rules)
}

func TestIntegration_NotOffered_ExistingToolRejectedByRPI(t *testing.T) {
	catalog := newCatalog(t)
	provider := newStaticRules(t)
	actx := rpi.AdmitContext{Tenant: "acme", Roles: []string{"viewer"}}

	// The tool exists in the catalog, but acme:viewer may not see it.
	tool := lookupTool(t, catalog, "work_orders.CreateOrder")
	err := rpi.Offer(context.Background(), provider, actx, tool)

	var te *taxonomy.TypedError
	if !errors.As(err, &te) {
		t.Fatalf("Offer error = %v (%T), want *taxonomy.TypedError", err, err)
	}
	if want := taxonomy.PrefixMolu + string(taxonomy.CodeNotOffered); te.Code != want {
		t.Fatalf("Code = %q, want exactly %q", te.Code, want)
	}
	for _, other := range []taxonomy.ErrorCode{taxonomy.CodeNotFound, taxonomy.CodeEmpty} {
		if te.Code == taxonomy.PrefixMolu+string(other) {
			t.Errorf("Code = %q, must not collapse into %s", te.Code, other)
		}
	}
	if te.Details["tenant"] != "acme" || te.Details["tool"] != "work_orders.CreateOrder" {
		t.Errorf("Details = %v, want tenant acme and tool work_orders.CreateOrder", te.Details)
	}

	// The agent receives the code as JSON.
	body, err := json.Marshal(te)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	var wire struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if wire.Code != "XOLU-MOLU-FRONT-NOT_OFFERED" {
		t.Errorf("JSON code = %q, want XOLU-MOLU-FRONT-NOT_OFFERED", wire.Code)
	}
}

func TestIntegration_NotOffered_SameToolOfferedWhenAllowed(t *testing.T) {
	catalog := newCatalog(t)
	tool := lookupTool(t, catalog, "work_orders.CreateOrder")

	// The rejection comes from the RPI decision, not from the tool itself:
	// an allowed role, or the admit-all provider, gets the same tool offered.
	cases := map[string]struct {
		provider rpi.RPIProvider
		actx     rpi.AdmitContext
	}{
		"static-context-rules allowed role": {newStaticRules(t), rpi.AdmitContext{Tenant: "acme", Roles: []string{"operator"}}},
		"admit-all":                         {providers.NewAdmitAllProvider(), rpi.AdmitContext{Tenant: "acme", Roles: []string{"viewer"}}},
	}
	for name, c := range cases {
		if err := rpi.Offer(context.Background(), c.provider, c.actx, tool); err != nil {
			t.Errorf("%s: Offer error = %v, want nil", name, err)
		}
	}
}

package rpi

import (
	"context"
	"errors"
	"testing"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

func TestOfferAdmittedTool(t *testing.T) {
	m := &MockRPIProvider{
		AdmitFunc: func(ctx context.Context, actx AdmitContext, candidates []ToolCandidate) ([]ToolCandidate, error) {
			return candidates, nil
		},
	}
	actx := AdmitContext{Tenant: "acme", Roles: []string{"operator"}}
	tool := ToolCandidate{Name: "work_orders.CreateOrder", Namespace: "work_orders"}

	if err := Offer(context.Background(), m, actx, tool); err != nil {
		t.Fatalf("Offer error = %v, want nil", err)
	}
	if len(m.AdmitCalls) != 1 || m.AdmitCalls[0].Context.Tenant != "acme" ||
		len(m.AdmitCalls[0].Candidates) != 1 || m.AdmitCalls[0].Candidates[0].Name != tool.Name {
		t.Errorf("unexpected AdmitCalls: %+v", m.AdmitCalls)
	}
}

func TestOfferExcludedToolIsNotOffered(t *testing.T) {
	m := &MockRPIProvider{
		AdmitFunc: func(context.Context, AdmitContext, []ToolCandidate) ([]ToolCandidate, error) {
			return []ToolCandidate{}, nil
		},
	}
	actx := AdmitContext{Tenant: "acme", Roles: []string{"viewer"}}

	err := Offer(context.Background(), m, actx, ToolCandidate{Name: "work_orders.CreateOrder", Namespace: "work_orders"})

	var te *taxonomy.TypedError
	if !errors.As(err, &te) {
		t.Fatalf("Offer error = %v (%T), want *taxonomy.TypedError", err, err)
	}
	if want := taxonomy.PrefixMolu + string(taxonomy.CodeNotOffered); te.Code != want {
		t.Errorf("Code = %q, want %q", te.Code, want)
	}
	if te.Details["tenant"] != "acme" || te.Details["tool"] != "work_orders.CreateOrder" {
		t.Errorf("Details = %v, want tenant acme and tool work_orders.CreateOrder", te.Details)
	}
}

func TestOfferPropagatesProviderError(t *testing.T) {
	boom := errors.New("provider down")
	m := &MockRPIProvider{
		AdmitFunc: func(context.Context, AdmitContext, []ToolCandidate) ([]ToolCandidate, error) { return nil, boom },
	}

	err := Offer(context.Background(), m, AdmitContext{Tenant: "acme"}, ToolCandidate{Name: "walk", Primitive: true})
	if !errors.Is(err, boom) {
		t.Errorf("Offer error = %v, want %v", err, boom)
	}
}

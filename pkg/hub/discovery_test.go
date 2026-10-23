package hub

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/rpi/providers"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

var tenantCtx = rpi.AdmitContext{Tenant: "t1", Roles: []string{"operator"}}

func seededRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	for _, reg := range []ToolRegistration{
		primitiveTool("pub-1", "walk"),
		domainTool("pub-1", "work_orders", "CreateOrder"),
		domainTool("pub-2", "inventory", "ReserveStock"),
	} {
		if err := r.Register(reg); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func names(tools []rpi.ScoredTool) []string {
	out := make([]string, len(tools))
	for i, tl := range tools {
		out[i] = tl.Name
	}
	return out
}

func TestDiscoveryRejectsEmptyTenantWithoutCallingProvider(t *testing.T) {
	mock := &rpi.MockRPIProvider{}
	d := NewDiscovery(seededRegistry(t), mock)

	_, err := d.ListTools(context.Background(), rpi.AdmitContext{Roles: []string{"operator"}})
	te := requireTyped(t, err, taxonomy.CodeContractViolation)
	if fields, _ := te.Details["fields"].([]string); !reflect.DeepEqual(fields, []string{"tenant"}) {
		t.Fatalf("expected fields [tenant], got %v", te.Details["fields"])
	}
	if n := len(mock.AdmitCalls); n != 0 {
		t.Fatalf("provider must not be called, got %d Admit calls", n)
	}
}

func TestDiscoveryAdmitAllOffersWholeCatalog(t *testing.T) {
	d := NewDiscovery(seededRegistry(t), providers.NewAdmitAllProvider())

	got, err := d.ListTools(context.Background(), tenantCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"inventory.ReserveStock", "walk", "work_orders.CreateOrder"}
	if !reflect.DeepEqual(names(got), want) {
		t.Fatalf("expected %v, got %v", want, names(got))
	}
	for _, tl := range got {
		if tl.Score != 1.0 {
			t.Fatalf("%s: expected score 1.0, got %v", tl.Name, tl.Score)
		}
	}
}

func TestDiscoveryOmitsToolsTheProviderDoesNotAdmit(t *testing.T) {
	rules := map[string][]string{"t1:operator": {"walk", "work_orders.CreateOrder"}}
	d := NewDiscovery(seededRegistry(t), providers.NewStaticContextRulesProvider(rules))

	got, err := d.ListTools(context.Background(), tenantCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"walk", "work_orders.CreateOrder"}
	if !reflect.DeepEqual(names(got), want) {
		t.Fatalf("expected %v, got %v", want, names(got))
	}
}

func TestDiscoveryOrdersByDescendingScore(t *testing.T) {
	mock := &rpi.MockRPIProvider{
		AdmitFunc: func(_ context.Context, _ rpi.AdmitContext, c []rpi.ToolCandidate) ([]rpi.ToolCandidate, error) {
			return c, nil
		},
		ScoreFunc: func(_ context.Context, _ rpi.AdmitContext, a []rpi.ToolCandidate) ([]rpi.ScoredTool, error) {
			return []rpi.ScoredTool{
				{ToolCandidate: a[2], Score: 0.9},
				{ToolCandidate: a[0], Score: 0.5},
				{ToolCandidate: a[1], Score: 0.1},
			}, nil
		},
	}
	d := NewDiscovery(seededRegistry(t), mock)

	got, err := d.ListTools(context.Background(), tenantCtx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"work_orders.CreateOrder", "inventory.ReserveStock", "walk"}
	if !reflect.DeepEqual(names(got), want) {
		t.Fatalf("expected provider order %v, got %v", want, names(got))
	}
}

func TestDiscoveryEmptyWhenNothingIsAdmitted(t *testing.T) {
	cases := map[string]struct {
		reg  *Registry
		prov rpi.RPIProvider
	}{
		"empty catalog":   {NewRegistry(), providers.NewAdmitAllProvider()},
		"no rule matches": {seededRegistry(t), providers.NewStaticContextRulesProvider(map[string][]string{"other:role": {"walk"}})},
		"no rules at all": {seededRegistry(t), providers.NewStaticContextRulesProvider(nil)},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			got, err := NewDiscovery(tc.reg, tc.prov).ListTools(context.Background(), tenantCtx)
			te := requireTyped(t, err, taxonomy.CodeEmpty)
			if te.Details["tenant"] != "t1" {
				t.Fatalf("expected tenant in details, got %v", te.Details)
			}
			if len(got) != 0 {
				t.Fatalf("expected no tools, got %v", names(got))
			}
		})
	}
}

func TestDiscoveryReturnsProviderErrorsUnchanged(t *testing.T) {
	boom := errors.New("rpi backend down")
	mock := &rpi.MockRPIProvider{
		AdmitFunc: func(context.Context, rpi.AdmitContext, []rpi.ToolCandidate) ([]rpi.ToolCandidate, error) {
			return nil, boom
		},
	}
	_, err := NewDiscovery(seededRegistry(t), mock).ListTools(context.Background(), tenantCtx)
	if !errors.Is(err, boom) {
		t.Fatalf("expected provider error, got %v", err)
	}

	mock = &rpi.MockRPIProvider{
		AdmitFunc: func(_ context.Context, _ rpi.AdmitContext, c []rpi.ToolCandidate) ([]rpi.ToolCandidate, error) {
			return c, nil
		},
		ScoreFunc: func(context.Context, rpi.AdmitContext, []rpi.ToolCandidate) ([]rpi.ScoredTool, error) {
			return nil, boom
		},
	}
	_, err = NewDiscovery(seededRegistry(t), mock).ListTools(context.Background(), tenantCtx)
	if !errors.Is(err, boom) {
		t.Fatalf("expected score error, got %v", err)
	}
}

func TestDiscoveryPassesCatalogFieldsToProvider(t *testing.T) {
	r := NewRegistry()
	reg := domainTool("pub-1", "work_orders", "CreateOrder")
	reg.Description = "creates an order"
	reg.InputSchema = map[string]any{"type": "object", "required": []any{"id"}}
	if err := r.Register(reg); err != nil {
		t.Fatal(err)
	}
	got, err := NewDiscovery(r, providers.NewAdmitAllProvider()).ListTools(context.Background(), tenantCtx)
	if err != nil {
		t.Fatal(err)
	}
	tl := got[0]
	if tl.Description != "creates an order" || tl.Namespace != "work_orders" || tl.Primitive ||
		!reflect.DeepEqual(tl.InputSchema, reg.InputSchema) {
		t.Fatalf("catalog fields lost on the way: %+v", tl)
	}
}

func TestDiscoveryConcurrentWithRegistrations(t *testing.T) {
	r := seededRegistry(t)
	d := NewDiscovery(r, providers.NewAdmitAllProvider())

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = r.Register(domainTool("pub-x", "ns", fmt.Sprintf("Fn%d", i)))
		}(i)
		go func() {
			defer wg.Done()
			if _, err := d.ListTools(context.Background(), tenantCtx); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
}

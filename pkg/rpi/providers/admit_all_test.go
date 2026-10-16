package providers

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

func sampleCatalog() []rpi.ToolCandidate {
	return []rpi.ToolCandidate{
		{Name: "walk", Primitive: true},
		{Name: "work_orders.CreateOrder", Namespace: "work_orders"},
		{Name: "inventory.ReserveStock", Namespace: "inventory"},
	}
}

func TestAdmitAllAdmitReturnsAll(t *testing.T) {
	p := NewAdmitAllProvider()
	catalog := sampleCatalog()

	// admit-all ignores tenant, roles and query.
	for _, actx := range []rpi.AdmitContext{
		{Tenant: "acme", Roles: []string{"operator"}},
		{Tenant: "other", Roles: nil, Query: "stock"},
	} {
		admitted, err := p.Admit(context.Background(), actx, catalog)
		if err != nil {
			t.Fatalf("Admit(%+v) error: %v", actx, err)
		}
		if len(admitted) != len(catalog) {
			t.Fatalf("Admit(%+v) = %d tools, want %d", actx, len(admitted), len(catalog))
		}
		for i := range catalog {
			if admitted[i].Name != catalog[i].Name {
				t.Errorf("Admit(%+v)[%d] = %q, want %q", actx, i, admitted[i].Name, catalog[i].Name)
			}
		}
	}
}

func TestAdmitAllAdmitEmptyCatalog(t *testing.T) {
	p := NewAdmitAllProvider()

	for name, catalog := range map[string][]rpi.ToolCandidate{"nil": nil, "empty": {}} {
		admitted, err := p.Admit(context.Background(), rpi.AdmitContext{Tenant: "acme"}, catalog)
		if err != nil {
			t.Fatalf("%s catalog: Admit error: %v", name, err)
		}
		if admitted == nil || len(admitted) != 0 {
			t.Errorf("%s catalog: Admit = %#v, want empty non-nil slice", name, admitted)
		}
	}
}

func TestAdmitAllAdmitDoesNotShareInput(t *testing.T) {
	p := NewAdmitAllProvider()
	catalog := sampleCatalog()

	admitted, err := p.Admit(context.Background(), rpi.AdmitContext{Tenant: "acme"}, catalog)
	if err != nil {
		t.Fatalf("Admit error: %v", err)
	}
	admitted[0].Name = "changed"

	if catalog[0].Name != "walk" {
		t.Errorf("modifying the result changed the input: %q", catalog[0].Name)
	}
}

func TestAdmitAllScoreUniformOne(t *testing.T) {
	p := NewAdmitAllProvider()
	admitted := sampleCatalog()

	scored, err := p.Score(context.Background(), rpi.AdmitContext{Tenant: "acme"}, admitted)
	if err != nil {
		t.Fatalf("Score error: %v", err)
	}
	if len(scored) != len(admitted) {
		t.Fatalf("Score = %d tools, want %d", len(scored), len(admitted))
	}
	for i, st := range scored {
		if st.Score != 1.0 {
			t.Errorf("scored[%d].Score = %v, want 1.0", i, st.Score)
		}
	}
}

func TestAdmitAllScorePreservesOrder(t *testing.T) {
	p := NewAdmitAllProvider()
	admitted := sampleCatalog()

	scored, err := p.Score(context.Background(), rpi.AdmitContext{Tenant: "acme"}, admitted)
	if err != nil {
		t.Fatalf("Score error: %v", err)
	}
	for i := range admitted {
		if scored[i].ToolCandidate.Name != admitted[i].Name {
			t.Errorf("scored[%d] = %q, want %q", i, scored[i].Name, admitted[i].Name)
		}
	}
}

func TestAdmitAllScoreEmpty(t *testing.T) {
	p := NewAdmitAllProvider()

	scored, err := p.Score(context.Background(), rpi.AdmitContext{Tenant: "acme"}, nil)
	if err != nil {
		t.Fatalf("Score error: %v", err)
	}
	if scored == nil || len(scored) != 0 {
		t.Errorf("Score(nil) = %#v, want empty non-nil slice", scored)
	}
}

func TestAdmitAllConcurrent(t *testing.T) {
	p := NewAdmitAllProvider()
	catalog := sampleCatalog()

	const n = 100
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			actx := rpi.AdmitContext{Tenant: fmt.Sprintf("tenant-%d", i), Roles: []string{"operator"}}
			admitted, err := p.Admit(context.Background(), actx, catalog)
			if err != nil {
				errs <- err
				return
			}
			// Each caller owns its result and may modify it.
			admitted[0].Name = fmt.Sprintf("changed-%d", i)
			scored, err := p.Score(context.Background(), actx, admitted)
			if err != nil {
				errs <- err
				return
			}
			if len(scored) != len(catalog) {
				errs <- fmt.Errorf("goroutine %d: got %d scored tools, want %d", i, len(scored), len(catalog))
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
	if catalog[0].Name != "walk" {
		t.Errorf("concurrent callers changed the shared catalog: %q", catalog[0].Name)
	}
}

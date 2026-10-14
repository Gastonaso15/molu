package hub

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

func domainTool(publisher, ns, fn string) ToolRegistration {
	return ToolRegistration{
		PublisherID: publisher,
		Name:        ns + "." + fn,
		Namespace:   ns,
		Description: "domain function",
		InputSchema: map[string]any{"type": "object"},
	}
}

func primitiveTool(publisher, name string) ToolRegistration {
	return ToolRegistration{
		PublisherID: publisher,
		Name:        name,
		Primitive:   true,
		Description: "generic primitive",
		InputSchema: map[string]any{"type": "object"},
	}
}

func requireTyped(t *testing.T, err error, code taxonomy.ErrorCode) *taxonomy.TypedError {
	t.Helper()
	var te *taxonomy.TypedError
	if !errors.As(err, &te) {
		t.Fatalf("expected *taxonomy.TypedError, got %T (%v)", err, err)
	}
	if te.Code != taxonomy.PrefixMolu+string(code) {
		t.Fatalf("expected code %s, got %s", taxonomy.PrefixMolu+string(code), te.Code)
	}
	return te
}

func TestRegisterAndLookupDomainTool(t *testing.T) {
	r := NewRegistry()
	reg := domainTool("pub-1", "work_orders", "CreateOrder")
	if err := r.Register(reg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := r.Lookup("work_orders.CreateOrder")
	if !ok {
		t.Fatal("expected tool to be found")
	}
	if got.PublisherID != "pub-1" || got.Namespace != "work_orders" {
		t.Fatalf("unexpected registration: %+v", got)
	}
}

func TestRegisterPrimitiveWithoutNamespace(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(primitiveTool("pub-1", "walk")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.Lookup("walk"); !ok {
		t.Fatal("expected primitive to be found")
	}
}

func TestRegisterRejectsInvalidPayload(t *testing.T) {
	cases := map[string]struct {
		reg    ToolRegistration
		fields []string
	}{
		"empty publisher": {
			reg:    func() ToolRegistration { x := domainTool("", "ns", "Fn"); return x }(),
			fields: []string{"publisher_id"},
		},
		"empty name": {
			reg:    ToolRegistration{PublisherID: "p", Primitive: true},
			fields: []string{"name"},
		},
		"primitive with namespace": {
			reg:    ToolRegistration{PublisherID: "p", Name: "walk", Primitive: true, Namespace: "ns"},
			fields: []string{"namespace"},
		},
		"primitive with dotted name": {
			reg:    ToolRegistration{PublisherID: "p", Name: "ns.walk", Primitive: true},
			fields: []string{"name"},
		},
		"domain without namespace": {
			reg:    ToolRegistration{PublisherID: "p", Name: "CreateOrder"},
			fields: []string{"namespace"},
		},
		"domain name without namespace prefix": {
			reg:    ToolRegistration{PublisherID: "p", Name: "CreateOrder", Namespace: "work_orders"},
			fields: []string{"name"},
		},
		"domain name with empty function part": {
			reg:    ToolRegistration{PublisherID: "p", Name: "work_orders.", Namespace: "work_orders"},
			fields: []string{"name"},
		},
		"multiple invalid fields": {
			reg:    ToolRegistration{Primitive: true},
			fields: []string{"publisher_id", "name"},
		},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			r := NewRegistry()
			te := requireTyped(t, r.Register(tc.reg), taxonomy.CodeContractViolation)
			got, _ := te.Details["fields"].([]string)
			if !reflect.DeepEqual(got, tc.fields) {
				t.Fatalf("expected fields %v, got %v", tc.fields, got)
			}
			if n := len(r.List("")); n != 0 {
				t.Fatalf("invalid registration must not be stored, catalog has %d", n)
			}
		})
	}
}

func TestRegisterDuplicateOverwritesIdempotently(t *testing.T) {
	r := NewRegistry()
	first := domainTool("pub-1", "work_orders", "CreateOrder")
	first.Description = "v1"
	second := first
	second.Description = "v2"

	if err := r.Register(first); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(second); err != nil {
		t.Fatalf("duplicate registration must not error: %v", err)
	}
	if err := r.Register(second); err != nil {
		t.Fatalf("repeating the same registration must not error: %v", err)
	}
	if n := len(r.List("")); n != 1 {
		t.Fatalf("expected 1 tool after overwrite, got %d", n)
	}
	got, _ := r.Lookup("work_orders.CreateOrder")
	if got.Description != "v2" {
		t.Fatalf("expected overwritten description v2, got %q", got.Description)
	}
}

func TestRegisterRejectsNameOwnedByAnotherPublisher(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(primitiveTool("pub-1", "walk")); err != nil {
		t.Fatal(err)
	}
	te := requireTyped(t, r.Register(primitiveTool("pub-2", "walk")), taxonomy.CodeContractViolation)
	got, _ := te.Details["fields"].([]string)
	if !reflect.DeepEqual(got, []string{"name"}) {
		t.Fatalf("expected fields [name], got %v", got)
	}
	reg, _ := r.Lookup("walk")
	if reg.PublisherID != "pub-1" {
		t.Fatalf("original owner must be preserved, got %q", reg.PublisherID)
	}
}

func TestUnregister(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(domainTool("pub-1", "inventory", "ReserveStock")); err != nil {
		t.Fatal(err)
	}
	if err := r.Unregister("pub-1", "inventory.ReserveStock"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.Lookup("inventory.ReserveStock"); ok {
		t.Fatal("tool should be gone from flat index")
	}
	if n := len(r.List("pub-1")); n != 0 {
		t.Fatalf("tool should be gone from publisher view, got %d", n)
	}
}

func TestUnregisterNotFound(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(domainTool("pub-1", "inventory", "ReserveStock")); err != nil {
		t.Fatal(err)
	}
	for label, args := range map[string][2]string{
		"unknown name":      {"pub-1", "inventory.Missing"},
		"unknown publisher": {"pub-9", "inventory.ReserveStock"},
	} {
		t.Run(label, func(t *testing.T) {
			te := requireTyped(t, r.Unregister(args[0], args[1]), taxonomy.CodeNotFound)
			if te.Details["publisher_id"] != args[0] || te.Details["name"] != args[1] {
				t.Fatalf("expected details {publisher_id, name}, got %v", te.Details)
			}
		})
	}
	if _, ok := r.Lookup("inventory.ReserveStock"); !ok {
		t.Fatal("failed unregister must not remove other publishers' tools")
	}
}

func TestListByPublisherAndAll(t *testing.T) {
	r := NewRegistry()
	for _, reg := range []ToolRegistration{
		domainTool("pub-2", "inventory", "ReserveStock"),
		domainTool("pub-1", "work_orders", "CreateOrder"),
		domainTool("pub-1", "work_orders", "CloseOrder"),
		primitiveTool("pub-1", "walk"),
	} {
		if err := r.Register(reg); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(r.List("pub-1")); n != 3 {
		t.Fatalf("expected 3 tools for pub-1, got %d", n)
	}
	all := r.List("")
	if len(all) != 4 {
		t.Fatalf("expected 4 tools overall, got %d", len(all))
	}
	names := make([]string, len(all))
	for i, reg := range all {
		names[i] = reg.Name
	}
	want := []string{"inventory.ReserveStock", "walk", "work_orders.CloseOrder", "work_orders.CreateOrder"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("expected deterministic order by name %v, got %v", want, names)
	}
}

func TestListOnEmptyCatalogReturnsEmptySlice(t *testing.T) {
	got := NewRegistry().List("")
	if got == nil || len(got) != 0 {
		t.Fatalf("expected non-nil empty slice, got %#v", got)
	}
}

func TestReturnedRegistrationsAreIsolatedFromCatalog(t *testing.T) {
	r := NewRegistry()
	reg := domainTool("pub-1", "work_orders", "CreateOrder")
	reg.Metadata = map[string]any{"k": "v"}
	if err := r.Register(reg); err != nil {
		t.Fatal(err)
	}
	// Mutating the caller's input after Register must not affect the catalog.
	reg.Metadata["k"] = "mutated"
	reg.InputSchema["type"] = "mutated"

	got, _ := r.Lookup("work_orders.CreateOrder")
	if got.Metadata["k"] != "v" || got.InputSchema["type"] != "object" {
		t.Fatalf("catalog aliased caller input: %+v", got)
	}
	// Mutating a returned copy must not affect the catalog either.
	got.Metadata["k"] = "mutated"
	got.InputSchema["type"] = "mutated"
	again, _ := r.Lookup("work_orders.CreateOrder")
	if again.Metadata["k"] != "v" || again.InputSchema["type"] != "object" {
		t.Fatalf("catalog aliased returned copy: %+v", again)
	}
	listed := r.List("")
	listed[0].Metadata["k"] = "mutated"
	third, _ := r.Lookup("work_orders.CreateOrder")
	if third.Metadata["k"] != "v" {
		t.Fatal("catalog aliased List result")
	}
}

// TestConcurrentAccess hammers every operation at once; it is meaningful
// under `go test -race` (RF-10).
func TestConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	const (
		publishers = 8
		tools      = 50
		rounds     = 20
	)
	var wg sync.WaitGroup
	for p := 0; p < publishers; p++ {
		pub := fmt.Sprintf("pub-%d", p)
		ns := fmt.Sprintf("ns%d", p)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < rounds; round++ {
				for i := 0; i < tools; i++ {
					reg := domainTool(pub, ns, fmt.Sprintf("Fn%d", i))
					reg.Metadata = map[string]any{"round": round}
					if err := r.Register(reg); err != nil {
						t.Errorf("register: %v", err)
						return
					}
				}
				for i := 0; i < tools; i += 2 {
					if err := r.Unregister(pub, fmt.Sprintf("%s.Fn%d", ns, i)); err != nil {
						// Allowed only when already removed in an earlier round.
						requireTyped(t, err, taxonomy.CodeNotFound)
					}
				}
			}
		}()
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds*tools; i++ {
				r.List(pub)
				r.List("")
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < rounds*tools; i++ {
				r.Lookup(fmt.Sprintf("%s.Fn%d", ns, i%tools))
			}
		}()
	}
	wg.Wait()

	// After the storm, every odd-numbered tool must remain, every even one gone.
	for p := 0; p < publishers; p++ {
		got := r.List(fmt.Sprintf("pub-%d", p))
		if len(got) != tools/2 {
			t.Fatalf("pub-%d: expected %d tools, got %d", p, tools/2, len(got))
		}
	}
}

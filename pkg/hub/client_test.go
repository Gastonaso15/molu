package hub_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/rpi/providers"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

func TestRemoteHubClient_ListTools_SuccessAndEmpty(t *testing.T) {
	registry := hub.NewRegistry()
	provider := providers.NewAdmitAllProvider()
	discovery := hub.NewDiscovery(registry, provider)
	server := hub.NewHubHTTPServer(registry, discovery)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	client := hub.NewRemoteHubClient(ts.URL, 2*time.Second)

	// 1. Empty catalog -> returns taxonomy.CodeEmpty
	_, err := client.ListTools(context.Background(), rpi.AdmitContext{Tenant: "acme"})
	if err == nil {
		t.Fatal("expected error on empty catalog, got nil")
	}
	if !strings.Contains(taxonomy.CodeFromError(err), string(taxonomy.CodeEmpty)) {
		t.Fatalf("expected EMPTY error, got %v", err)
	}

	// 2. Register tool and verify discovery
	reg := hub.ToolRegistration{
		PublisherID: "pub-1",
		Name:        "work_orders.CreateOrder",
		Description: "Create a work order",
		InputSchema: map[string]any{"type": "object"},
		Namespace:   "work_orders",
	}
	if err := registry.Register(reg); err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	tools, err := client.ListTools(context.Background(), rpi.AdmitContext{Tenant: "acme"})
	if err != nil {
		t.Fatalf("unexpected error listing tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "work_orders.CreateOrder" {
		t.Fatalf("expected 1 tool 'work_orders.CreateOrder', got %v", tools)
	}
}

func TestRemoteHubClient_ListTools_ContractViolation(t *testing.T) {
	registry := hub.NewRegistry()
	provider := providers.NewAdmitAllProvider()
	discovery := hub.NewDiscovery(registry, provider)
	server := hub.NewHubHTTPServer(registry, discovery)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	client := hub.NewRemoteHubClient(ts.URL, 2*time.Second)

	// Missing tenant -> CONTRACT_VIOLATION
	_, err := client.ListTools(context.Background(), rpi.AdmitContext{Tenant: ""})
	if err == nil {
		t.Fatal("expected error with empty tenant, got nil")
	}
	if !strings.Contains(taxonomy.CodeFromError(err), string(taxonomy.CodeContractViolation)) {
		t.Fatalf("expected CONTRACT_VIOLATION error, got %v", err)
	}
}

func TestRemoteHubClient_Lookup(t *testing.T) {
	registry := hub.NewRegistry()
	server := hub.NewHubHTTPServer(registry, nil)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	client := hub.NewRemoteHubClient(ts.URL, 2*time.Second)

	// 1. Lookup non-existent
	_, ok := client.Lookup("work_orders.GetOrder")
	if ok {
		t.Fatal("expected false for unregistered tool")
	}

	// 2. Register tool and lookup
	reg := hub.ToolRegistration{
		PublisherID: "pub-1",
		Name:        "work_orders.GetOrder",
		Description: "Get order details",
		InputSchema: map[string]any{"type": "object"},
		Namespace:   "work_orders",
	}
	if err := registry.Register(reg); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	found, ok := client.Lookup("work_orders.GetOrder")
	if !ok {
		t.Fatal("expected true for registered tool")
	}
	if found.Name != "work_orders.GetOrder" || found.PublisherID != "pub-1" {
		t.Fatalf("unexpected tool details: %+v", found)
	}
}

func TestRemoteHubClient_ServerUnavailable(t *testing.T) {
	// Pointing to a dead port
	client := hub.NewRemoteHubClient("http://127.0.0.1:59999", 500*time.Millisecond)

	_, err := client.ListTools(context.Background(), rpi.AdmitContext{Tenant: "acme"})
	if err == nil {
		t.Fatal("expected error on dead server, got nil")
	}
	if !strings.Contains(taxonomy.CodeFromError(err), string(taxonomy.CodeSubstrateUnavailable)) {
		t.Fatalf("expected SUBSTRATE_UNAVAILABLE error, got %v", err)
	}
}

func TestRemoteHubClient_Concurrency(t *testing.T) {
	registry := hub.NewRegistry()
	reg := hub.ToolRegistration{
		PublisherID: "pub-1",
		Name:        "work_orders.CreateOrder",
		Description: "Create a work order",
		InputSchema: map[string]any{"type": "object"},
		Namespace:   "work_orders",
	}
	_ = registry.Register(reg)

	provider := providers.NewAdmitAllProvider()
	discovery := hub.NewDiscovery(registry, provider)
	server := hub.NewHubHTTPServer(registry, discovery)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	client := hub.NewRemoteHubClient(ts.URL, 2*time.Second)

	const numRoutines = 25
	errCh := make(chan error, numRoutines*2)

	for i := 0; i < numRoutines; i++ {
		go func() {
			_, ok := client.Lookup("work_orders.CreateOrder")
			if !ok {
				errCh <- context.DeadlineExceeded
				return
			}
			errCh <- nil
		}()

		go func() {
			tools, err := client.ListTools(context.Background(), rpi.AdmitContext{Tenant: "acme"})
			if err != nil || len(tools) != 1 {
				errCh <- err
				return
			}
			errCh <- nil
		}()
	}

	for i := 0; i < numRoutines*2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent remote hub client error: %v", err)
		}
	}
}

// flakyHub wraps a Hub handler so tests can simulate a transient outage.
type flakyHub struct {
	down  atomic.Bool
	calls atomic.Int64
	next  http.Handler
}

func (f *flakyHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	if f.down.Load() {
		http.Error(w, "hub down", http.StatusServiceUnavailable)
		return
	}
	f.next.ServeHTTP(w, r)
}

func newFlakyHub(t *testing.T) (*hub.Registry, *flakyHub, *httptest.Server) {
	t.Helper()
	registry := hub.NewRegistry()
	discovery := hub.NewDiscovery(registry, providers.NewAdmitAllProvider())
	f := &flakyHub{next: hub.NewHubHTTPServer(registry, discovery).Handler()}
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	return registry, f, ts
}

func registerOrders(t *testing.T, r *hub.Registry, name string) {
	t.Helper()
	err := r.Register(hub.ToolRegistration{
		PublisherID: "pub-1", Name: name, Description: name,
		InputSchema: map[string]any{"type": "object"}, Namespace: "work_orders",
	})
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func TestRemoteHubClient_StartPolling_RefreshesCatalogue(t *testing.T) {
	registry, _, ts := newFlakyHub(t)
	client := hub.NewRemoteHubClient(ts.URL, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartPolling(ctx, 20*time.Millisecond)

	registerOrders(t, registry, "work_orders.GetOrder")
	eventually(t, "polling to pick up the new tool", func() bool {
		_, ok := client.Lookup("work_orders.GetOrder")
		return ok
	})
}

func TestRemoteHubClient_StartPolling_RetainsLastCatalogueWhenHubDown(t *testing.T) {
	registry, f, ts := newFlakyHub(t)
	registerOrders(t, registry, "work_orders.CreateOrder")
	client := hub.NewRemoteHubClient(ts.URL, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartPolling(ctx, 20*time.Millisecond)
	eventually(t, "first catalogue", func() bool {
		_, ok := client.Lookup("work_orders.CreateOrder")
		return ok
	})

	// Hub goes down: several polls fail, the last valid catalogue stays.
	f.down.Store(true)
	before := f.calls.Load()
	eventually(t, "failing polls", func() bool { return f.calls.Load() >= before+3 })
	if _, ok := client.Lookup("work_orders.CreateOrder"); !ok {
		t.Fatal("catalogue must be retained while the Hub is down")
	}

	// Hub comes back with a changed catalogue: the cache is replaced.
	registerOrders(t, registry, "work_orders.AdvanceOrder")
	f.down.Store(false)
	eventually(t, "recovery picks up the new tool", func() bool {
		_, ok := client.Lookup("work_orders.AdvanceOrder")
		return ok
	})
}

func TestRemoteHubClient_StartPolling_StopsWithContext(t *testing.T) {
	_, f, ts := newFlakyHub(t)
	client := hub.NewRemoteHubClient(ts.URL, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	client.StartPolling(ctx, 10*time.Millisecond)
	eventually(t, "some polls", func() bool { return f.calls.Load() >= 2 })
	cancel()
	time.Sleep(50 * time.Millisecond) // let an in-flight poll finish
	stopped := f.calls.Load()
	time.Sleep(100 * time.Millisecond)
	if got := f.calls.Load(); got != stopped {
		t.Fatalf("polling continued after cancel: %d -> %d calls", stopped, got)
	}
}

func TestRemoteHubClient_ListTools_RetainsLastValidOnTransientOutage(t *testing.T) {
	registry, f, ts := newFlakyHub(t)
	registerOrders(t, registry, "work_orders.CreateOrder")
	client := hub.NewRemoteHubClient(ts.URL, time.Second)
	actx := rpi.AdmitContext{Tenant: "acme"}

	tools, err := client.ListTools(context.Background(), actx)
	if err != nil || len(tools) != 1 {
		t.Fatalf("first ListTools: %v, %d tools", err, len(tools))
	}

	f.down.Store(true)
	tools, err = client.ListTools(context.Background(), actx)
	if err != nil || len(tools) != 1 {
		t.Fatalf("ListTools during outage must serve the retained catalogue: %v, %d tools", err, len(tools))
	}

	// Another tenant never had a catalogue: typed SUBSTRATE_UNAVAILABLE.
	_, err = client.ListTools(context.Background(), rpi.AdmitContext{Tenant: "other"})
	if !strings.Contains(taxonomy.CodeFromError(err), string(taxonomy.CodeSubstrateUnavailable)) {
		t.Fatalf("expected SUBSTRATE_UNAVAILABLE for a tenant without catalogue, got %v", err)
	}
}

func TestRemoteHubClient_ListTools_EmptyIsNotRetained(t *testing.T) {
	registry, _, ts := newFlakyHub(t)
	registerOrders(t, registry, "work_orders.CreateOrder")
	client := hub.NewRemoteHubClient(ts.URL, time.Second)
	actx := rpi.AdmitContext{Tenant: "acme"}

	if _, err := client.ListTools(context.Background(), actx); err != nil {
		t.Fatal(err)
	}
	registry.Unregister("pub-1", "work_orders.CreateOrder")
	_, err := client.ListTools(context.Background(), actx)
	if !strings.Contains(taxonomy.CodeFromError(err), string(taxonomy.CodeEmpty)) {
		t.Fatalf("a valid EMPTY answer must not be masked by the retained catalogue, got %v", err)
	}
}

func TestRemoteHubClient_StartPolling_NeverReachableKeepsLookupFalse(t *testing.T) {
	client := hub.NewRemoteHubClient("http://127.0.0.1:59999", 200*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartPolling(ctx, 20*time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	if _, ok := client.Lookup("work_orders.CreateOrder"); ok {
		t.Fatal("no catalogue ever obtained: Lookup must not offer tools")
	}
}

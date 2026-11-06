package hub_test

import (
	"context"
	"net/http/httptest"
	"strings"
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


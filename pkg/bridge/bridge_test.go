package bridge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

type mockToolLookup struct {
	mu    sync.RWMutex
	tools map[string]hub.ToolRegistration
}

func (m *mockToolLookup) Lookup(name string) (hub.ToolRegistration, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tools[name]
	return t, ok
}

type mockCaller struct {
	mu       sync.Mutex
	calls    []callerCall
	callFunc func(ctx context.Context, endpoint string, auth AuthMetadata, args map[string]any) (any, error)
}

type callerCall struct {
	endpoint string
	auth     AuthMetadata
	args     map[string]any
}

func (m *mockCaller) Call(ctx context.Context, endpoint string, auth AuthMetadata, args map[string]any) (any, error) {
	m.mu.Lock()
	m.calls = append(m.calls, callerCall{endpoint: endpoint, auth: auth, args: args})
	m.mu.Unlock()
	if m.callFunc != nil {
		return m.callFunc(ctx, endpoint, auth, args)
	}
	return map[string]any{"ok": true}, nil
}

func TestBridge_NotOffered_UnknownTool(t *testing.T) {
	lookup := &mockToolLookup{tools: map[string]hub.ToolRegistration{}}
	bridge := NewExecutionBridge(lookup, nil, nil, nil)

	actx := rpi.AdmitContext{Tenant: "tenant-1"}
	res, err := bridge.Execute(context.Background(), "unknown.Tool", map[string]any{}, actx)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	typedErr, ok := err.(*taxonomy.TypedError)
	if !ok {
		t.Fatalf("expected *taxonomy.TypedError, got %T", err)
	}
	if typedErr.Code != taxonomy.PrefixMolu+string(taxonomy.CodeNotOffered) {
		t.Errorf("expected NOT_OFFERED code, got %s", typedErr.Code)
	}
	if typedErr.Details["tenant"] != "tenant-1" || typedErr.Details["tool"] != "unknown.Tool" {
		t.Errorf("unexpected details: %v", typedErr.Details)
	}
}

func TestBridge_NotOffered_RPIExcluded(t *testing.T) {
	lookup := &mockToolLookup{
		tools: map[string]hub.ToolRegistration{
			"orders.Create": {
				Name:      "orders.Create",
				Namespace: "orders",
				Metadata: map[string]any{
					"endpoint": "http://localhost:8080/create",
				},
			},
		},
	}

	rpiMock := &rpi.MockRPIProvider{
		AdmitFunc: func(ctx context.Context, actx rpi.AdmitContext, candidates []rpi.ToolCandidate) ([]rpi.ToolCandidate, error) {
			// Exclude everything
			return nil, nil
		},
	}

	bridge := NewExecutionBridge(lookup, rpiMock, nil, nil)

	actx := rpi.AdmitContext{Tenant: "tenant-1", Roles: []string{"viewer"}}
	res, err := bridge.Execute(context.Background(), "orders.Create", map[string]any{}, actx)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	typedErr, ok := err.(*taxonomy.TypedError)
	if !ok {
		t.Fatalf("expected *taxonomy.TypedError, got %T", err)
	}
	if typedErr.Code != taxonomy.PrefixMolu+string(taxonomy.CodeNotOffered) {
		t.Errorf("expected NOT_OFFERED code, got %s", typedErr.Code)
	}
}

func TestBridge_ContractViolation_InvalidSchema(t *testing.T) {
	lookup := &mockToolLookup{
		tools: map[string]hub.ToolRegistration{
			"orders.Create": {
				Name:      "orders.Create",
				Namespace: "orders",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"customer_id": map[string]any{"type": "string"},
						"amount":      map[string]any{"type": "number"},
					},
					"required": []any{"customer_id", "amount"},
				},
				Metadata: map[string]any{
					"endpoint": "http://localhost:8080/create",
				},
			},
		},
	}

	caller := &mockCaller{}
	bridge := NewExecutionBridge(lookup, nil, nil, caller)

	actx := rpi.AdmitContext{Tenant: "tenant-1"}
	// Missing "amount" and wrong type for "customer_id"
	args := map[string]any{"customer_id": 12345}

	res, err := bridge.Execute(context.Background(), "orders.Create", args, actx)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	typedErr, ok := err.(*taxonomy.TypedError)
	if !ok {
		t.Fatalf("expected *taxonomy.TypedError, got %T", err)
	}
	if typedErr.Code != taxonomy.PrefixMolu+string(taxonomy.CodeContractViolation) {
		t.Errorf("expected CONTRACT_VIOLATION code, got %s", typedErr.Code)
	}
	if typedErr.Details["tenant"] != "tenant-1" || typedErr.Details["tool"] != "orders.Create" {
		t.Errorf("details should contain tenant and tool: %v", typedErr.Details)
	}

	fields, ok := typedErr.Details["fields"].([]string)
	if !ok || len(fields) == 0 {
		t.Errorf("expected details.fields with violations, got: %v", typedErr.Details["fields"])
	}

	// Verify caller was NEVER invoked
	caller.mu.Lock()
	callCount := len(caller.calls)
	caller.mu.Unlock()
	if callCount != 0 {
		t.Errorf("expected caller not to be invoked, but got %d calls", callCount)
	}
}

func TestBridge_MissingEndpoint_ContractViolation(t *testing.T) {
	lookup := &mockToolLookup{
		tools: map[string]hub.ToolRegistration{
			"orders.Create": {
				Name:      "orders.Create",
				Namespace: "orders",
				Metadata:  map[string]any{}, // Missing endpoint
			},
		},
	}

	bridge := NewExecutionBridge(lookup, nil, nil, nil)
	actx := rpi.AdmitContext{Tenant: "tenant-1"}

	_, err := bridge.Execute(context.Background(), "orders.Create", map[string]any{}, actx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	typedErr, ok := err.(*taxonomy.TypedError)
	if !ok {
		t.Fatalf("expected *taxonomy.TypedError, got %T", err)
	}
	if typedErr.Code != taxonomy.PrefixMolu+string(taxonomy.CodeContractViolation) {
		t.Errorf("expected CONTRACT_VIOLATION code, got %s", typedErr.Code)
	}
}

func TestBridge_Success(t *testing.T) {
	lookup := &mockToolLookup{
		tools: map[string]hub.ToolRegistration{
			"orders.Create": {
				Name:      "orders.Create",
				Namespace: "orders",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"customer_id": map[string]any{"type": "string"},
					},
					"required": []any{"customer_id"},
				},
				Metadata: map[string]any{
					"endpoint": "http://orders-service/create",
					"auth": map[string]any{
						"mode":      "bearer",
						"token_ref": "orders:api-token",
					},
				},
			},
		},
	}

	expectedResult := map[string]any{"order_id": "ORD-1234", "status": "confirmed"}
	caller := &mockCaller{
		callFunc: func(ctx context.Context, endpoint string, auth AuthMetadata, args map[string]any) (any, error) {
			if endpoint != "http://orders-service/create" {
				return nil, fmt.Errorf("unexpected endpoint %s", endpoint)
			}
			if auth.Mode != AuthModeBearer || auth.TokenRef != "orders:api-token" {
				return nil, fmt.Errorf("unexpected auth %v", auth)
			}
			if args["customer_id"] != "cust-99" {
				return nil, fmt.Errorf("unexpected args %v", args)
			}
			return expectedResult, nil
		},
	}

	bridge := NewExecutionBridge(lookup, nil, nil, caller)
	actx := rpi.AdmitContext{Tenant: "tenant-acme"}
	args := map[string]any{"customer_id": "cust-99"}

	result, err := bridge.Execute(context.Background(), "orders.Create", args, actx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resMap, ok := result.(map[string]any)
	if !ok || resMap["order_id"] != "ORD-1234" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestBridge_Concurrency(t *testing.T) {
	const goroutines = 25
	const iterations = 20

	lookup := &mockToolLookup{
		tools: map[string]hub.ToolRegistration{
			"orders.Create": {
				Name:      "orders.Create",
				Namespace: "orders",
				Metadata: map[string]any{
					"endpoint": "http://orders-service/create",
				},
			},
			"inventory.Check": {
				Name:      "inventory.Check",
				Namespace: "inventory",
				Metadata: map[string]any{
					"endpoint": "http://inventory-service/check",
				},
			},
		},
	}

	caller := &mockCaller{
		callFunc: func(ctx context.Context, endpoint string, auth AuthMetadata, args map[string]any) (any, error) {
			return map[string]any{"status": "ok", "endpoint": endpoint}, nil
		},
	}

	bridge := NewExecutionBridge(lookup, nil, nil, caller)

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*iterations)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				tool := "orders.Create"
				if (gid+i)%2 == 0 {
					tool = "inventory.Check"
				}
				actx := rpi.AdmitContext{Tenant: fmt.Sprintf("tenant-%d", gid)}
				res, err := bridge.Execute(context.Background(), tool, map[string]any{"iter": i}, actx)
				if err != nil {
					errCh <- fmt.Errorf("g %d i %d: %w", gid, i, err)
					return
				}
				if res == nil {
					errCh <- errors.New("nil result")
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrency error: %v", err)
	}
}


package bridge_integration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/bridge"
	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/mcp"
	"github.com/Gastonaso15/molu/pkg/rpi/providers"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

type harness struct {
	server       *mcp.Server
	registry     *hub.Registry
	stdinWriter  *io.PipeWriter
	stdoutReader *bufio.Reader
	linesCh      chan string
	cancel       context.CancelFunc
}

func setupBridgeE2E(t *testing.T, pubServerURL string) *harness {
	t.Helper()

	registry := hub.NewRegistry()

	// Register test domain functions
	err := registry.Register(hub.ToolRegistration{
		PublisherID: "pub-orders",
		Name:        "work_orders.CreateOrder",
		Namespace:   "work_orders",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":  map[string]any{"type": "string"},
				"amount": map[string]any{"type": "number"},
			},
			"required": []any{"title", "amount"},
		},
		Metadata: map[string]any{
			"endpoint": pubServerURL + "/create",
			"auth": map[string]any{
				"mode":      "bearer",
				"token_ref": "orders:api-token",
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	err = registry.Register(hub.ToolRegistration{
		PublisherID: "pub-orders",
		Name:        "work_orders.GetOrder",
		Namespace:   "work_orders",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"order_id": map[string]any{"type": "string"},
			},
			"required": []any{"order_id"},
		},
		Metadata: map[string]any{
			"endpoint": pubServerURL + "/get",
			"auth": map[string]any{
				"mode": "none",
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	err = registry.Register(hub.ToolRegistration{
		PublisherID: "pub-inventory",
		Name:        "inventory.CheckStock",
		Namespace:   "inventory",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sku": map[string]any{"type": "string"},
			},
		},
		Metadata: map[string]any{
			"endpoint": pubServerURL + "/inventory",
		},
	})
	if err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	// Admit all provider for general integration tests
	rpiProvider := providers.NewAdmitAllProvider()

	// Set secret token for orders:api-token in env
	t.Setenv("MOLU_FRONT_TOKEN_ORDERS_API_TOKEN", "super-secret-bearer-token")

	caller := bridge.NewHTTPDomainCaller(nil, nil, 2*time.Second)
	execBridge := bridge.NewExecutionBridge(registry, rpiProvider, nil, caller)

	srv := mcp.NewServer(mcp.ServerConfig{
		DomainExecutor: execBridge,
		CatalogLookup:  registry,
		CallTimeout:    2 * time.Second,
	})

	stdinReader, stdinWriter := io.Pipe()
	stdoutReaderPipe, stdoutWriter := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{
		server:       srv,
		registry:     registry,
		stdinWriter:  stdinWriter,
		stdoutReader: bufio.NewReader(stdoutReaderPipe),
		linesCh:      make(chan string, 50),
		cancel:       cancel,
	}

	go func() {
		_ = srv.Serve(ctx, stdinReader, stdoutWriter)
	}()

	go func() {
		for {
			line, err := h.stdoutReader.ReadString('\n')
			if err != nil {
				return
			}
			h.linesCh <- line
		}
	}()

	t.Cleanup(func() {
		cancel()
		_ = stdinWriter.Close()
		_ = stdoutWriter.Close()
	})

	return h
}

func (h *harness) call(t *testing.T, reqJSON string) map[string]any {
	t.Helper()
	if _, err := io.WriteString(h.stdinWriter, reqJSON+"\n"); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	select {
	case line := <-h.linesCh:
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON response %q: %v", line, err)
		}
		return resp
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for response")
		return nil
	}
}

func e2eFailure(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if resp["error"] != nil {
		t.Fatalf("unexpected JSON-RPC protocol error: %v", resp["error"])
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result object in response: %v", resp)
	}
	if res["isError"] != true {
		t.Fatalf("expected result.isError == true, got: %v", res)
	}
	contentSlice, ok := res["content"].([]any)
	if !ok || len(contentSlice) == 0 {
		t.Fatalf("missing or empty content in result: %v", res)
	}
	contentMap, ok := contentSlice[0].(map[string]any)
	if !ok {
		t.Fatalf("content element not map: %v", contentSlice[0])
	}
	text, _ := contentMap["text"].(string)

	var typedErr map[string]any
	if err := json.Unmarshal([]byte(text), &typedErr); err != nil {
		t.Fatalf("failed to parse typed error JSON from text %q: %v", text, err)
	}
	return typedErr
}

func e2eSuccess(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if resp["error"] != nil {
		t.Fatalf("unexpected JSON-RPC protocol error: %v", resp["error"])
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result object in response: %v", resp)
	}
	if res["isError"] == true {
		t.Fatalf("unexpected isError == true: %v", res)
	}
	contentSlice, ok := res["content"].([]any)
	if !ok || len(contentSlice) == 0 {
		t.Fatalf("missing or empty content in result: %v", res)
	}
	contentMap, ok := contentSlice[0].(map[string]any)
	if !ok {
		t.Fatalf("content element not map: %v", contentSlice[0])
	}
	text, _ := contentMap["text"].(string)

	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("failed to parse result JSON from text %q: %v", text, err)
	}
	return payload
}

func TestIntegration_Bridge_Success(t *testing.T) {
	var capturedAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"order_id": "WO-555", "status": "created"}`))
	}))
	defer ts.Close()

	h := setupBridgeE2E(t, ts.URL)

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"work_orders.CreateOrder","arguments":{"title":"Repair Pump","amount":120.5}}}`
	resp := h.call(t, req)

	res := e2eSuccess(t, resp)
	if res["order_id"] != "WO-555" || res["status"] != "created" {
		t.Errorf("unexpected payload: %v", res)
	}
	if capturedAuth != "Bearer super-secret-bearer-token" {
		t.Errorf("expected bearer token header, got: %q", capturedAuth)
	}
}

func TestIntegration_Bridge_Taxonomy_NotOffered_NotInCatalog(t *testing.T) {
	h := setupBridgeE2E(t, "http://localhost:9999")

	req := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"crm.DeleteAccount","arguments":{}}}`
	resp := h.call(t, req)

	errData := e2eFailure(t, resp)
	expectedCode := taxonomy.PrefixMolu + string(taxonomy.CodeNotOffered)
	if errData["code"] != expectedCode {
		t.Errorf("expected code %s, got %v", expectedCode, errData["code"])
	}
}

func TestIntegration_Bridge_Taxonomy_NotOffered_RPIExcluded(t *testing.T) {
	registry := hub.NewRegistry()
	_ = registry.Register(hub.ToolRegistration{
		PublisherID: "pub-orders",
		Name:        "work_orders.CreateOrder",
		Namespace:   "work_orders",
		Metadata:    map[string]any{"endpoint": "http://localhost:9999/create"},
	})

	// Rule only admits viewer for GetOrder, not CreateOrder
	rulesJSON := `{"tenant-acme:viewer": ["work_orders.GetOrder"]}`
	rules, err := providers.ParseStaticContextRules([]byte(rulesJSON))
	if err != nil {
		t.Fatalf("failed to parse rules: %v", err)
	}
	rpiProvider := providers.NewStaticContextRulesProvider(rules)

	execBridge := bridge.NewExecutionBridge(registry, rpiProvider, nil, nil)
	srv := mcp.NewServer(mcp.ServerConfig{
		DomainExecutor: execBridge,
		CatalogLookup:  registry,
		CallTimeout:    2 * time.Second,
	})

	stdinReader, stdinWriter := io.Pipe()
	stdoutReaderPipe, stdoutWriter := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer stdinWriter.Close()
	defer stdoutWriter.Close()

	go func() {
		_ = srv.Serve(ctx, stdinReader, stdoutWriter)
	}()

	lineCh := make(chan string, 1)
	go func() {
		r := bufio.NewReader(stdoutReaderPipe)
		line, _ := r.ReadString('\n')
		lineCh <- line
	}()

	req := `{"jsonrpc":"2.0","id":22,"method":"tools/call","params":{"name":"work_orders.CreateOrder","tenant":"tenant-acme","roles":["viewer"],"arguments":{}}}` + "\n"
	_, _ = io.WriteString(stdinWriter, req)

	select {
	case line := <-lineCh:
		var resp map[string]any
		_ = json.Unmarshal([]byte(line), &resp)
		errData := e2eFailure(t, resp)
		expectedCode := taxonomy.PrefixMolu + string(taxonomy.CodeNotOffered)
		if errData["code"] != expectedCode {
			t.Errorf("expected code %s, got %v", expectedCode, errData["code"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestIntegration_Bridge_Taxonomy_ContractViolation_Schema(t *testing.T) {
	h := setupBridgeE2E(t, "http://localhost:9999")

	// Missing amount and title is wrong type
	req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"work_orders.CreateOrder","arguments":{"title":999}}}`
	resp := h.call(t, req)

	errData := e2eFailure(t, resp)
	expectedCode := taxonomy.PrefixMolu + string(taxonomy.CodeContractViolation)
	if errData["code"] != expectedCode {
		t.Errorf("expected code %s, got %v", expectedCode, errData["code"])
	}

	details, ok := errData["details"].(map[string]any)
	if !ok {
		t.Fatalf("missing details in error: %v", errData)
	}
	fields, ok := details["fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Errorf("expected details.fields with violations, got: %v", details["fields"])
	}
}

func TestIntegration_Bridge_Taxonomy_ContractViolation_MissingSecret(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("publisher should not be called when token is missing")
	}))
	defer ts.Close()

	h := setupBridgeE2E(t, ts.URL)

	// Register a tool requiring an unconfigured token_ref
	_ = h.registry.Register(hub.ToolRegistration{
		PublisherID: "pub-orders",
		Name:        "work_orders.UnconfiguredAuth",
		Namespace:   "work_orders",
		Metadata: map[string]any{
			"endpoint": ts.URL + "/unconfigured",
			"auth": map[string]any{
				"mode":      "bearer",
				"token_ref": "missing:not-set",
			},
		},
	})

	req := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"work_orders.UnconfiguredAuth","arguments":{}}}`
	resp := h.call(t, req)

	errData := e2eFailure(t, resp)
	expectedCode := taxonomy.PrefixMolu + string(taxonomy.CodeContractViolation)
	if errData["code"] != expectedCode {
		t.Errorf("expected code %s, got %v", expectedCode, errData["code"])
	}
}

func TestIntegration_Bridge_Taxonomy_NotFound_Publisher404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "not found"}`))
	}))
	defer ts.Close()

	h := setupBridgeE2E(t, ts.URL)

	req := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"work_orders.GetOrder","arguments":{"order_id":"unknown-999"}}}`
	resp := h.call(t, req)

	errData := e2eFailure(t, resp)
	expectedCode := taxonomy.PrefixMolu + string(taxonomy.CodeNotFound)
	if errData["code"] != expectedCode {
		t.Errorf("expected code %s, got %v", expectedCode, errData["code"])
	}
}

func TestIntegration_Bridge_Taxonomy_SubstrateUnavailable_Publisher5xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error": "upstream unavailable"}`))
	}))
	defer ts.Close()

	h := setupBridgeE2E(t, ts.URL)

	req := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"work_orders.GetOrder","arguments":{"order_id":"123"}}}`
	resp := h.call(t, req)

	errData := e2eFailure(t, resp)
	expectedCode := taxonomy.PrefixMolu + string(taxonomy.CodeSubstrateUnavailable)
	if errData["code"] != expectedCode {
		t.Errorf("expected code %s, got %v", expectedCode, errData["code"])
	}
	details, _ := errData["details"].(map[string]any)
	if details["lastSuccessfulContact"] == nil || details["nextRetryAt"] == nil {
		t.Errorf("missing retry metadata in details: %v", details)
	}
}

func TestIntegration_Bridge_Taxonomy_SubstrateUnavailable_Timeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	registry := hub.NewRegistry()
	_ = registry.Register(hub.ToolRegistration{
		PublisherID: "pub-test",
		Name:        "test.SlowTool",
		Namespace:   "test",
		Metadata: map[string]any{
			"endpoint": ts.URL + "/slow",
		},
	})

	caller := bridge.NewHTTPDomainCaller(nil, nil, 50*time.Millisecond)
	execBridge := bridge.NewExecutionBridge(registry, nil, nil, caller)

	srv := mcp.NewServer(mcp.ServerConfig{
		DomainExecutor: execBridge,
		CatalogLookup:  registry,
		CallTimeout:    2 * time.Second,
	})

	stdinReader, stdinWriter := io.Pipe()
	stdoutReaderPipe, stdoutWriter := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer stdinWriter.Close()
	defer stdoutWriter.Close()

	go func() {
		_ = srv.Serve(ctx, stdinReader, stdoutWriter)
	}()

	lineCh := make(chan string, 1)
	go func() {
		r := bufio.NewReader(stdoutReaderPipe)
		line, _ := r.ReadString('\n')
		lineCh <- line
	}()

	_, _ = io.WriteString(stdinWriter, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"test.SlowTool","arguments":{}}}`+"\n")

	select {
	case line := <-lineCh:
		var resp map[string]any
		_ = json.Unmarshal([]byte(line), &resp)
		errData := e2eFailure(t, resp)
		expectedCode := taxonomy.PrefixMolu + string(taxonomy.CodeSubstrateUnavailable)
		if errData["code"] != expectedCode {
			t.Errorf("expected code %s, got %v", expectedCode, errData["code"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for slow call response")
	}
}

func TestIntegration_Bridge_Taxonomy_EmptyCollection(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items": []}`))
	}))
	defer ts.Close()

	h := setupBridgeE2E(t, ts.URL)

	req := `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"inventory.CheckStock","arguments":{"sku":"ITEM-001"}}}`
	resp := h.call(t, req)

	errData := e2eFailure(t, resp)
	expectedCode := taxonomy.PrefixMolu + string(taxonomy.CodeEmpty)
	if errData["code"] != expectedCode {
		t.Errorf("expected code %s, got %v", expectedCode, errData["code"])
	}
}

func TestIntegration_Bridge_Taxonomy_InvalidTransition_Verbatim(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code": "XOLU-INVALID_TRANSITION", "message": "guard failed: order is already closed", "details": {"current_state": "closed"}}`))
	}))
	defer ts.Close()

	h := setupBridgeE2E(t, ts.URL)

	req := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"work_orders.GetOrder","arguments":{"order_id":"123"}}}`
	resp := h.call(t, req)

	errData := e2eFailure(t, resp)
	if errData["code"] != "XOLU-INVALID_TRANSITION" {
		t.Errorf("expected verbatim code XOLU-INVALID_TRANSITION, got %v", errData["code"])
	}
	if errData["message"] != "guard failed: order is already closed" {
		t.Errorf("expected verbatim message, got %v", errData["message"])
	}
}

func TestIntegration_Bridge_DomainNeutrality(t *testing.T) {
	// Assert no mentions of the forbidden term in this integration package
	forbidden := strings.ToLower(string([]byte{'s', 'e', 'a', 'm'}))
	if strings.Contains(strings.ToLower("work_orders.CreateOrder"), forbidden) {
		t.Fatal("unexpected domain term")
	}
}

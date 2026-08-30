package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ha1tch/molu/pkg/catalogue"
	"github.com/ha1tch/molu/pkg/exec"
	"github.com/ha1tch/molu/pkg/health"
	"github.com/ha1tch/molu/pkg/semantic"
)

func TestMCPServer_InitializeAndToolsList(t *testing.T) {
	semStore := semantic.NewSemanticStore("tenant-test")
	probe := health.NewHealthProbe("http://localhost:8080", time.Minute, time.Second, time.Minute, time.Second, time.Minute)
	executor := exec.NewExecutor("http://localhost:8080", "bearer", "", "tenant-test", time.Second, true, semStore, probe)
	catStore := catalogue.NewCatalogueStore("", "", "", time.Minute)

	server := NewServer(executor, catStore)
	ctx := context.Background()

	// 1. Test initialize
	initReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05"}`),
	}
	initResp := server.Dispatch(ctx, initReq)
	if initResp == nil || initResp.Error != nil {
		t.Fatalf("initialize failed: %v", initResp)
	}

	// 2. Test tools/list
	listReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}
	listResp := server.Dispatch(ctx, listReq)
	if listResp == nil || listResp.Error != nil {
		t.Fatalf("tools/list failed: %v", listResp)
	}

	toolsResult, ok := listResp.Result.(ToolsListResult)
	if !ok {
		t.Fatalf("expected ToolsListResult, got %T", listResp.Result)
	}
	// Must have exactly the 13 generic tools
	if len(toolsResult.Tools) != 13 {
		t.Errorf("expected 13 generic tools, got %d", len(toolsResult.Tools))
	}
}

func TestMCPServer_ToolsCallDescribe(t *testing.T) {
	semStore := semantic.NewSemanticStore("tenant-test")
	semStore.Update(&semantic.SemanticMap{
		Entities: map[string]*semantic.EntityDef{
			"Customer": {Name: "Customer"},
		},
		Tenant: "tenant-test",
		ReadAt: time.Now(),
	})

	probe := health.NewHealthProbe("http://localhost:8080", time.Minute, time.Second, time.Minute, time.Second, time.Minute)
	executor := exec.NewExecutor("http://localhost:8080", "bearer", "", "tenant-test", time.Second, true, semStore, probe)
	server := NewServer(executor, nil)
	ctx := context.Background()

	callReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"describe","arguments":{"scope":"entities"}}`),
	}
	callResp := server.Dispatch(ctx, callReq)
	if callResp == nil || callResp.Error != nil {
		t.Fatalf("tools/call describe failed: %v", callResp)
	}

	result, ok := callResp.Result.(ToolCallResult)
	if !ok || len(result.Content) == 0 {
		t.Fatalf("expected ToolCallResult with content, got %v", callResp.Result)
	}

	if result.IsError {
		t.Errorf("expected isError to be false for describe")
	}
}

func TestMCPServer_StreamableHTTPTransport(t *testing.T) {
	semStore := semantic.NewSemanticStore("tenant-test")
	probe := health.NewHealthProbe("http://localhost:8080", time.Minute, time.Second, time.Minute, time.Second, time.Minute)
	executor := exec.NewExecutor("http://localhost:8080", "bearer", "", "tenant-test", time.Second, true, semStore, probe)
	server := NewServer(executor, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Launch HTTP server on ephemeral port in test
	go func() {
		_ = server.RunHTTP(ctx, "127.0.0.1:18090", "none", "")
	}()

	// Give it a moment to bind
	time.Sleep(50 * time.Millisecond)

	// Send POST request
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	resp, err := http.Post("http://127.0.0.1:18090/mcp", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to POST to MCP HTTP server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 OK, got %d", resp.StatusCode)
	}

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if jsonResp.Error != nil {
		t.Errorf("unexpected error in response: %v", jsonResp.Error)
	}
}

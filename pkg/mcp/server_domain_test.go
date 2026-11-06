package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

type mockDomainLookup struct {
	tools map[string]hub.ToolRegistration
}

func (m *mockDomainLookup) Lookup(name string) (hub.ToolRegistration, bool) {
	t, ok := m.tools[name]
	return t, ok
}

type mockDomainExecutor struct {
	executeFunc func(ctx context.Context, toolName string, args map[string]any, actx rpi.AdmitContext) (any, error)
}

func (m *mockDomainExecutor) Execute(ctx context.Context, toolName string, args map[string]any, actx rpi.AdmitContext) (any, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, toolName, args, actx)
	}
	return map[string]any{"status": "ok"}, nil
}

func TestServer_Domain_Success(t *testing.T) {
	lookup := &mockDomainLookup{
		tools: map[string]hub.ToolRegistration{
			"work_orders.CreateOrder": {
				Name:      "work_orders.CreateOrder",
				Namespace: "work_orders",
			},
		},
	}

	var capturedTool string
	var capturedArgs map[string]any

	executor := &mockDomainExecutor{
		executeFunc: func(ctx context.Context, toolName string, args map[string]any, actx rpi.AdmitContext) (any, error) {
			capturedTool = toolName
			capturedArgs = args
			return map[string]any{"order_id": "WO-101", "created": true}, nil
		},
	}

	srv := NewServer(ServerConfig{
		DomainExecutor: executor,
		CatalogLookup:  lookup,
	})

	callReq := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"work_orders.CreateOrder","arguments":{"amount":250}}}`
	var out bytes.Buffer
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if capturedTool != "work_orders.CreateOrder" {
		t.Errorf("expected tool 'work_orders.CreateOrder', got %q", capturedTool)
	}
	if capturedArgs["amount"] != float64(250) && capturedArgs["amount"] != 250 {
		t.Errorf("expected amount 250, got %v", capturedArgs["amount"])
	}

	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v, body: %s", err, out.String())
	}

	if resp.Result.IsError {
		t.Errorf("expected isError=false, got true: %s", out.String())
	}
	if len(resp.Result.Content) == 0 {
		t.Fatalf("empty content in result")
	}
	if !strings.Contains(resp.Result.Content[0].Text, "WO-101") {
		t.Errorf("expected WO-101 in text, got: %s", resp.Result.Content[0].Text)
	}
}

func TestServer_Domain_UnknownToolNotOffered(t *testing.T) {
	lookup := &mockDomainLookup{tools: map[string]hub.ToolRegistration{}}
	executor := &mockDomainExecutor{}

	srv := NewServer(ServerConfig{
		DomainExecutor: executor,
		CatalogLookup:  lookup,
	})

	callReq := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"work_orders.Unknown","arguments":{}}}`
	var out bytes.Buffer
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !resp.Result.IsError {
		t.Errorf("expected isError=true for unknown domain tool")
	}
	if !strings.Contains(resp.Result.Content[0].Text, string(taxonomy.CodeNotOffered)) {
		t.Errorf("expected NOT_OFFERED code in error text, got: %s", resp.Result.Content[0].Text)
	}
}

func TestServer_Domain_ContractViolationPropagated(t *testing.T) {
	lookup := &mockDomainLookup{
		tools: map[string]hub.ToolRegistration{
			"work_orders.CreateOrder": {Name: "work_orders.CreateOrder"},
		},
	}
	executor := &mockDomainExecutor{
		executeFunc: func(ctx context.Context, toolName string, args map[string]any, actx rpi.AdmitContext) (any, error) {
			return nil, taxonomy.NewContractViolation("tenant-1", toolName, []string{"amount"})
		},
	}

	srv := NewServer(ServerConfig{
		DomainExecutor: executor,
		CatalogLookup:  lookup,
	})

	callReq := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"work_orders.CreateOrder","arguments":{"amount":"not-a-number"}}}`
	var out bytes.Buffer
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !resp.Result.IsError {
		t.Errorf("expected isError=true")
	}
	if !strings.Contains(resp.Result.Content[0].Text, string(taxonomy.CodeContractViolation)) {
		t.Errorf("expected CONTRACT_VIOLATION in text, got: %s", resp.Result.Content[0].Text)
	}
	if !strings.Contains(resp.Result.Content[0].Text, "amount") {
		t.Errorf("expected field 'amount' in details, got: %s", resp.Result.Content[0].Text)
	}
}

func TestServer_Domain_NativePrimitivesUninterfered(t *testing.T) {
	lookup := &mockDomainLookup{
		tools: map[string]hub.ToolRegistration{
			"work_orders.CreateOrder": {Name: "work_orders.CreateOrder"},
		},
	}
	domainExecCalled := false
	executor := &mockDomainExecutor{
		executeFunc: func(ctx context.Context, toolName string, args map[string]any, actx rpi.AdmitContext) (any, error) {
			domainExecCalled = true
			return nil, fmt.Errorf("should not be called for native primitives")
		},
	}

	srv := NewServer(ServerConfig{
		DomainExecutor: executor,
		CatalogLookup:  lookup,
	})

	// Invoke native describe primitive (no namespace)
	callReq := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"describe","arguments":{"scope":"all"}}}`
	var out bytes.Buffer
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if domainExecCalled {
		t.Errorf("domain executor should not be called for native primitive 'describe'")
	}

	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.Result.IsError {
		t.Errorf("expected describe to succeed, got isError=true: %s", out.String())
	}
	if !strings.Contains(resp.Result.Content[0].Text, "describe") {
		t.Errorf("expected describe in output: %s", resp.Result.Content[0].Text)
	}
}


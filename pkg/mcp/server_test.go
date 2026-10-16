package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Gastonaso15/molu/pkg/schema"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

type mockPrimitiveProvider struct {
	primitives []schema.PrimitiveSchema
	validateFn func(primitiveName string, input map[string]interface{}) *taxonomy.TypedError
}

func (m *mockPrimitiveProvider) GetPrimitives() []schema.PrimitiveSchema {
	return m.primitives
}

func (m *mockPrimitiveProvider) ValidateInput(primitiveName string, input map[string]interface{}) *taxonomy.TypedError {
	if m.validateFn != nil {
		return m.validateFn(primitiveName, input)
	}
	return nil
}

type mockHealthChecker struct {
	checkFn func() error
}

func (m *mockHealthChecker) Check() error {
	if m.checkFn != nil {
		return m.checkFn()
	}
	return nil
}

type mockSubstrateExecutor struct {
	executeFn func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error)
}

func (m *mockSubstrateExecutor) Execute(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, primitive, args)
	}
	return map[string]interface{}{"status": "ok"}, nil
}

func TestServer_InitializeAndListTools(t *testing.T) {
	prims := []schema.PrimitiveSchema{
		{
			Name:        "walk",
			Description: "Executes an FSM transition",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":    map[string]interface{}{"type": "string"},
					"event": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"id", "event"},
			},
		},
		{
			Name:        "find",
			Description: "Searches entities in substrate",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
				},
			},
		},
	}

	provider := &mockPrimitiveProvider{primitives: prims}
	srv := NewServer(ServerConfig{
		Provider: provider,
	})

	// 1. Initialize request
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}`
	var out bytes.Buffer
	err := srv.HandleMessage(context.Background(), []byte(initReq), &out)
	if err != nil {
		t.Fatalf("unexpected error handling initialize: %v", err)
	}

	var initResp map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &initResp); err != nil {
		t.Fatalf("failed to parse initialize response: %v", err)
	}
	result, ok := initResp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result in response, got %v", initResp)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocolVersion 2024-11-05, got %v", result["protocolVersion"])
	}

	// 2. Tools/list request
	out.Reset()
	listReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	err = srv.HandleMessage(context.Background(), []byte(listReq), &out)
	if err != nil {
		t.Fatalf("unexpected error handling tools/list: %v", err)
	}

	var listResp map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to parse tools/list response: %v", err)
	}
	listResult, ok := listResp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result in list response, got %v", listResp)
	}
	tools, ok := listResult["tools"].([]interface{})
	if !ok {
		t.Fatalf("expected tools array, got %v", listResult["tools"])
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	tool0 := tools[0].(map[string]interface{})
	if tool0["name"] != "walk" {
		t.Errorf("expected tool name 'walk', got %v", tool0["name"])
	}
	if tool0["description"] != "Executes an FSM transition" {
		t.Errorf("expected description 'Executes an FSM transition', got %v", tool0["description"])
	}
	// Verify strict no namespace
	for _, toolItem := range tools {
		toolMap := toolItem.(map[string]interface{})
		name := toolMap["name"].(string)
		if strings.Contains(name, ".") {
			t.Errorf("generic primitive %q MUST NOT contain namespace dots", name)
		}
	}
}

func TestServer_ListTools_Empty(t *testing.T) {
	provider := &mockPrimitiveProvider{primitives: nil}
	srv := NewServer(ServerConfig{
		Provider: provider,
	})

	var out bytes.Buffer
	listReq := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	err := srv.HandleMessage(context.Background(), []byte(listReq), &out)
	if err != nil {
		t.Fatalf("unexpected error handling tools/list: %v", err)
	}

	var listResp map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to parse tools/list response: %v", err)
	}
	listResult := listResp["result"].(map[string]interface{})
	tools := listResult["tools"].([]interface{})
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for empty catalog, got %d", len(tools))
	}
}

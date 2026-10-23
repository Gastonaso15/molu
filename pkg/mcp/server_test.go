package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestServer_CallTool_SubstrateUnavailable(t *testing.T) {
	healthErr := taxonomy.NewSubstrateUnavailable("2026-10-23T12:00:00Z", "2026-10-23T12:00:05Z", 2, 5)
	health := &mockHealthChecker{
		checkFn: func() error {
			return healthErr
		},
	}
	srv := NewServer(ServerConfig{
		Health: health,
	})

	var out bytes.Buffer
	callReq := `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"walk","arguments":{"machine_id":"m1","input":"advance"}}}`
	err := srv.HandleMessage(context.Background(), []byte(callReq), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected error response, got result: %v", resp.Result)
	}
	if resp.Error.Code != -32000 {
		t.Errorf("expected JSON-RPC code -32000, got %d", resp.Error.Code)
	}

	dataMap, ok := resp.Error.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected error.data to be object, got %T: %v", resp.Error.Data, resp.Error.Data)
	}
	if dataMap["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeSubstrateUnavailable) {
		t.Errorf("expected code %s, got %v", taxonomy.PrefixMolu+taxonomy.CodeSubstrateUnavailable, dataMap["code"])
	}
}

func TestServer_CallTool_NotOffered(t *testing.T) {
	health := &mockHealthChecker{
		checkFn: func() error { return nil },
	}
	srv := NewServer(ServerConfig{
		Health: health,
	})

	var out bytes.Buffer
	callReq := `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"unknown_tool","arguments":{}}}`
	err := srv.HandleMessage(context.Background(), []byte(callReq), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected error response, got result: %v", resp.Result)
	}
	if resp.Error.Code != -32000 {
		t.Errorf("expected JSON-RPC code -32000, got %d", resp.Error.Code)
	}

	dataMap, ok := resp.Error.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected error.data to be object, got %T", resp.Error.Data)
	}
	if dataMap["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeNotOffered) {
		t.Errorf("expected code %s, got %v", taxonomy.PrefixMolu+taxonomy.CodeNotOffered, dataMap["code"])
	}
}

func TestServer_CallTool_ContractViolation(t *testing.T) {
	srv := NewServer(ServerConfig{})

	tests := []struct {
		name         string
		tool         string
		argsJSON     string
		expectedCode string
	}{
		{
			name:         "create missing data and entity_type",
			tool:         "create",
			argsJSON:     `{}`,
			expectedCode: string(taxonomy.PrefixMolu + taxonomy.CodeContractViolation),
		},
		{
			name:         "get missing id",
			tool:         "get",
			argsJSON:     `{"entity_type":"customer"}`,
			expectedCode: string(taxonomy.PrefixMolu + taxonomy.CodeContractViolation),
		},
		{
			name:         "walk missing input",
			tool:         "walk",
			argsJSON:     `{"machine_id":"m1"}`,
			expectedCode: string(taxonomy.PrefixMolu + taxonomy.CodeContractViolation),
		},
		{
			name:         "cal_check invalid calendars type",
			tool:         "cal_check",
			argsJSON:     `{"calendars":"not-an-array","start":"2026-10-23T10:00:00Z","end":"2026-10-23T11:00:00Z"}`,
			expectedCode: string(taxonomy.PrefixMolu + taxonomy.CodeContractViolation),
		},
		{
			name:         "cal_confirm missing booking_id",
			tool:         "cal_confirm",
			argsJSON:     `{}`,
			expectedCode: string(taxonomy.PrefixMolu + taxonomy.CodeContractViolation),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			callReq := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tc.tool + `","arguments":` + tc.argsJSON + `}}`
			var out bytes.Buffer
			err := srv.HandleMessage(context.Background(), []byte(callReq), &out)
			if err != nil {
				t.Fatalf("unexpected handle error: %v", err)
			}

			var resp jsonRPCResponse
			if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
				t.Fatalf("failed to parse response: %v", err)
			}
			if resp.Error == nil {
				t.Fatalf("expected error response, got result: %v", resp.Result)
			}
			if resp.Error.Code != -32602 {
				t.Errorf("expected JSON-RPC code -32602 (Invalid params), got %d", resp.Error.Code)
			}

			dataMap, ok := resp.Error.Data.(map[string]interface{})
			if !ok {
				t.Fatalf("expected error.data to be object, got %T: %v", resp.Error.Data, resp.Error.Data)
			}
			if dataMap["code"] != tc.expectedCode {
				t.Errorf("expected code %s, got %v", tc.expectedCode, dataMap["code"])
			}
			details, ok := dataMap["details"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected details in error.data, got %v", dataMap)
			}
			fields, ok := details["fields"].([]interface{})
			if !ok || len(fields) == 0 {
				t.Errorf("expected non-empty fields array in details, got %v", details["fields"])
			}
		})
	}
}

func TestServer_CallTool_Success(t *testing.T) {
	srv := NewServer(ServerConfig{})

	callReq := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get","arguments":{"entity_type":"user","id":"123"}}}`
	var out bytes.Buffer
	err := srv.HandleMessage(context.Background(), []byte(callReq), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result object, got %T: %v", resp.Result, resp.Result)
	}
	content, ok := resMap["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Errorf("expected non-empty content in result, got %v", resMap)
	}
}

func TestServer_CallTool_T5_Entities(t *testing.T) {
	mockExec := &mockSubstrateExecutor{}
	srv := NewServer(ServerConfig{
		Executor: mockExec,
	})

	// 1. get -> 404 maps to NOT_FOUND
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("404 entity not found")
	}
	callReq := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get","arguments":{"entity_type":"asset","id":"ast-99"}}}`
	var out bytes.Buffer
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp jsonRPCResponse
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatalf("expected NOT_FOUND error, got nil")
	}
	data := resp.Error.Data.(map[string]interface{})
	if data["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeNotFound) {
		t.Errorf("expected NOT_FOUND code, got %v", data["code"])
	}

	// 2. list -> empty results maps to EMPTY
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"items": []interface{}{}}, nil
	}
	out.Reset()
	callReq = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list","arguments":{"entity_type":"asset"}}}`
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatalf("expected EMPTY error, got nil")
	}
	data = resp.Error.Data.(map[string]interface{})
	if data["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeEmpty) {
		t.Errorf("expected EMPTY code, got %v", data["code"])
	}

	// 3. create -> success
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"id": "asset-1", "created": true}, nil
	}
	out.Reset()
	callReq = `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create","arguments":{"entity_type":"asset","data":{"name":"pump"}}}}`
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp = jsonRPCResponse{}
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// 4. update -> success
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"id": "asset-1", "updated": true}, nil
	}
	out.Reset()
	callReq = `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"update","arguments":{"entity_type":"asset","id":"asset-1","changes":{"name":"pump2"}}}}`
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp = jsonRPCResponse{}
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestServer_CallTool_T5_FSM(t *testing.T) {
	mockExec := &mockSubstrateExecutor{}
	srv := NewServer(ServerConfig{
		Executor: mockExec,
	})

	// 1. walk -> guard failure maps to INVALID_TRANSITION verbatim
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return nil, &taxonomy.TypedError{
			Code:    taxonomy.PrefixXolu + string(taxonomy.CodeInvalidTransition),
			Message: "guard condition failed: payload.temp > 100",
		}
	}
	callReq := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"walk","arguments":{"machine_id":"m-1","input":"START"}}}`
	var out bytes.Buffer
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp jsonRPCResponse
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatalf("expected INVALID_TRANSITION error, got nil")
	}
	data := resp.Error.Data.(map[string]interface{})
	if data["code"] != string(taxonomy.PrefixXolu+taxonomy.CodeInvalidTransition) {
		t.Errorf("expected verbatim XOLU-INVALID_TRANSITION code, got %v", data["code"])
	}
	if !strings.Contains(resp.Error.Message, "guard condition failed") {
		t.Errorf("expected verbatim message, got %v", resp.Error.Message)
	}

	// 2. machine_state -> 404 maps to NOT_FOUND
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("machine 404 not found")
	}
	out.Reset()
	callReq = `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"machine_state","arguments":{"machine_id":"m-nonexistent"}}}`
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatalf("expected NOT_FOUND error, got nil")
	}
	data = resp.Error.Data.(map[string]interface{})
	if data["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeNotFound) {
		t.Errorf("expected NOT_FOUND code, got %v", data["code"])
	}

	// 3. machine_history -> empty maps to EMPTY
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"history": []interface{}{}}, nil
	}
	out.Reset()
	callReq = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"machine_history","arguments":{"machine_id":"m-1"}}}`
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatalf("expected EMPTY error, got nil")
	}
	data = resp.Error.Data.(map[string]interface{})
	if data["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeEmpty) {
		t.Errorf("expected EMPTY code, got %v", data["code"])
	}
}

func TestServer_CallTool_T5_QueryAndCalendars(t *testing.T) {
	mockExec := &mockSubstrateExecutor{}
	srv := NewServer(ServerConfig{
		Executor: mockExec,
	})

	// 1. query -> empty maps to EMPTY
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"rows": []interface{}{}}, nil
	}
	callReq := `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"query","arguments":{"language":"oql","query":"SELECT * FROM asset"}}}`
	var out bytes.Buffer
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp jsonRPCResponse
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatalf("expected EMPTY error, got nil")
	}
	data := resp.Error.Data.(map[string]interface{})
	if data["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeEmpty) {
		t.Errorf("expected EMPTY code, got %v", data["code"])
	}

	// 2. cal_check -> success
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"available": true}, nil
	}
	out.Reset()
	callReq = `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"cal_check","arguments":{"calendars":["c1"],"start":"2026-10-23T10:00:00Z","end":"2026-10-23T11:00:00Z"}}}`
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp = jsonRPCResponse{}
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// 3. cal_openings -> empty maps to EMPTY
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"openings": []interface{}{}}, nil
	}
	out.Reset()
	callReq = `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"cal_openings","arguments":{"calendars":["c1"],"duration":"PT1H","from":"2026-10-23T10:00:00Z","to":"2026-10-23T18:00:00Z"}}}`
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp = jsonRPCResponse{}
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatalf("expected EMPTY error, got nil")
	}
	data = resp.Error.Data.(map[string]interface{})
	if data["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeEmpty) {
		t.Errorf("expected EMPTY code, got %v", data["code"])
	}

	// 4. cal_propose -> returns booking_id
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"booking_id": "bk-123"}, nil
	}
	out.Reset()
	callReq = `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"cal_propose","arguments":{"calendars":["c1"],"start":"2026-10-23T10:00:00Z","end":"2026-10-23T11:00:00Z"}}}`
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp = jsonRPCResponse{}
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// 5. cal_confirm -> 404 maps to NOT_FOUND
	mockExec.executeFn = func(ctx context.Context, primitive string, args map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("404 booking not found")
	}
	out.Reset()
	callReq = `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"cal_confirm","arguments":{"booking_id":"bk-invalid"}}}`
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatalf("expected NOT_FOUND error, got nil")
	}
	data = resp.Error.Data.(map[string]interface{})
	if data["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeNotFound) {
		t.Errorf("expected NOT_FOUND code, got %v", data["code"])
	}
}

func TestServer_CallTool_T5_Describe(t *testing.T) {
	srv := NewServer(ServerConfig{})

	callReq := `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"describe","arguments":{"scope":"entities"}}}`
	var out bytes.Buffer
	if err := srv.HandleMessage(context.Background(), []byte(callReq), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp jsonRPCResponse
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result map, got %v", resp.Result)
	}
	content := resMap["content"].([]interface{})
	if len(content) == 0 {
		t.Errorf("expected non-empty content in describe result")
	}
}

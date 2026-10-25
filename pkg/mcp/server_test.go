package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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
	dataMap := toolFailure(t, resp)
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
	dataMap := toolFailure(t, resp)
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
			dataMap := toolFailure(t, resp)
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
	data := toolFailure(t, resp)
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
	data = toolFailure(t, resp)
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
	data := toolFailure(t, resp)
	if data["code"] != string(taxonomy.PrefixXolu+taxonomy.CodeInvalidTransition) {
		t.Errorf("expected verbatim XOLU-INVALID_TRANSITION code, got %v", data["code"])
	}
	if !strings.Contains(data["message"].(string), "guard condition failed") {
		t.Errorf("expected verbatim message, got %v", data["message"])
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
	data = toolFailure(t, resp)
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
	data = toolFailure(t, resp)
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
	data := toolFailure(t, resp)
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
	data = toolFailure(t, resp)
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
	data = toolFailure(t, resp)
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

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Bytes()
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestServer_UpdatePrimitives_Notification(t *testing.T) {
	srv := NewServer(ServerConfig{})
	var out safeBuffer
	srv.SetOutput(&out)

	newPrims := []schema.PrimitiveSchema{
		{
			Name:        "describe",
			Description: "Semantic map",
			InputSchema: map[string]interface{}{"type": "object"},
		},
	}

	srv.UpdatePrimitives(newPrims)

	tools := srv.HandleListTools(context.Background())
	if len(tools) != 1 || tools[0].Name != "describe" {
		t.Fatalf("expected 1 tool 'describe', got %v", tools)
	}

	// Verify notification written to out
	var notif map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &notif); err != nil {
		t.Fatalf("expected valid JSON notification in output, got %q: %v", out.String(), err)
	}
	if notif["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got %v", notif["jsonrpc"])
	}
	if notif["method"] != "notifications/tools/list_changed" {
		t.Errorf("expected method 'notifications/tools/list_changed', got %v", notif["method"])
	}
}

func TestServer_Concurrency(t *testing.T) {
	mockExec := &mockSubstrateExecutor{}
	srv := NewServer(ServerConfig{
		Executor: mockExec,
	})
	safeOut := &safeBuffer{}
	srv.SetOutput(safeOut)

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Readers: HandleListTools & tools/list via HandleMessage
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_ = srv.HandleListTools(ctx)
					req := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
					_ = srv.HandleMessage(ctx, []byte(req), safeOut)
				}
			}
		}()
	}

	// Callers: tools/call via HandleMessage
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					reqValid := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"describe","arguments":{"scope":"all"}}}`
					_ = srv.HandleMessage(ctx, []byte(reqValid), safeOut)

					reqInvalid := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"unknown","arguments":{}}}`
					_ = srv.HandleMessage(ctx, []byte(reqInvalid), safeOut)
				}
			}
		}()
	}

	// Writers: UpdatePrimitives
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			toggle := false
			for {
				select {
				case <-ctx.Done():
					return
				default:
					if toggle {
						srv.UpdatePrimitives(StandardPrimitives())
					} else {
						srv.UpdatePrimitives([]schema.PrimitiveSchema{
							{
								Name:        "describe",
								Description: "Dynamic description",
								InputSchema: map[string]interface{}{"type": "object"},
							},
						})
					}
					toggle = !toggle
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
}


type statusError struct{ status int }

func (e *statusError) Error() string   { return "xolu error" }
func (e *statusError) HTTPStatus() int { return e.status }

func TestServer_CallTool_NotFoundFromHTTPStatus(t *testing.T) {
	cases := []struct {
		primitive string
		args      string
	}{
		{"walk", `{"machine_id":"m-9","input":"GO"}`},
		{"machine_history", `{"machine_id":"m-9"}`},
		{"machine_state", `{"machine_id":"m-9"}`},
	}
	for _, tc := range cases {
		t.Run(tc.primitive, func(t *testing.T) {
			srv := NewServer(ServerConfig{Executor: &mockSubstrateExecutor{
				executeFn: func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
					return nil, &statusError{status: 404}
				},
			}})
			req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tc.primitive + `","arguments":` + tc.args + `}}`
			var out bytes.Buffer
			if err := srv.HandleMessage(context.Background(), []byte(req), &out); err != nil {
				t.Fatal(err)
			}
			var resp jsonRPCResponse
			json.Unmarshal(out.Bytes(), &resp)
			data := toolFailure(t, resp)
			if data["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeNotFound) {
				t.Fatalf("expected NOT_FOUND, got %v (%v)", data["code"], data["message"])
			}
			if msg, _ := data["message"].(string); !strings.Contains(msg, "m-9") {
				t.Errorf("NOT_FOUND must name the machine, got %q", msg)
			}
		})
	}
}

func TestServer_CallTool_ListEmptyUsesXoluDataEnvelope(t *testing.T) {
	cases := map[string]struct {
		res     map[string]interface{}
		isEmpty bool
	}{
		"empty data":     {map[string]interface{}{"data": []interface{}{}}, true},
		"zero total":     {map[string]interface{}{"data": []interface{}{}, "pagination": map[string]interface{}{"total_items": float64(0)}}, true},
		"data with item": {map[string]interface{}{"data": []interface{}{map[string]interface{}{"id": 1}}}, false},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			srv := NewServer(ServerConfig{Executor: &mockSubstrateExecutor{
				executeFn: func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
					return tc.res, nil
				},
			}})
			req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list","arguments":{"entity_type":"asset"}}}`
			var out bytes.Buffer
			if err := srv.HandleMessage(context.Background(), []byte(req), &out); err != nil {
				t.Fatal(err)
			}
			var resp jsonRPCResponse
			json.Unmarshal(out.Bytes(), &resp)
			gotEmpty := false
			if r, ok := resp.Result.(map[string]interface{}); ok && r["isError"] == true {
				gotEmpty = toolFailure(t, resp)["code"] == string(taxonomy.PrefixMolu+taxonomy.CodeEmpty)
			}
			if gotEmpty != tc.isEmpty {
				t.Fatalf("expected EMPTY=%v, got response %s", tc.isEmpty, out.String())
			}
		})
	}
}

// toolFailure returns the typed error carried by an isError tools/call result.
// An untyped failure carries only its message.
func toolFailure(t *testing.T, resp jsonRPCResponse) map[string]interface{} {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("a tool failure must be an isError result, got JSON-RPC error %+v", resp.Error)
	}
	res, ok := resp.Result.(map[string]interface{})
	if !ok || res["isError"] != true {
		t.Fatalf("expected an isError result, got %v", resp.Result)
	}
	content, _ := res["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected one content item, got %v", res["content"])
	}
	text, _ := content[0].(map[string]interface{})["text"].(string)
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return map[string]interface{}{"message": text}
	}
	return data
}

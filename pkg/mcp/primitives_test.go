package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Gastonaso15/molu/pkg/schema"
)

func TestStandardPrimitives_CountAndNames(t *testing.T) {
	prims := StandardPrimitives()
	if len(prims) != 13 {
		t.Fatalf("expected exactly 13 standard primitives, got %d", len(prims))
	}

	expectedNames := []string{
		"describe",
		"get",
		"list",
		"create",
		"update",
		"query",
		"walk",
		"machine_state",
		"machine_history",
		"cal_check",
		"cal_openings",
		"cal_propose",
		"cal_confirm",
	}

	nameMap := make(map[string]schema.PrimitiveSchema)
	for _, p := range prims {
		// RF-8 and convention: NUNCA llevan namespace
		if strings.Contains(p.Name, ".") {
			t.Errorf("primitive %q must NOT contain namespace dots", p.Name)
		}
		// Neutralidad de dominio: sin menciones a seam
		if strings.Contains(strings.ToLower(p.Name), "seam") {
			t.Errorf("primitive %q must NOT mention seam", p.Name)
		}
		if strings.Contains(strings.ToLower(p.Description), "seam") {
			t.Errorf("primitive %q description must NOT mention seam", p.Name)
		}
		if p.Description == "" {
			t.Errorf("primitive %q has empty description", p.Name)
		}
		if p.InputSchema == nil {
			t.Errorf("primitive %q has nil inputSchema", p.Name)
		}
		if p.InputSchema["type"] != "object" {
			t.Errorf("primitive %q inputSchema type must be 'object', got %v", p.Name, p.InputSchema["type"])
		}
		nameMap[p.Name] = p
	}

	for _, name := range expectedNames {
		if _, ok := nameMap[name]; !ok {
			t.Errorf("expected standard primitive %q not found", name)
		}
	}
}

func TestStandardPrimitives_SchemasDetail(t *testing.T) {
	prims := StandardPrimitives()
	lookup := make(map[string]schema.PrimitiveSchema)
	for _, p := range prims {
		lookup[p.Name] = p
	}

	// 1. describe: scope enum, optional name
	desc := lookup["describe"]
	props := desc.InputSchema["properties"].(map[string]interface{})
	scopeProp := props["scope"].(map[string]interface{})
	if scopeProp["type"] != "string" {
		t.Errorf("describe.scope type expected string, got %v", scopeProp["type"])
	}
	enums := scopeProp["enum"].([]interface{})
	if len(enums) != 5 {
		t.Errorf("describe.scope enum expected 5 items, got %d", len(enums))
	}

	// 2. get: required entity_type, id
	getPrim := lookup["get"]
	reqGet := getPrim.InputSchema["required"].([]interface{})
	if len(reqGet) != 2 || reqGet[0] != "entity_type" || reqGet[1] != "id" {
		t.Errorf("get required fields expected [entity_type, id], got %v", reqGet)
	}

	// 3. list: required entity_type
	listPrim := lookup["list"]
	reqList := listPrim.InputSchema["required"].([]interface{})
	if len(reqList) != 1 || reqList[0] != "entity_type" {
		t.Errorf("list required fields expected [entity_type], got %v", reqList)
	}

	// 4. create: required entity_type, data
	createPrim := lookup["create"]
	reqCreate := createPrim.InputSchema["required"].([]interface{})
	if len(reqCreate) != 2 || reqCreate[0] != "entity_type" || reqCreate[1] != "data" {
		t.Errorf("create required fields expected [entity_type, data], got %v", reqCreate)
	}

	// 5. update: required entity_type, id, changes
	updatePrim := lookup["update"]
	reqUpdate := updatePrim.InputSchema["required"].([]interface{})
	if len(reqUpdate) != 3 {
		t.Errorf("update required fields expected 3 items, got %v", reqUpdate)
	}

	// 6. query: required language, query
	queryPrim := lookup["query"]
	reqQuery := queryPrim.InputSchema["required"].([]interface{})
	if len(reqQuery) != 2 || reqQuery[0] != "language" || reqQuery[1] != "query" {
		t.Errorf("query required fields expected [language, query], got %v", reqQuery)
	}

	// 7. walk: required machine_id, input
	walkPrim := lookup["walk"]
	reqWalk := walkPrim.InputSchema["required"].([]interface{})
	if len(reqWalk) != 2 || reqWalk[0] != "machine_id" || reqWalk[1] != "input" {
		t.Errorf("walk required fields expected [machine_id, input], got %v", reqWalk)
	}

	// 8. machine_state: required machine_id
	mStatePrim := lookup["machine_state"]
	reqMState := mStatePrim.InputSchema["required"].([]interface{})
	if len(reqMState) != 1 || reqMState[0] != "machine_id" {
		t.Errorf("machine_state required fields expected [machine_id], got %v", reqMState)
	}

	// 9. machine_history: required machine_id
	mHistPrim := lookup["machine_history"]
	reqMHist := mHistPrim.InputSchema["required"].([]interface{})
	if len(reqMHist) != 1 || reqMHist[0] != "machine_id" {
		t.Errorf("machine_history required fields expected [machine_id], got %v", reqMHist)
	}

	// 10. cal_check: required calendars, start, end
	calCheckPrim := lookup["cal_check"]
	reqCalCheck := calCheckPrim.InputSchema["required"].([]interface{})
	if len(reqCalCheck) != 3 {
		t.Errorf("cal_check required fields expected 3 items, got %v", reqCalCheck)
	}

	// 11. cal_openings: required calendars, duration, from, to
	calOpeningsPrim := lookup["cal_openings"]
	reqCalOpenings := calOpeningsPrim.InputSchema["required"].([]interface{})
	if len(reqCalOpenings) != 4 {
		t.Errorf("cal_openings required fields expected 4 items, got %v", reqCalOpenings)
	}

	// 12. cal_propose: required calendars, start, end
	calPropPrim := lookup["cal_propose"]
	reqCalProp := calPropPrim.InputSchema["required"].([]interface{})
	if len(reqCalProp) != 3 {
		t.Errorf("cal_propose required fields expected 3 items, got %v", reqCalProp)
	}

	// 13. cal_confirm: required calendar_id and booking_id (xolu confirms within
	// one calendar and cannot look a booking up without it)
	calConfPrim := lookup["cal_confirm"]
	reqCalConf := calConfPrim.InputSchema["required"].([]interface{})
	if len(reqCalConf) != 2 || reqCalConf[0] != "calendar_id" || reqCalConf[1] != "booking_id" {
		t.Errorf("cal_confirm required fields expected [calendar_id booking_id], got %v", reqCalConf)
	}
}

func TestServer_ListTools_Exposes13StandardPrimitives(t *testing.T) {
	// A server initialized with Default ServerConfig (no provider specified)
	// should expose the 13 standard primitives in tools/list
	srv := NewServer(ServerConfig{})

	reqJSON := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	var out strings.Builder
	err := srv.HandleMessage(context.Background(), []byte(reqJSON), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp struct {
		Result struct {
			Tools []ToolDescriptor `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out.String()), &resp); err != nil {
		t.Fatalf("failed to unmarshal tools/list response: %v", err)
	}

	if len(resp.Result.Tools) != 13 {
		t.Fatalf("expected 13 tools exposed in tools/list, got %d", len(resp.Result.Tools))
	}

	foundMap := make(map[string]ToolDescriptor)
	for _, tool := range resp.Result.Tools {
		if strings.Contains(tool.Name, ".") {
			t.Errorf("tool %q has unexpected namespace dot", tool.Name)
		}
		foundMap[tool.Name] = tool
	}

	expectedPrims := []string{
		"describe", "get", "list", "create", "update", "query",
		"walk", "machine_state", "machine_history",
		"cal_check", "cal_openings", "cal_propose", "cal_confirm",
	}
	for _, name := range expectedPrims {
		if _, ok := foundMap[name]; !ok {
			t.Errorf("expected tool %q not found in tools/list response", name)
		}
	}
}

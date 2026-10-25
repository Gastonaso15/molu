package mcp

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

	"github.com/Gastonaso15/molu/pkg/schema"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
	"github.com/Gastonaso15/molu/pkg/xolu"
)

func TestServer_E2E_Stdio_WithHTTPXoluClient(t *testing.T) {
	// Mock substrate xolu server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/asset/ast-1":
			json.NewEncoder(w).Encode(map[string]interface{}{"id": "ast-1", "name": "Main Pump"})
		case r.Method == "GET" && r.URL.Path == "/api/v1/asset/ast-404":
			http.NotFound(w, r)
		case r.Method == "GET" && r.URL.Path == "/api/v1/asset":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data":       []interface{}{},
				"pagination": map[string]interface{}{"page": 1, "per_page": 100, "total_items": 0},
			})
		case r.Method == "POST" && r.URL.Path == "/api/v2/fsm/machine/m-1/walk":
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "COMPLETED"})
		case r.Method == "POST" && r.URL.Path == "/api/v2/fsm/machine/m-guard-fail/walk":
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code":    "XOLU-INVALID_TRANSITION",
				"message": "guard rejected transition",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	xoluClient := xolu.NewHTTPXoluClient(ts.URL, 2*time.Second)

	srv := NewServer(ServerConfig{
		Executor: xoluClient,
	})

	// Setup simulated stdio via pipes
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- srv.Serve(ctx, stdinReader, stdoutWriter)
	}()

	linesCh := make(chan string, 50)
	go func() {
		s := bufio.NewScanner(stdoutReader)
		for s.Scan() {
			linesCh <- s.Text()
		}
		close(linesCh)
	}()

	// Helper to send request and read single-line response
	sendAndReceive := func(reqJSON string) map[string]interface{} {
		_, err := stdinWriter.Write([]byte(reqJSON + "\n"))
		if err != nil {
			t.Fatalf("failed to write to stdin pipe: %v", err)
		}
		select {
		case line := <-linesCh:
			var resp map[string]interface{}
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				t.Fatalf("failed to parse json response %q: %v", line, err)
			}
			return resp
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for response to %s", reqJSON)
			return nil
		}
	}

	// 1. Initialize
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`
	initResp := sendAndReceive(initReq)
	if initResp["error"] != nil {
		t.Fatalf("expected initialize success, got %v", initResp["error"])
	}

	// 2. tools/list: must expose 13 primitives without namespace
	listReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	listResp := sendAndReceive(listReq)
	resObj := listResp["result"].(map[string]interface{})
	tools := resObj["tools"].([]interface{})
	if len(tools) != 13 {
		t.Fatalf("expected 13 standard primitives in tools/list, got %d", len(tools))
	}
	for _, toolItem := range tools {
		toolMap := toolItem.(map[string]interface{})
		name := toolMap["name"].(string)
		if strings.Contains(name, ".") {
			t.Errorf("primitive %q contains dot (must not have namespace)", name)
		}
	}

	// 3. tools/call get: success
	getReq := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get","arguments":{"entity_type":"asset","id":"ast-1"}}}`
	getResp := sendAndReceive(getReq)
	if getResp["error"] != nil {
		t.Fatalf("expected get success, got %v", getResp["error"])
	}
	getResult := getResp["result"].(map[string]interface{})
	content := getResult["content"].([]interface{})[0].(map[string]interface{})
	if !strings.Contains(content["text"].(string), "Main Pump") {
		t.Errorf("expected Main Pump in text, got %v", content["text"])
	}

	// 4. tools/call list: empty result maps to EMPTY taxonomy
	emptyListReq := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list","arguments":{"entity_type":"asset"}}}`
	emptyListResp := sendAndReceive(emptyListReq)
	if emptyListResp["error"] == nil {
		t.Fatalf("expected EMPTY taxonomy error, got result: %v", emptyListResp["result"])
	}
	errData := emptyListResp["error"].(map[string]interface{})["data"].(map[string]interface{})
	if errData["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeEmpty) {
		t.Errorf("expected EMPTY code, got %v", errData["code"])
	}

	// 5. tools/call get 404: maps to NOT_FOUND taxonomy
	notFoundReq := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get","arguments":{"entity_type":"asset","id":"ast-404"}}}`
	notFoundResp := sendAndReceive(notFoundReq)
	if notFoundResp["error"] == nil {
		t.Fatalf("expected NOT_FOUND error, got result: %v", notFoundResp["result"])
	}
	errData = notFoundResp["error"].(map[string]interface{})["data"].(map[string]interface{})
	if errData["code"] != string(taxonomy.PrefixMolu+taxonomy.CodeNotFound) {
		t.Errorf("expected NOT_FOUND code, got %v", errData["code"])
	}

	// 6. tools/call walk: success
	walkReq := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"walk","arguments":{"machine_id":"m-1","input":"START"}}}`
	walkResp := sendAndReceive(walkReq)
	if walkResp["error"] != nil {
		t.Fatalf("expected walk success, got %v", walkResp["error"])
	}
	walkResult := walkResp["result"].(map[string]interface{})
	walkContent := walkResult["content"].([]interface{})[0].(map[string]interface{})
	if !strings.Contains(walkContent["text"].(string), "COMPLETED") {
		t.Errorf("expected COMPLETED in walk output, got %v", walkContent["text"])
	}

	// 7. tools/call walk: guard rejection maps to INVALID_TRANSITION verbatim
	guardFailReq := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"walk","arguments":{"machine_id":"m-guard-fail","input":"START"}}}`
	guardFailResp := sendAndReceive(guardFailReq)
	if guardFailResp["error"] == nil {
		t.Fatalf("expected INVALID_TRANSITION error, got result: %v", guardFailResp["result"])
	}
	errData = guardFailResp["error"].(map[string]interface{})["data"].(map[string]interface{})
	if errData["code"] != "XOLU-INVALID_TRANSITION" {
		t.Errorf("expected verbatim XOLU-INVALID_TRANSITION, got %v", errData["code"])
	}

	// 8. Hot reload via UpdatePrimitives emits notification
	srv.UpdatePrimitives([]schema.PrimitiveSchema{
		{
			Name:        "describe",
			Description: "Semantic map",
			InputSchema: map[string]interface{}{"type": "object"},
		},
	})
	select {
	case line := <-linesCh:
		var notif map[string]interface{}
		if err := json.Unmarshal([]byte(line), &notif); err != nil {
			t.Fatalf("failed to parse notification %q: %v", line, err)
		}
		if notif["method"] != "notifications/tools/list_changed" {
			t.Errorf("expected notifications/tools/list_changed, got %v", notif["method"])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for notification")
	}

	// Teardown
	cancel()
	_ = stdinWriter.Close()
	_ = stdoutWriter.Close()
}

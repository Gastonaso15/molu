package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// toolCallReply is a tools/call answer as an MCP client sees it.
type toolCallReply struct {
	Result *struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func callTool(t *testing.T, srv *Server, params string) toolCallReply {
	t.Helper()
	var out bytes.Buffer
	msg := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + params + `}`
	if err := srv.HandleMessage(context.Background(), []byte(msg), &out); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	var reply toolCallReply
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
		t.Fatalf("invalid reply %q: %v", out.String(), err)
	}
	return reply
}

// typedFromToolError requires an isError result whose text is the typed error.
func typedFromToolError(t *testing.T, reply toolCallReply) taxonomy.TypedError {
	t.Helper()
	if reply.Error != nil {
		t.Fatalf("a tool failure must not be a JSON-RPC error, got code %d", reply.Error.Code)
	}
	if reply.Result == nil || !reply.Result.IsError {
		t.Fatalf("expected a result with isError=true, got %+v", reply.Result)
	}
	if len(reply.Result.Content) != 1 || reply.Result.Content[0].Type != "text" {
		t.Fatalf("expected one text content item, got %+v", reply.Result.Content)
	}
	var te taxonomy.TypedError
	if err := json.Unmarshal([]byte(reply.Result.Content[0].Text), &te); err != nil {
		t.Fatalf("content text must be the typed error as JSON: %v (%q)", err, reply.Result.Content[0].Text)
	}
	return te
}

func TestToolsCallFailuresAreIsErrorResultsWithTheTypedCode(t *testing.T) {
	execErr := func(err error) *mockSubstrateExecutor {
		return &mockSubstrateExecutor{executeFn: func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
			return nil, err
		}}
	}
	cases := []struct {
		label  string
		srv    *Server
		params string
		code   taxonomy.ErrorCode
		prefix string
	}{
		{"not offered", NewServer(ServerConfig{Executor: execErr(nil)}),
			`{"name":"foo","arguments":{}}`, taxonomy.CodeNotOffered, taxonomy.PrefixMolu},
		{"contract violation on arguments", NewServer(ServerConfig{Executor: execErr(nil)}),
			`{"name":"get","arguments":{"entity_type":"asset"}}`, taxonomy.CodeContractViolation, taxonomy.PrefixMolu},
		{"substrate unavailable", NewServer(ServerConfig{Executor: execErr(nil), Health: &mockHealthChecker{checkFn: func() error { return taxonomy.NewSubstrateUnavailable("t0", "t1", 1, 3) }}}),
			`{"name":"describe","arguments":{}}`, taxonomy.CodeSubstrateUnavailable, taxonomy.PrefixMolu},
		{"not found from http status", NewServer(ServerConfig{Executor: execErr(&statusError{status: 404})}),
			`{"name":"get","arguments":{"entity_type":"asset","id":7}}`, taxonomy.CodeNotFound, taxonomy.PrefixMolu},
		{"invalid transition verbatim", NewServer(ServerConfig{Executor: execErr(&taxonomy.TypedError{
			Code: taxonomy.PrefixXolu + string(taxonomy.CodeInvalidTransition), Message: "guard failed: temp > 100"})}),
			`{"name":"walk","arguments":{"machine_id":"m-1","input":"GO"}}`, taxonomy.CodeInvalidTransition, taxonomy.PrefixXolu},
		{"empty list", NewServer(ServerConfig{Executor: &mockSubstrateExecutor{executeFn: func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"data": []interface{}{}}, nil
		}}}), `{"name":"list","arguments":{"entity_type":"asset"}}`, taxonomy.CodeEmpty, taxonomy.PrefixMolu},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			te := typedFromToolError(t, callTool(t, tc.srv, tc.params))
			if te.Code != tc.prefix+string(tc.code) {
				t.Fatalf("expected code %s%s, got %s", tc.prefix, tc.code, te.Code)
			}
			if te.Message == "" {
				t.Fatal("the typed message must reach the agent")
			}
		})
	}
}

func TestToolsCallUntypedExecutorFailureIsIsErrorWithMessage(t *testing.T) {
	srv := NewServer(ServerConfig{Executor: &mockSubstrateExecutor{executeFn: func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("xolu error (400): bad request")
	}}})
	reply := callTool(t, srv, `{"name":"describe","arguments":{}}`)
	if reply.Error != nil || reply.Result == nil || !reply.Result.IsError {
		t.Fatalf("expected isError result, got %+v / %+v", reply.Result, reply.Error)
	}
	if got := reply.Result.Content[0].Text; got != "xolu error (400): bad request" {
		t.Fatalf("untyped failures keep their plain message, got %q", got)
	}
}

func TestToolsCallSuccessIsNotMarkedAsError(t *testing.T) {
	srv := NewServer(ServerConfig{Executor: &mockSubstrateExecutor{}})
	reply := callTool(t, srv, `{"name":"describe","arguments":{}}`)
	if reply.Error != nil || reply.Result == nil || reply.Result.IsError {
		t.Fatalf("a successful call must not set isError, got %+v / %+v", reply.Result, reply.Error)
	}
}

func TestToolsCallMalformedParamsStayProtocolErrors(t *testing.T) {
	srv := NewServer(ServerConfig{Executor: &mockSubstrateExecutor{}})
	for label, params := range map[string]string{"string": `"x"`, "null": `null`} {
		t.Run(label, func(t *testing.T) {
			reply := callTool(t, srv, params)
			if reply.Error == nil || reply.Error.Code != -32602 {
				t.Fatalf("malformed params must be JSON-RPC -32602, got %+v / %+v", reply.Result, reply.Error)
			}
		})
	}
}

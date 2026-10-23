package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/rpi/providers"
	"github.com/Gastonaso15/molu/pkg/schema"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

type mockDiscoverer struct {
	calls []rpi.AdmitContext
	fn    func(ctx context.Context, actx rpi.AdmitContext) ([]rpi.ScoredTool, error)
}

func (m *mockDiscoverer) ListTools(ctx context.Context, actx rpi.AdmitContext) ([]rpi.ScoredTool, error) {
	m.calls = append(m.calls, actx)
	return m.fn(ctx, actx)
}

type rpcReply struct {
	Result struct {
		Tools []ToolDescriptor `json:"tools"`
	} `json:"result"`
	Error *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

func toolsList(t *testing.T, srv *Server, params string) rpcReply {
	t.Helper()
	msg := `{"jsonrpc":"2.0","id":1,"method":"tools/list"`
	if params != "" {
		msg += `,"params":` + params
	}
	msg += `}`
	var out bytes.Buffer
	if err := srv.HandleMessage(context.Background(), []byte(msg), &out); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	var reply rpcReply
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
		t.Fatalf("invalid JSON-RPC reply %q: %v", out.String(), err)
	}
	return reply
}

func errData(t *testing.T, reply rpcReply) taxonomy.TypedError {
	t.Helper()
	if reply.Error == nil {
		t.Fatal("expected a JSON-RPC error")
	}
	var te taxonomy.TypedError
	if err := json.Unmarshal(reply.Error.Data, &te); err != nil {
		t.Fatalf("error data must be a typed error: %v (%s)", err, reply.Error.Data)
	}
	return te
}

func scored(name string, score float64, schemaMap map[string]any) rpi.ScoredTool {
	return rpi.ScoredTool{
		ToolCandidate: rpi.ToolCandidate{Name: name, Description: "d-" + name, InputSchema: schemaMap},
		Score:         score,
	}
}

func TestToolsListHubParsesAdmitContextFromParams(t *testing.T) {
	d := &mockDiscoverer{fn: func(context.Context, rpi.AdmitContext) ([]rpi.ScoredTool, error) {
		return []rpi.ScoredTool{scored("walk", 1, nil)}, nil
	}}
	srv := NewServer(ServerConfig{Discovery: d})

	toolsList(t, srv, `{"tenant":"t1","roles":["operator","admin"],"query":"orders"}`)

	want := rpi.AdmitContext{Tenant: "t1", Roles: []string{"operator", "admin"}, Query: "orders"}
	if len(d.calls) != 1 || !reflect.DeepEqual(d.calls[0], want) {
		t.Fatalf("expected one call with %+v, got %+v", want, d.calls)
	}
}

func TestToolsListHubAcceptsStandardCursorParam(t *testing.T) {
	d := &mockDiscoverer{fn: func(context.Context, rpi.AdmitContext) ([]rpi.ScoredTool, error) {
		return []rpi.ScoredTool{scored("walk", 1, nil)}, nil
	}}
	reply := toolsList(t, NewServer(ServerConfig{Discovery: d}), `{"tenant":"t1","cursor":"abc"}`)
	if reply.Error != nil || len(reply.Result.Tools) != 1 {
		t.Fatalf("cursor must not break the call: %+v", reply)
	}
}

func TestToolsListHubReturnsDescriptorsInRelevanceOrder(t *testing.T) {
	custom := map[string]any{"type": "object", "required": []any{"id"}}
	d := &mockDiscoverer{fn: func(context.Context, rpi.AdmitContext) ([]rpi.ScoredTool, error) {
		return []rpi.ScoredTool{
			scored("work_orders.CreateOrder", 0.9, custom),
			scored("walk", 0.4, nil),
		}, nil
	}}
	reply := toolsList(t, NewServer(ServerConfig{Discovery: d}), `{"tenant":"t1"}`)

	if reply.Error != nil {
		t.Fatalf("unexpected error: %+v", reply.Error)
	}
	got := reply.Result.Tools
	if len(got) != 2 || got[0].Name != "work_orders.CreateOrder" || got[1].Name != "walk" {
		t.Fatalf("expected provider order, got %+v", got)
	}
	if got[0].Description != "d-work_orders.CreateOrder" || !reflect.DeepEqual(got[0].InputSchema, custom) {
		t.Fatalf("descriptor fields lost: %+v", got[0])
	}
	if got[1].InputSchema["type"] != "object" {
		t.Fatalf("missing schema must default to an object schema, got %v", got[1].InputSchema)
	}
}

func TestToolsListHubTypedErrorsKeepTheirCode(t *testing.T) {
	cases := map[string]struct {
		err      error
		code     taxonomy.ErrorCode
		rpcCode  int
		hasField string
	}{
		"contract violation": {taxonomy.NewContractViolation("", "tools/list", []string{"tenant"}), taxonomy.CodeContractViolation, -32602, "fields"},
		"empty":              {taxonomy.NewEmpty("t1", "tools/list", nil), taxonomy.CodeEmpty, -32000, "tenant"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			d := &mockDiscoverer{fn: func(context.Context, rpi.AdmitContext) ([]rpi.ScoredTool, error) {
				return nil, tc.err
			}}
			reply := toolsList(t, NewServer(ServerConfig{Discovery: d}), `{"tenant":"t1"}`)

			te := errData(t, reply)
			if te.Code != taxonomy.PrefixMolu+string(tc.code) {
				t.Fatalf("expected code %s, got %s", tc.code, te.Code)
			}
			if _, ok := te.Details[tc.hasField]; !ok {
				t.Fatalf("details must survive, missing %q in %v", tc.hasField, te.Details)
			}
			if reply.Error.Code != tc.rpcCode {
				t.Fatalf("expected JSON-RPC code %d, got %d", tc.rpcCode, reply.Error.Code)
			}
			if reply.Error.Message != tc.err.(*taxonomy.TypedError).Message {
				t.Fatalf("message must be the typed message, got %q", reply.Error.Message)
			}
		})
	}
}

func TestToolsListHubUntypedErrorIsInternalError(t *testing.T) {
	d := &mockDiscoverer{fn: func(context.Context, rpi.AdmitContext) ([]rpi.ScoredTool, error) {
		return nil, errors.New("rpi backend down")
	}}
	reply := toolsList(t, NewServer(ServerConfig{Discovery: d}), `{"tenant":"t1"}`)
	if reply.Error == nil || reply.Error.Code != -32603 || !strings.Contains(reply.Error.Message, "rpi backend down") {
		t.Fatalf("expected -32603 carrying the cause, got %+v", reply.Error)
	}
}

func TestToolsListHubMalformedParamsAreContractViolation(t *testing.T) {
	for label, params := range map[string]string{
		"string":     `"t1"`,
		"array":      `[1]`,
		"wrong type": `{"tenant":5}`,
		"bad roles":  `{"tenant":"t1","roles":"admin"}`,
	} {
		t.Run(label, func(t *testing.T) {
			d := &mockDiscoverer{fn: func(context.Context, rpi.AdmitContext) ([]rpi.ScoredTool, error) {
				return []rpi.ScoredTool{scored("walk", 1, nil)}, nil
			}}
			reply := toolsList(t, NewServer(ServerConfig{Discovery: d}), params)

			te := errData(t, reply)
			if te.Code != taxonomy.PrefixMolu+string(taxonomy.CodeContractViolation) || reply.Error.Code != -32602 {
				t.Fatalf("expected CONTRACT_VIOLATION/-32602, got %s/%d", te.Code, reply.Error.Code)
			}
			if len(d.calls) != 0 {
				t.Fatalf("discovery must not be consulted, got %d calls", len(d.calls))
			}
		})
	}
}

func TestToolsListHubMissingParamsReachDiscoveryEmpty(t *testing.T) {
	d := &mockDiscoverer{fn: func(context.Context, rpi.AdmitContext) ([]rpi.ScoredTool, error) {
		return nil, taxonomy.NewContractViolation("", "tools/list", []string{"tenant"})
	}}
	reply := toolsList(t, NewServer(ServerConfig{Discovery: d}), "")

	if len(d.calls) != 1 || d.calls[0].Tenant != "" {
		t.Fatalf("expected one call with empty context, got %+v", d.calls)
	}
	if te := errData(t, reply); te.Code != taxonomy.PrefixMolu+string(taxonomy.CodeContractViolation) {
		t.Fatalf("expected CONTRACT_VIOLATION, got %s", te.Code)
	}
}

func TestToolsListHubWithRealDiscovery(t *testing.T) {
	reg := hub.NewRegistry()
	for _, r := range []hub.ToolRegistration{
		{PublisherID: "p", Name: "walk", Primitive: true, Description: "d", InputSchema: map[string]any{"type": "object"}},
		{PublisherID: "p", Name: "work_orders.CreateOrder", Namespace: "work_orders", Description: "d", InputSchema: map[string]any{"type": "object"}},
	} {
		if err := reg.Register(r); err != nil {
			t.Fatal(err)
		}
	}
	rules := map[string][]string{"t1:operator": {"work_orders.CreateOrder"}}
	srv := NewServer(ServerConfig{Discovery: hub.NewDiscovery(reg, providers.NewStaticContextRulesProvider(rules))})

	reply := toolsList(t, srv, `{"tenant":"t1","roles":["operator"]}`)
	if reply.Error != nil || len(reply.Result.Tools) != 1 || reply.Result.Tools[0].Name != "work_orders.CreateOrder" {
		t.Fatalf("expected only the admitted tool, got %+v", reply)
	}

	reply = toolsList(t, srv, `{"tenant":"t1","roles":["intruder"]}`)
	if te := errData(t, reply); te.Code != taxonomy.PrefixMolu+string(taxonomy.CodeEmpty) {
		t.Fatalf("expected EMPTY for a role without rules, got %s", te.Code)
	}
}

func TestToolsListWithoutHubIgnoresParams(t *testing.T) {
	prims := []schema.PrimitiveSchema{{Name: "walk", Description: "d"}}
	srv := NewServer(ServerConfig{Provider: &mockPrimitiveProvider{primitives: prims}})

	reply := toolsList(t, srv, `{"tenant":"t1"}`)
	if reply.Error != nil || len(reply.Result.Tools) != 1 || reply.Result.Tools[0].Name != "walk" {
		t.Fatalf("without a Hub tools/list must keep serving primitives, got %+v", reply)
	}
}

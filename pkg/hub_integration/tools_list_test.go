package hub_integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Gastonaso15/molu/pkg/config"
	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/mcp"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

const rulesJSON = `{
	"acme:operator": ["walk", "work_orders.CreateOrder"],
	"acme:viewer":   ["walk"]
}`

type reply struct {
	Result struct {
		Tools []mcp.ToolDescriptor `json:"tools"`
	} `json:"result"`
	Error *struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	} `json:"error"`
}

// serverFromEnv assembles the Hub exactly as the binary does: configuration
// from the environment decides the RPI provider, nothing else changes.
func serverFromEnv(t *testing.T, env map[string]string) *mcp.Server {
	t.Helper()
	t.Setenv("MOLU_FRONT_XOLU_URL", "http://localhost:8080")
	t.Setenv("MOLU_FRONT_XOLU_AUTH_MODE", "jwt")
	t.Setenv("MOLU_FRONT_XOLU_TOKEN_FILE", "xolu.jwt")
	for k, v := range env {
		t.Setenv(k, v)
	}
	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	provider, err := hub.NewRPIProvider(cfg.HubRPIProvider, cfg.HubRPIRulesFile, cfg.HubRPIRulesJSON)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}

	reg := hub.NewRegistry()
	for _, r := range []hub.ToolRegistration{
		{PublisherID: "pub-1", Name: "walk", Primitive: true, Description: "fsm walk", InputSchema: map[string]any{"type": "object"}},
		{PublisherID: "pub-1", Name: "work_orders.CreateOrder", Namespace: "work_orders", Description: "creates an order", InputSchema: map[string]any{"type": "object"}},
		{PublisherID: "pub-2", Name: "inventory.ReserveStock", Namespace: "inventory", Description: "reserves stock", InputSchema: map[string]any{"type": "object"}},
	} {
		if err := reg.Register(r); err != nil {
			t.Fatalf("register %s: %v", r.Name, err)
		}
	}
	return mcp.NewServer(mcp.ServerConfig{Discovery: hub.NewDiscovery(reg, provider)})
}

func toolsList(t *testing.T, srv *mcp.Server, params string) reply {
	t.Helper()
	msg := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":` + params + `}`
	var out bytes.Buffer
	if err := srv.HandleMessage(context.Background(), []byte(msg), &out); err != nil {
		t.Fatal(err)
	}
	var r reply
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("bad reply %q: %v", out.String(), err)
	}
	return r
}

func names(r reply) []string {
	out := []string{}
	for _, tl := range r.Result.Tools {
		out = append(out, tl.Name)
	}
	return out
}

// primitiveNames are the generic primitives every tools/list carries first.
func primitiveNames() []string {
	out := []string{}
	for _, p := range mcp.StandardPrimitives() {
		out = append(out, p.Name)
	}
	return out
}

func withPrimitives(domain ...string) []string {
	return append(primitiveNames(), domain...)
}

func errCode(t *testing.T, r reply) string {
	t.Helper()
	if r.Error == nil {
		t.Fatalf("expected an error, got tools %v", names(r))
	}
	var te taxonomy.TypedError
	if err := json.Unmarshal(r.Error.Data, &te); err != nil {
		t.Fatalf("error data is not a typed error: %v", err)
	}
	return te.Code
}

// Criterio 7: switching the RPI provider by configuration alone changes what
// tools/list offers for the same requester and the same catalog.
func TestIntegration_ProviderSwitchChangesDiscovery(t *testing.T) {
	params := `{"tenant":"acme","roles":["viewer"]}`

	all := toolsList(t, serverFromEnv(t, map[string]string{
		"MOLU_HUB_RPI_PROVIDER": "admit-all",
	}), params)
	if want := withPrimitives("inventory.ReserveStock", "work_orders.CreateOrder"); !reflect.DeepEqual(names(all), want) {
		t.Fatalf("admit-all: expected %v, got %v", want, names(all))
	}

	static := toolsList(t, serverFromEnv(t, map[string]string{
		"MOLU_HUB_RPI_PROVIDER":   "static-context-rules",
		"MOLU_HUB_RPI_RULES_JSON": rulesJSON,
	}), params)
	if want := withPrimitives(); !reflect.DeepEqual(names(static), want) {
		t.Fatalf("static-context-rules: expected %v, got %v", want, names(static))
	}
}

func TestIntegration_StaticRulesFromFileAndRoles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte(rulesJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := serverFromEnv(t, map[string]string{
		"MOLU_HUB_RPI_PROVIDER":   "static-context-rules",
		"MOLU_HUB_RPI_RULES_FILE": path,
	})

	op := toolsList(t, srv, `{"tenant":"acme","roles":["operator"]}`)
	if want := withPrimitives("work_orders.CreateOrder"); !reflect.DeepEqual(names(op), want) {
		t.Fatalf("operator: expected %v, got %v", want, names(op))
	}
	viewer := toolsList(t, srv, `{"tenant":"acme","roles":["viewer"]}`)
	if want := withPrimitives(); !reflect.DeepEqual(names(viewer), want) {
		t.Fatalf("viewer: expected %v, got %v", want, names(viewer))
	}
}

// Criterio 2 over tools/list: the typed codes reach the client intact.
func TestIntegration_ToolsListTypedErrors(t *testing.T) {
	srv := serverFromEnv(t, map[string]string{
		"MOLU_HUB_RPI_PROVIDER":   "static-context-rules",
		"MOLU_HUB_RPI_RULES_JSON": rulesJSON,
	})

	// EMPTY from the Hub is not an error of the unified surface: the generic
	// primitives remain.
	empty := toolsList(t, srv, `{"tenant":"acme","roles":["intruder"]}`)
	if empty.Error != nil || !reflect.DeepEqual(names(empty), withPrimitives()) {
		t.Fatalf("expected the primitives alone, got error=%v names=%v", empty.Error, names(empty))
	}

	noTenant := toolsList(t, srv, `{"roles":["operator"]}`)
	if got := errCode(t, noTenant); got != taxonomy.PrefixMolu+string(taxonomy.CodeContractViolation) {
		t.Fatalf("expected CONTRACT_VIOLATION, got %s", got)
	}
}

// The Hub's reserved names must be exactly the generic primitives molu serves,
// or a publisher could shadow a real primitive (or lose a name for nothing).
// pkg/hub cannot import pkg/mcp, so the comparison lives here.
func TestIntegration_ReservedNamesAreTheStandardPrimitives(t *testing.T) {
	for _, p := range mcp.StandardPrimitives() {
		if !hub.IsReservedPrimitive(p.Name) {
			t.Errorf("primitive %q is not reserved in the Hub registry", p.Name)
		}
	}
	for _, name := range []string{"cal_list", "find", "search", "delete"} {
		if hub.IsReservedPrimitive(name) && !mcp.IsStandardPrimitive(name) {
			t.Errorf("%q is reserved in the Hub but molu has no such primitive", name)
		}
	}
}

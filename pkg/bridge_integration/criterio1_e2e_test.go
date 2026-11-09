package bridge_integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/bridge"
	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/mcp"
	"github.com/Gastonaso15/molu/pkg/publisher/orders"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// Criterio 1: a stock MCP client completes an end-to-end scenario with an FSM
// rejection visible on the way. Every hop is a real HTTP service:
//
//	MCP client --stdio--> molu (Execution Bridge, remote Hub client)
//	                        |--> Hub (catalog + discovery)
//	                        '--> publisher-orders --> xolu (FSM)
//
// xolu is simulated at the wire level: it enforces the order lifecycle using the
// definition the publisher itself posts, and answers rejections in xolu's real
// FSM error shape ({"error":"XOLU-FSM00x",...}).

// fakeXolu interprets the FSM definition it is given, like xolu's runtime.
type fakeXolu struct {
	mu         sync.Mutex
	def        map[string]any
	machines   map[string]*fakeMachine
	entities   map[string]map[string]any
	seq        int
	entSeq     int
	walks      int
	rejections []string // xolu error codes issued on walk
}

type fakeMachine struct {
	id, state, ref string
}

func newFakeXolu() *fakeXolu {
	return &fakeXolu{machines: map[string]*fakeMachine{}, entities: map[string]map[string]any{}}
}

func (x *fakeXolu) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/fsm/def", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		if r.Method == http.MethodGet {
			items := []any{}
			if x.def != nil {
				items = append(items, map[string]any{"id": 42, "name": x.def["name"]})
			}
			xoluJSON(w, 200, map[string]any{"items": items})
			return
		}
		var def map[string]any
		_ = json.NewDecoder(r.Body).Decode(&def)
		x.def = def
		xoluJSON(w, 200, map[string]any{"id": 42, "name": def["name"]})
	})
	mux.HandleFunc("/api/v2/fsm/machine", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		var req struct {
			Ref string `json:"ref"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if _, ok := x.entities[req.Ref]; !ok {
			xoluError(w, 404, "XOLU-ST001", "bound entity not found")
			return
		}
		x.seq++
		id := strconv.Itoa(1000 + x.seq)
		m := &fakeMachine{id: id, state: x.def["initial"].(string), ref: req.Ref}
		x.machines[id] = m
		xoluJSON(w, 200, map[string]any{"id": 1000 + x.seq, "state": m.state, "ref": m.ref})
	})
	mux.HandleFunc("/api/v2/fsm/machine/", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v2/fsm/machine/"), "/")
		m, ok := x.machines[parts[0]]
		if !ok {
			xoluError(w, 404, "XOLU-FSM002", "machine not found")
			return
		}
		if len(parts) == 1 {
			xoluJSON(w, 200, map[string]any{"id": m.id, "state": m.state, "ref": m.ref})
			return
		}
		x.walk(w, r, m)
	})
	mux.HandleFunc("/api/v1/work_order", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		var doc map[string]any
		_ = json.NewDecoder(r.Body).Decode(&doc)
		x.entSeq++
		x.entities["work_order:"+strconv.Itoa(x.entSeq)] = doc
		xoluJSON(w, 201, map[string]any{"id": x.entSeq, "message": "created"})
	})
	mux.HandleFunc("/api/v1/work_order/", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		doc, ok := x.entities["work_order:"+strings.TrimPrefix(r.URL.Path, "/api/v1/work_order/")]
		if !ok {
			xoluError(w, 404, "XOLU-ST001", "entity not found")
			return
		}
		xoluJSON(w, 200, doc)
	})
	return mux
}

// walk mirrors xolu: no transition for (state,input) -> 409 FSM003; transitions
// exist but every guard is false -> 422 FSM004.
func (x *fakeXolu) walk(w http.ResponseWriter, r *http.Request, m *fakeMachine) {
	x.walks++
	var req struct {
		Input   string         `json:"input"`
		Payload map[string]any `json:"payload"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	var matching []map[string]any
	for _, t := range x.def["transitions"].([]any) {
		tr := t.(map[string]any)
		if tr["from"] != m.state {
			continue
		}
		if tr["input"] == req.Input {
			matching = append(matching, tr)
		}
	}
	if len(matching) == 0 {
		x.rejections = append(x.rejections, "XOLU-FSM003")
		xoluError(w, 409, "XOLU-FSM003", fmt.Sprintf("no transition for input %q from state %q", req.Input, m.state))
		return
	}
	for _, tr := range matching {
		guard, _ := tr["guard"].(string)
		if guard != "" {
			if res, _ := req.Payload["resolution"].(string); res == "" {
				continue // the only guard in this lifecycle reads payload.resolution
			}
		}
		prev := m.state
		m.state = tr["to"].(string)
		xoluJSON(w, 200, map[string]any{"previous": prev, "current": m.state, "terminal": false, "outputs": []any{}})
		return
	}
	x.rejections = append(x.rejections, "XOLU-FSM004")
	xoluError(w, 422, "XOLU-FSM004", fmt.Sprintf("transition guard rejected: %s", matching[0]["guard"]))
}

// xoluError writes the envelope real xolu uses: {"error":{"code","message","status"}}.
func xoluError(w http.ResponseWriter, status int, code, msg string) {
	xoluJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg, "status": status}})
}

func xoluJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type c1Harness struct {
	xolu      *fakeXolu
	registry  *hub.Registry
	registrar *orders.Registrar
	stdin     *io.PipeWriter
	lines     chan string
	nextID    int
}

func setupCriterio1(t *testing.T) *c1Harness {
	t.Helper()
	const token = "orders-bearer-secret"

	// xolu
	fx := newFakeXolu()
	xoluSrv := httptest.NewServer(fx.handler())
	t.Cleanup(xoluSrv.Close)

	// Hub (standalone service)
	registry := hub.NewRegistry()
	provider, err := hub.NewRPIProvider("admit-all", "", "")
	if err != nil {
		t.Fatal(err)
	}
	hubSrv := httptest.NewServer(hub.NewHubHTTPServer(registry, hub.NewDiscovery(registry, provider)).Handler())
	t.Cleanup(hubSrv.Close)

	// Publisher, auto-registered in the Hub
	svc := orders.NewService(orders.NewHTTPSubstrate(xoluSrv.URL, "", 2*time.Second))
	pubSrv := httptest.NewServer(orders.NewServer(svc, token).Handler())
	t.Cleanup(pubSrv.Close)
	registrar := orders.NewRegistrar(hubSrv.URL, pubSrv.URL, orders.DefaultBackoff())
	if err := registrar.Register(context.Background()); err != nil {
		t.Fatalf("publisher registration: %v", err)
	}

	// molu: remote Hub client + Execution Bridge, as cmd/molu wires them
	t.Setenv("MOLU_FRONT_TOKEN_ORDERS_API_TOKEN", token)
	remoteHub := hub.NewRemoteHubClient(hubSrv.URL, 2*time.Second)
	execBridge := bridge.NewExecutionBridge(remoteHub, nil, nil, bridge.NewHTTPDomainCaller(nil, nil, 3*time.Second))
	srv := mcp.NewServer(mcp.ServerConfig{
		Discovery:      remoteHub,
		DomainExecutor: execBridge,
		CatalogLookup:  remoteHub,
		CallTimeout:    5 * time.Second,
		DefaultTenant:  "dev",
	})

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx, stdinR, stdoutW) }()
	lines := make(chan string, 50)
	go func() {
		rd := bufio.NewReader(stdoutR)
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			lines <- line
		}
	}()
	t.Cleanup(func() { cancel(); _ = stdinW.Close(); _ = stdoutW.Close() })

	return &c1Harness{xolu: fx, registry: registry, registrar: registrar, stdin: stdinW, lines: lines}
}

// rpc plays the MCP client: one JSON-RPC request in, one response out.
func (h *c1Harness) rpc(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	h.nextID++
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": h.nextID, "method": method, "params": params})
	if _, err := io.WriteString(h.stdin, string(req)+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-h.lines:
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("bad response %q: %v", line, err)
		}
		return resp
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", method)
		return nil
	}
}

func (h *c1Harness) call(t *testing.T, tool string, args map[string]any) map[string]any {
	return h.rpc(t, "tools/call", map[string]any{"name": tool, "arguments": args})
}

func TestCriterio1_E2E(t *testing.T) {
	h := setupCriterio1(t)

	// 1. Discovery goes through molu to the remote Hub and sees the publisher's tools.
	list := h.rpc(t, "tools/list", map[string]any{})
	res, _ := list["result"].(map[string]any)
	tools, _ := res["tools"].([]any)
	seen := map[string]bool{}
	for _, tl := range tools {
		if m, ok := tl.(map[string]any); ok {
			seen[m["name"].(string)] = true
		}
	}
	for _, want := range []string{"work_orders.CreateOrder", "work_orders.GetOrder", "work_orders.AdvanceOrder"} {
		if !seen[want] {
			t.Fatalf("tools/list is missing %s; got %v", want, seen)
		}
	}

	// 2. An order is created in xolu through the bridge and the publisher.
	created := e2eSuccess(t, h.call(t, "work_orders.CreateOrder", map[string]any{"title": "Replace pump", "description": "Pump A"}))
	id, _ := created["id"].(string)
	if id == "" || created["state"] != "Open" || created["title"] != "Replace pump" {
		t.Fatalf("unexpected created order: %v", created)
	}

	// 3. It can be read back.
	got := e2eSuccess(t, h.call(t, "work_orders.GetOrder", map[string]any{"order_id": id}))
	if got["id"] != id || got["state"] != "Open" {
		t.Fatalf("unexpected order: %v", got)
	}

	// 4. THE CRITERION: an illegal transition is rejected by xolu's FSM and the
	//    rejection reaches the MCP client intact, as INVALID_TRANSITION.
	rejected := e2eFailure(t, h.call(t, "work_orders.AdvanceOrder", map[string]any{
		"order_id": id, "input": "complete", "payload": map[string]any{"resolution": "done"},
	}))
	if rejected["code"] != taxonomy.PrefixXolu+string(taxonomy.CodeInvalidTransition) {
		t.Fatalf("code = %v, want %s%s", rejected["code"], taxonomy.PrefixXolu, taxonomy.CodeInvalidTransition)
	}
	if want := `no transition for input "complete" from state "Open"`; rejected["message"] != want {
		t.Fatalf("message = %q, want xolu's verbatim %q", rejected["message"], want)
	}
	details, _ := rejected["details"].(map[string]any)
	if details["xolu_code"] != "XOLU-FSM003" {
		t.Fatalf("xolu diagnostics lost on the way: %v", details)
	}

	// 5. The rejection came from xolu itself, not from the publisher or molu.
	if h.xolu.walks == 0 || len(h.xolu.rejections) != 1 || h.xolu.rejections[0] != "XOLU-FSM003" {
		t.Fatalf("xolu did not issue the rejection: walks=%d rejections=%v", h.xolu.walks, h.xolu.rejections)
	}

	// 6. The order is untouched by the rejected attempt.
	after := e2eSuccess(t, h.call(t, "work_orders.GetOrder", map[string]any{"order_id": id}))
	if after["state"] != "Open" {
		t.Fatalf("rejected transition changed the order: %v", after)
	}

	// 7. A legal path still works, and a guard rejection keeps its diagnostic.
	adv := e2eSuccess(t, h.call(t, "work_orders.AdvanceOrder", map[string]any{"order_id": id, "input": "start"}))
	if adv["current"] != "InProgress" {
		t.Fatalf("start: %v", adv)
	}
	guard := e2eFailure(t, h.call(t, "work_orders.AdvanceOrder", map[string]any{"order_id": id, "input": "complete"}))
	gd, _ := guard["details"].(map[string]any)
	msg, _ := guard["message"].(string)
	if guard["code"] != "XOLU-INVALID_TRANSITION" || gd["xolu_code"] != "XOLU-FSM004" ||
		!strings.HasPrefix(msg, "transition guard rejected: ") || !strings.Contains(msg, "payload.resolution") {
		t.Fatalf("guard rejection lost its diagnostic: %v", guard)
	}
}

func TestCriterio1_E2E_UnknownOrderIsNotFound(t *testing.T) {
	h := setupCriterio1(t)
	errData := e2eFailure(t, h.call(t, "work_orders.GetOrder", map[string]any{"order_id": "999999"}))
	if want := taxonomy.PrefixMolu + string(taxonomy.CodeNotFound); errData["code"] != want {
		t.Fatalf("code = %v, want %s", errData["code"], want)
	}
}

func TestCriterio1_E2E_UnregisteredToolIsNotOffered(t *testing.T) {
	h := setupCriterio1(t)
	if err := h.registrar.Unregister(context.Background()); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	errData := e2eFailure(t, h.call(t, "work_orders.CreateOrder", map[string]any{"title": "x"}))
	if want := taxonomy.PrefixMolu + string(taxonomy.CodeNotOffered); errData["code"] != want {
		t.Fatalf("code = %v, want %s after the publisher shut down", errData["code"], want)
	}
}

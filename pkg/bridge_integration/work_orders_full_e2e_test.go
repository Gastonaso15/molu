package bridge_integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
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

// fullWireXolu simulates xolu at the HTTP wire level for multi-entity CRUD,
// optimistic _version locking, atomic Commit, OQL WHERE queries and FSM v2
// definitions/machines.
type fullWireXolu struct {
	mu       sync.Mutex
	seq      int64
	fsmSeq   int64
	defs     map[string]int64
	defDocs  map[int64]map[string]any // definition id -> definition
	machines map[string]string        // machine id -> state
	machDef  map[string]int64         // machine id -> definition id
	entities map[string]map[string]map[string]any
}

func newFullWireXolu() *fullWireXolu {
	return &fullWireXolu{
		defs:     map[string]int64{},
		defDocs:  map[int64]map[string]any{},
		machines: map[string]string{},
		machDef:  map[string]int64{},
		entities: map[string]map[string]map[string]any{},
	}
}

func (x *fullWireXolu) seed(entity, id string, data map[string]any) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.putLocked(entity, id, data, 1)
}

func (x *fullWireXolu) putLocked(entity, id string, data map[string]any, version int64) {
	if x.entities[entity] == nil {
		x.entities[entity] = map[string]map[string]any{}
	}
	doc := make(map[string]any, len(data)+2)
	for k, v := range data {
		doc[k] = v
	}
	if n, err := strconv.ParseInt(id, 10, 64); err == nil {
		doc["id"] = n
	} else {
		doc["id"] = id
	}
	doc["_version"] = version
	x.entities[entity][id] = doc
}

func (x *fullWireXolu) handler() http.Handler {
	mux := http.NewServeMux()

	// FSM v2 definitions
	mux.HandleFunc("/api/v2/fsm/def", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		if r.Method == http.MethodGet {
			items := make([]any, 0, len(x.defs))
			for name, id := range x.defs {
				items = append(items, map[string]any{"id": id, "name": name})
			}
			xoluJSON(w, 200, map[string]any{"items": items})
			return
		}
		var def map[string]any
		_ = json.NewDecoder(r.Body).Decode(&def)
		name, _ := def["name"].(string)
		x.fsmSeq++
		x.defs[name] = x.fsmSeq
		x.defDocs[x.fsmSeq] = def
		xoluJSON(w, 200, map[string]any{"id": x.fsmSeq, "name": name})
	})

	// FSM v2 machine create
	mux.HandleFunc("/api/v2/fsm/machine", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		var body struct {
			Definition json.Number `json:"definition"`
		}
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		_ = dec.Decode(&body)
		defID, _ := body.Definition.Int64()
		initial := "pending"
		if def, ok := x.defDocs[defID]; ok {
			initial, _ = def["initial"].(string)
		}
		x.fsmSeq++
		id := strconv.FormatInt(1000+x.fsmSeq, 10)
		x.machines[id] = initial
		x.machDef[id] = defID
		xoluJSON(w, 200, map[string]any{"id": 1000 + x.fsmSeq, "state": initial})
	})

	// Atomic multi-entity commit
	mux.HandleFunc("/api/v1/commit", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		var req struct {
			Update struct {
				Entity  string         `json:"entity"`
				ID      int64          `json:"id"`
				Version *int64         `json:"version"`
				Data    map[string]any `json:"data"`
			} `json:"update"`
			Append []struct {
				Entity string         `json:"entity"`
				ID     *int64         `json:"id"`
				Data   map[string]any `json:"data"`
			} `json:"append"`
			FSMWalk *struct {
				Machine int64  `json:"machine"`
				Input   string `json:"input"`
			} `json:"fsm_walk"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			xoluError(w, 400, "XOLU-ST000", "bad json")
			return
		}
		if len(req.Append) > 25 {
			xoluError(w, 400, "XOLU-ST000", "too many appends")
			return
		}
		walkTo := ""
		if req.FSMWalk != nil {
			mid := strconv.FormatInt(req.FSMWalk.Machine, 10)
			state, ok := x.machines[mid]
			if !ok {
				xoluError(w, 404, "XOLU-FSM002", "machine not found")
				return
			}
			def := x.defDocs[x.machDef[mid]]
			transitions, _ := def["transitions"].([]any)
			for _, raw := range transitions {
				tr, _ := raw.(map[string]any)
				if tr["from"] == state && tr["input"] == req.FSMWalk.Input {
					walkTo, _ = tr["to"].(string)
				}
			}
			if walkTo == "" {
				xoluError(w, 409, "XOLU-FSM003", fmt.Sprintf("no transition for input %q from state %q", req.FSMWalk.Input, state))
				return
			}
		}
		updID := strconv.FormatInt(req.Update.ID, 10)
		nextVer := int64(1)
		if req.Update.Version != nil {
			curDoc, ok := x.entities[req.Update.Entity][updID]
			if !ok {
				xoluError(w, 404, "XOLU-ST001", "entity not found")
				return
			}
			if curVer := toInt64Any(curDoc["_version"]); curVer != *req.Update.Version {
				xoluError(w, 409, "XOLU-ST005", "version conflict")
				return
			}
			nextVer = *req.Update.Version + 1
		}
		for _, a := range req.Append {
			if a.ID != nil {
				aid := strconv.FormatInt(*a.ID, 10)
				if _, exists := x.entities[a.Entity][aid]; exists {
					xoluError(w, 409, "XOLU-ST009", "duplicate id")
					return
				}
			}
		}
		x.putLocked(req.Update.Entity, updID, req.Update.Data, nextVer)
		for _, a := range req.Append {
			var aid string
			if a.ID != nil {
				aid = strconv.FormatInt(*a.ID, 10)
			} else {
				x.seq++
				aid = strconv.FormatInt(x.seq, 10)
			}
			x.putLocked(a.Entity, aid, a.Data, 1)
		}
		if req.FSMWalk != nil {
			x.machines[strconv.FormatInt(req.FSMWalk.Machine, 10)] = walkTo
			xoluJSON(w, 200, map[string]any{"status": "ok", "fsm_walk": map[string]any{"current": walkTo}})
			return
		}
		xoluJSON(w, 200, map[string]any{"status": "ok"})
	})

	// OQL query handler supporting FROM <entity> and simple AND equality clauses
	mux.HandleFunc("/api/v1/oql/query", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		var q struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		entity := extractOQLEntity(q.Query)
		store, exists := x.entities[entity]
		if !exists {
			xoluError(w, 400, "XOLU-QL004", fmt.Sprintf("entity '%s' does not exist", entity))
			return
		}
		conds := extractOQLEqConditions(q.Query)
		rows := make([]any, 0, len(store))
		for _, doc := range store {
			if matchesAll(doc, conds) {
				cp := make(map[string]any, len(doc))
				for k, v := range doc {
					cp[k] = v
				}
				rows = append(rows, cp)
			}
		}
		xoluJSON(w, 200, map[string]any{"status": "ok", "data": rows})
	})

	// Generic entity REST routes: /api/v1/{entity} and /api/v1/{entity}/{id}
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		defer x.mu.Unlock()
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		parts := strings.Split(rest, "/")
		entity := parts[0]

		if len(parts) == 1 && r.Method == http.MethodPost {
			var doc map[string]any
			_ = json.NewDecoder(r.Body).Decode(&doc)
			x.seq++
			id := strconv.FormatInt(x.seq, 10)
			x.putLocked(entity, id, doc, 1)
			xoluJSON(w, 201, map[string]any{"id": x.seq, "message": "created"})
			return
		}
		if len(parts) == 2 {
			id := parts[1]
			doc, ok := x.entities[entity][id]
			if !ok {
				xoluError(w, 404, "XOLU-ST001", "entity not found")
				return
			}
			if r.Method == http.MethodGet {
				xoluJSON(w, 200, doc)
				return
			}
			if r.Method == http.MethodPatch {
				var patch map[string]any
				_ = json.NewDecoder(r.Body).Decode(&patch)
				if want, has := patch["_version"]; has {
					if toInt64Any(doc["_version"]) != toInt64Any(want) {
						xoluError(w, 409, "XOLU-ST005", "version conflict")
						return
					}
				}
				for k, v := range patch {
					if k != "_version" {
						doc[k] = v
					}
				}
				doc["_version"] = toInt64Any(doc["_version"]) + 1
				xoluJSON(w, 200, map[string]any{"message": "updated"})
				return
			}
		}
		xoluError(w, 404, "XOLU-ST001", "route not found")
	})

	return mux
}

var (
	reOQLFrom = regexp.MustCompile(`(?i)\bFROM\s+([a-zA-Z0-9_]+)`)
	reOQLEq   = regexp.MustCompile(`([a-zA-Z0-9_]+)\s*=\s*(?:'([^']*)'|([0-9]+))`)
)

func extractOQLEntity(q string) string {
	m := reOQLFrom.FindStringSubmatch(q)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

type oqlEq struct {
	field string
	val   string
}

func extractOQLEqConditions(q string) []oqlEq {
	idx := strings.Index(strings.ToUpper(q), "WHERE ")
	if idx < 0 {
		return nil
	}
	clause := q[idx+6:]
	if ob := strings.Index(strings.ToUpper(clause), " ORDER BY"); ob >= 0 {
		clause = clause[:ob]
	}
	matches := reOQLEq.FindAllStringSubmatch(clause, -1)
	out := make([]oqlEq, 0, len(matches))
	for _, m := range matches {
		val := m[2]
		if val == "" {
			val = m[3]
		}
		out = append(out, oqlEq{field: m[1], val: val})
	}
	return out
}

func matchesAll(doc map[string]any, conds []oqlEq) bool {
	for _, c := range conds {
		got := fmt.Sprint(doc[c.field])
		if f, ok := doc[c.field].(float64); ok && f == float64(int64(f)) {
			got = strconv.FormatInt(int64(f), 10)
		}
		if got != c.val {
			return false
		}
	}
	return true
}

func toInt64Any(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		x, _ := n.Int64()
		return x
	}
	return 0
}

// TestWorkOrders_FullScenario_MCP_E2E executes the complete end-to-end work
// order lifecycle through the full MCP stack (stdio MCP client -> molu ->
// Execution Bridge -> Hub Standalone -> publisher-orders -> wire-level xolu):
//
//  1. Discover all 33 work_orders.* tools via tools/list (unified with 13 primitives).
//  2. Create crew, warehouse and consumable.
//  3. Register initial stock entry (plus idempotent replay check).
//  4. Create a template with workflow states, headers, blocks and consumables.
//  5. Open a work order on the job board (blocks + mobile warehouse in one Commit).
//  6. Verify backlog listing and awaiting_claim rejection before claim.
//  7. Claim the order and confirm backlog is now EMPTY.
//  8. Confirm the execution header, record the consumable load (central -> mobile warehouse),
//     run block 1 and advance step 1.
//  9. Run block 2 and close the order (refused while a block is pending); verify post-close record rejected.
//
// 10. Settle the consumable: used leaves the mobile warehouse, the surplus goes back to central.
// 11. Inspect final order, warehouse and consumable balances.
func TestWorkOrders_FullScenario_MCP_E2E(t *testing.T) {
	const token = "orders-e2e-full-secret"

	fx := newFullWireXolu()
	// Reference records that live outside the work-order write tools: users, unit, asset_type and asset.
	fx.seed("users", "1", map[string]any{"name": "Ana Tecnica", "active": true})
	fx.seed("users", "2", map[string]any{"name": "Luis Operario", "active": true})
	fx.seed("unit", "1", map[string]any{"name": "Unidad", "symbol": "u"})
	fx.seed("asset_types", "5", map[string]any{"name": "Bomba hidraulica"})
	fx.seed("assets", "11", map[string]any{"name": "Bomba Principal #11", "type_id": int64(5)})

	xoluSrv := httptest.NewServer(fx.handler())
	t.Cleanup(xoluSrv.Close)

	registry := hub.NewRegistry()
	provider, err := hub.NewRPIProvider("admit-all", "", "")
	if err != nil {
		t.Fatal(err)
	}
	hubSrv := httptest.NewServer(hub.NewHubHTTPServer(registry, hub.NewDiscovery(registry, provider)).Handler())
	t.Cleanup(hubSrv.Close)

	svc := orders.NewService(orders.NewHTTPSubstrate(xoluSrv.URL, "", 2*time.Second))
	pubSrv := httptest.NewServer(orders.NewServer(svc, token).Handler())
	t.Cleanup(pubSrv.Close)

	registrar := orders.NewRegistrar(hubSrv.URL, pubSrv.URL, orders.DefaultBackoff())
	if err := registrar.Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}

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
	lines := make(chan string, 100)
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

	h := &c1Harness{stdin: stdinW, lines: lines}

	// 1. Unified discovery: 13 primitives + 33 work_orders.* tools = 46 tools.
	listResp := h.rpc(t, "tools/list", map[string]any{})
	tools := listResp["result"].(map[string]any)["tools"].([]any)
	domainCount := 0
	for _, raw := range tools {
		name := raw.(map[string]any)["name"].(string)
		if strings.HasPrefix(name, "work_orders.") {
			domainCount++
		}
	}
	if domainCount != 33 {
		t.Fatalf("discovered %d work_orders.* tools, want 33 (total tools=%d)", domainCount, len(tools))
	}

	// 2. Create crew, central warehouse and consumable.
	crew := e2eSuccess(t, h.call(t, "work_orders.CreateCrew", map[string]any{
		"name":                "Cuadrilla Hidraulica",
		"responsible_user_id": 1,
		"member_user_ids":     []any{1, 2},
	}))
	crewID := crew["id"].(string)

	centralWH := e2eSuccess(t, h.call(t, "work_orders.CreateWarehouse", map[string]any{
		"name":                "Deposito Central",
		"location":            "-34.9011,-56.1645",
		"responsible_user_id": 1,
	}))
	centralWHID := centralWH["id"].(string)

	cons := e2eSuccess(t, h.call(t, "work_orders.CreateConsumable", map[string]any{
		"name":      "Filtro hidraulico",
		"unit_id":   1,
		"stock_min": 3,
	}))
	consID := cons["id"].(string)

	// 3. Initial stock entry (10.0 units) + idempotent replay verification.
	entryReqID := "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	entryArgs := map[string]any{
		"type":                "entry",
		"consumable_id":       consID,
		"user_id":             1,
		"target_warehouse_id": centralWHID,
		"quantity":            10,
		"reason":              "Ingreso inicial de compra",
		"request_id":          entryReqID,
	}
	entry := e2eSuccess(t, h.call(t, "work_orders.RegisterMovement", entryArgs))
	if entry["stock_after"] != 10.0 {
		t.Fatalf("stock entry = %v", entry)
	}
	entryReplay := e2eSuccess(t, h.call(t, "work_orders.RegisterMovement", entryArgs))
	if entryReplay["already_processed"] != true || entryReplay["stock_after"] != 10.0 {
		t.Fatalf("idempotent entry replay = %v", entryReplay)
	}

	// 4. Create template with 3 states, headers, 2 ordered blocks and consumables.
	tpl := e2eSuccess(t, h.call(t, "work_orders.CreateTemplate", map[string]any{
		"name":            "Mantenimiento preventivo bomba",
		"description":     "Cambio de filtro y verificacion",
		"asset_type_id":   5,
		"min_assets":      1,
		"max_assets":      1,
		"workflow_states": []any{"Pendiente", "En curso", "Cerrada"},
		"header_items": []any{
			map[string]any{"tag": "prioridad", "type": "number", "filled_at": "upon_creating", "required": true, "default_value": "2"},
			map[string]any{"tag": "observacion", "type": "text", "filled_at": "upon_executing", "required": true},
		},
		"blocks": []any{
			map[string]any{
				"name":             "Preparacion",
				"responsible_type": "user",
				"lines":            []any{map[string]any{"name": "aislado", "type": "boolean"}},
			},
			map[string]any{
				"name":             "Mantenimiento",
				"responsible_type": "crew",
				"predecessor":      "Preparacion",
				"lines":            []any{map[string]any{"name": "horas", "type": "number"}},
			},
		},
		"consumables_enabled":       true,
		"extra_consumables_allowed": false,
		"consumables":               []any{map[string]any{"consumable_id": consID, "quantity": 2}},
	}))
	tplID := tpl["id"].(string)
	if tpl["supported"] != true || tpl["version"] != 1.0 {
		t.Fatalf("created template = %v", tpl)
	}

	// 5. Open work order on the job board (blocks + mobile warehouse in one Commit).
	order := e2eSuccess(t, h.call(t, "work_orders.CreateOrder", map[string]any{
		"template_id":        tplID,
		"assignment_type":    "board_posting",
		"responsible_type":   "crew",
		"asset_ids":          []any{11},
		"created_by_user_id": 1,
		"block_responsibles": map[string]any{"Preparacion": 1, "Mantenimiento": crewID},
	}))
	orderID := order["id"].(string)
	mobileWHID, _ := order["mobile_warehouse_id"].(string)
	if orderID == "" || mobileWHID != orderID || order["state"] != "Pendiente" {
		t.Fatalf("created order = %v", order)
	}

	// 6. Backlog shows the order; advancing before claiming is rejected as awaiting_claim.
	backlog := e2eSuccess(t, h.call(t, "work_orders.ListOrders", map[string]any{"backlog": true}))
	if backlog["total"] != 1.0 {
		t.Fatalf("backlog before claim = %v", backlog)
	}
	awaiting := e2eFailure(t, h.call(t, "work_orders.AdvanceOrder", map[string]any{
		"order_id": orderID, "input": "complete", "user_id": 1,
	}))
	if awaiting["code"] != "XOLU-INVALID_TRANSITION" || awaiting["details"].(map[string]any)["rule"] != "awaiting_claim" {
		t.Fatalf("advance before claim = %v", awaiting)
	}

	// 7. Claim the order as user 1 (responsible of crew) -> backlog becomes EMPTY.
	claimed := e2eSuccess(t, h.call(t, "work_orders.ClaimOrder", map[string]any{
		"order_id": orderID, "user_id": 1, "responsible_id": crewID,
	}))
	if claimed["claimed_by_user_id"] != "1" {
		t.Fatalf("claim = %v", claimed)
	}
	emptyBacklog := e2eFailure(t, h.call(t, "work_orders.ListOrders", map[string]any{"backlog": true}))
	if emptyBacklog["code"] != taxonomy.PrefixMolu+string(taxonomy.CodeEmpty) {
		t.Fatalf("backlog after claim should be EMPTY, got %v", emptyBacklog)
	}

	// 9. Execution: a block cannot start before the header is confirmed; then
	//    confirm the header, run block 1 and advance (Pendiente -> En curso).
	early := e2eFailure(t, h.call(t, "work_orders.ExecuteBlock", map[string]any{
		"order_id": orderID, "block": "Preparacion", "intent": "start", "user_id": 1,
	}))
	if early["details"].(map[string]any)["rule"] != "header_not_confirmed" {
		t.Fatalf("start before header = %v", early)
	}
	hdr := e2eSuccess(t, h.call(t, "work_orders.ConfirmExecutionHeader", map[string]any{
		"order_id": orderID, "user_id": 1, "header_values": map[string]any{"observacion": "Equipo aislado"},
	}))
	if hdr["confirmed_at"] == "" || hdr["order"].(map[string]any)["header_values"].(map[string]any)["observacion"] != "Equipo aislado" {
		t.Fatalf("confirm header = %v", hdr)
	}
	// Record the load: 6.0 units go from central (10 -> 4) into the mobile warehouse (0 -> 6).
	load := e2eSuccess(t, h.call(t, "work_orders.RecordOrderConsumable", map[string]any{
		"order_id": orderID, "consumable_id": consID, "user_id": 1, "intent": "confirm", "quantity": 6, "origin_warehouse_id": centralWHID,
	}))
	if mv := load["movement"].(map[string]any); mv["stock_after"] != 4.0 || mv["target_stock_after"] != 6.0 || load["consumable"].(map[string]any)["loaded"] != 6.0 {
		t.Fatalf("record consumable = %v", load)
	}
	waits := e2eFailure(t, h.call(t, "work_orders.ExecuteBlock", map[string]any{
		"order_id": orderID, "block": "Mantenimiento", "intent": "start", "user_id": 1,
	}))
	if waits["details"].(map[string]any)["rule"] != "predecessor_pending" {
		t.Fatalf("start with pending predecessor = %v", waits)
	}
	e2eSuccess(t, h.call(t, "work_orders.ExecuteBlock", map[string]any{"order_id": orderID, "block": "Preparacion", "intent": "start", "user_id": 1}))
	b1 := e2eSuccess(t, h.call(t, "work_orders.ExecuteBlock", map[string]any{
		"order_id": orderID, "block": "Preparacion", "intent": "complete", "user_id": 1, "values": map[string]any{"aislado": true},
	}))
	if b1["block"].(map[string]any)["state"] != "completed" || b1["previous"] != "in_progress" {
		t.Fatalf("complete block 1 = %v", b1)
	}
	step1 := e2eSuccess(t, h.call(t, "work_orders.AdvanceOrder", map[string]any{
		"order_id": orderID, "input": "complete", "user_id": 1,
	}))
	if step1["current"] != "En curso" {
		t.Fatalf("step 1 = %v", step1)
	}

	// 11. Closing needs every block completed: refused with block 2 pending,
	//     then run block 2 and close (En curso -> Cerrada).
	notYet := e2eFailure(t, h.call(t, "work_orders.AdvanceOrder", map[string]any{
		"order_id": orderID, "input": "complete", "user_id": 1,
	}))
	if notYet["details"].(map[string]any)["rule"] != "blocks_incomplete" {
		t.Fatalf("close with a pending block = %v", notYet)
	}
	e2eSuccess(t, h.call(t, "work_orders.ExecuteBlock", map[string]any{"order_id": orderID, "block": "Mantenimiento", "intent": "start", "user_id": 1}))
	saved := e2eSuccess(t, h.call(t, "work_orders.ExecuteBlock", map[string]any{
		"order_id": orderID, "block": "Mantenimiento", "intent": "save", "user_id": 1, "values": map[string]any{"horas": 1},
	}))
	if saved["block"].(map[string]any)["state"] != "in_progress" {
		t.Fatalf("save keeps the block in progress: %v", saved)
	}
	e2eSuccess(t, h.call(t, "work_orders.ExecuteBlock", map[string]any{
		"order_id": orderID, "block": "Mantenimiento", "intent": "complete", "user_id": 1, "values": map[string]any{"horas": 1.5},
	}))
	step2 := e2eSuccess(t, h.call(t, "work_orders.AdvanceOrder", map[string]any{
		"order_id": orderID, "input": "complete", "user_id": 1,
	}))
	if step2["current"] != "Cerrada" || step2["order"].(map[string]any)["terminal"] != true {
		t.Fatalf("step 2 = %v", step2)
	}

	// Recording after closure is rejected as final_state.
	postClose := e2eFailure(t, h.call(t, "work_orders.RecordOrderConsumable", map[string]any{
		"order_id": orderID, "consumable_id": consID, "user_id": 1, "intent": "more", "quantity": 1, "origin_warehouse_id": centralWHID,
	}))
	if postClose["code"] != "XOLU-INVALID_TRANSITION" || postClose["details"].(map[string]any)["rule"] != "final_state" {
		t.Fatalf("post-close record = %v", postClose)
	}

	// 12. Settle once finished: 2.5 used leave the mobile warehouse, the 3.5
	//     surplus goes back to central (mobile 0, central 4 + 3.5 = 7.5).
	settled := e2eSuccess(t, h.call(t, "work_orders.SettleOrderConsumable", map[string]any{
		"order_id": orderID, "consumable_id": consID, "user_id": 1, "used": 2.5, "return_warehouse_id": centralWHID,
	}))
	sc := settled["consumable"].(map[string]any)
	if sc["settled"] != true || sc["used"] != 2.5 || sc["returned"] != 3.5 || sc["loaded"] != 6.0 {
		t.Fatalf("settle = %v", settled)
	}
	if again := e2eSuccess(t, h.call(t, "work_orders.SettleOrderConsumable", map[string]any{
		"order_id": orderID, "consumable_id": consID, "user_id": 1, "used": 2.5, "return_warehouse_id": centralWHID,
	})); again["already_settled"] != true {
		t.Fatalf("settle again = %v", again)
	}

	// 13. Final inspection through GetOrder, GetWarehouse and GetConsumable.
	detail := e2eSuccess(t, h.call(t, "work_orders.GetOrder", map[string]any{"order_id": orderID}))
	if detail["state"] != "Cerrada" || detail["terminal"] != true || detail["claimed_by_user_id"] != "1" || detail["header_confirmed_at"] == nil {
		t.Fatalf("final order detail = %v", detail)
	}
	blocks, _ := detail["blocks"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("final blocks = %v", blocks)
	}
	answers, _ := detail["block_answers"].(map[string]any)
	if answers["Preparacion"].(map[string]any)["aislado"] != true || answers["Mantenimiento"].(map[string]any)["horas"] != 1.5 {
		t.Fatalf("final block_answers = %v", answers)
	}

	whDetail := e2eSuccess(t, h.call(t, "work_orders.GetWarehouse", map[string]any{"warehouse_id": centralWHID}))
	whStock := whDetail["stock"].([]any)
	if len(whStock) != 1 || whStock[0].(map[string]any)["quantity"] != 7.5 {
		t.Fatalf("final central warehouse stock = %v", whDetail)
	}

	consDetail := e2eSuccess(t, h.call(t, "work_orders.GetConsumable", map[string]any{"consumable_id": consID}))
	if consDetail["total_stock"] != 7.5 || consDetail["low_stock"] != true {
		t.Fatalf("final consumable detail = %v", consDetail)
	}
}

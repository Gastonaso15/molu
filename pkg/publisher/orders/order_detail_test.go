package orders

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func (f *fakeSubstrate) MachineHistory(_ context.Context, machineID string) ([]HistoryEntry, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.machines[machineID]; !ok {
		return nil, &XoluError{Status: 404, Code: "XOLU-FSM001", Message: "machine not found"}
	}
	return f.history[machineID], nil
}

func seedDetailWorld(fs *fakeSubstrate) {
	fs.put(EntityType, "20", map[string]any{
		"title": "Mantenimiento bomba", "description": "d", "state": "En curso", "workflow_mode": "template",
		"workflow_index": 1, "template_id": 9, "template_version": 2, "assignment_type": AssignmentBoard,
		"responsible_type": "crew", "responsible_id": 7, "assigned_to": "Cuadrilla A", "claimed_by_user_id": 9,
		"asset_ids": []any{11}, "block_count": 2, "mobile_warehouse_id": 20,
		"header_values": map[string]any{"prioridad": 2},
		"block_values":  map[string]any{"b0l0": true},
		"created_at":    "2026-11-05T10:00:00Z", "updated_at": "2026-11-06T11:00:00Z",
		"technician_signature": "data:image/png;base64,AAAA",
	})
	// Blocks are stored in no particular order; one belongs to another order.
	fs.put(workOrderBlocksEntity, "2", map[string]any{
		"work_order_id": 20, "block_index": 1, "predecessor_index": 0, "name": "Mantenimiento",
		"machine_id": 502, "state": "pending", "values": map[string]any{}, "started_at": "", "completed_at": "",
	})
	fs.put(workOrderBlocksEntity, "1", map[string]any{
		"work_order_id": 20, "block_index": 0, "predecessor_index": -1, "name": "Preparacion",
		"machine_id": 501, "state": "completed", "values": map[string]any{"equipo_aislado": true},
		"started_at": "2026-11-05T11:00:00Z", "completed_at": "2026-11-05T12:00:00Z",
	})
	fs.put(workOrderBlocksEntity, "3", map[string]any{"work_order_id": 21, "block_index": 0, "name": "Otra"})

	fs.put("warehouse", "20", map[string]any{
		"name": "Mantenimiento bomba - warehouse", "type": "mobile", "work_order_id": 20, "enabled": true,
		"responsible_user_id": 9,
		"consumables": []any{
			map[string]any{"consumable_id": 1, "stock": 2.5}, // legacy balance, overridden below
			map[string]any{"consumable_id": 3, "stock": 1},   // legacy only
		},
	})
	fs.put("consumable", "1", map[string]any{"name": "Filtro", "consumable_unit": 1, "stock_by_warehouse": map[string]any{"20": 150, "5": 99}})
	fs.put("consumable", "2", map[string]any{"name": "Kit de limpieza", "stock_by_warehouse": map[string]any{"20": 40}})
	fs.put("consumable", "3", map[string]any{"name": "Precinto", "consumable_unit": 1})
	fs.put("consumable", "4", map[string]any{"name": "Otro almacen", "stock_by_warehouse": map[string]any{"5": 10}})
	fs.put("unit", "1", map[string]any{"symbol": "u", "name": "unidad"})
}

func TestGetOrder_TemplateOrderWithBlocks(t *testing.T) {
	fs := newFake()
	seedDetailWorld(fs)
	got, err := NewService(fs).GetOrder(context.Background(), "20")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Mantenimiento bomba" || got.CreatedAt != "2026-11-05T10:00:00Z" || got.UpdatedAt != "2026-11-06T11:00:00Z" ||
		got.ClaimedByUserID != "9" || !got.Signed || got.BlockValues["b0l0"] != true {
		t.Fatalf("order = %+v", got)
	}
	if len(got.Blocks) != 2 {
		t.Fatalf("blocks = %+v", got.Blocks)
	}
	b0, b1 := got.Blocks[0], got.Blocks[1]
	if b0.Index != 0 || b0.Name != "Preparacion" || b0.State != "completed" || b0.PredecessorIndex != nil ||
		b0.Values["equipo_aislado"] != true || b0.StartedAt == "" || b0.CompletedAt == "" {
		t.Errorf("block 0 = %+v", b0)
	}
	if b1.Index != 1 || b1.State != "pending" || b1.PredecessorIndex == nil || *b1.PredecessorIndex != 0 || b1.ID != "2" {
		t.Errorf("block 1 = %+v", b1)
	}
	if got.History != nil {
		t.Errorf("template orders have no FSM history: %+v", got.History)
	}
}

func TestGetOrder_MobileWarehouseStock(t *testing.T) {
	fs := newFake()
	seedDetailWorld(fs)
	got, err := NewService(fs).GetOrder(context.Background(), "20")
	if err != nil {
		t.Fatal(err)
	}
	w := got.MobileWarehouse
	if w == nil || w.ID != "20" || w.Name != "Mantenimiento bomba - warehouse" || !w.Enabled || w.ResponsibleUserID != "9" {
		t.Fatalf("warehouse = %+v", w)
	}
	type line struct {
		id, name, unit string
		qty            float64
	}
	var lines []line
	for _, l := range w.Stock {
		lines = append(lines, line{l.ConsumableID, l.Name, l.Unit, l.Quantity})
	}
	want := []line{{"1", "Filtro", "u", 1.5}, {"2", "Kit de limpieza", "", 0.4}, {"3", "Precinto", "u", 1}}
	if len(lines) != len(want) {
		t.Fatalf("stock = %+v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("stock[%d] = %+v, want %+v", i, lines[i], want[i])
		}
	}
}

func TestGetOrder_WarehouseMissingIsOmitted(t *testing.T) {
	fs := newFake()
	seedDetailWorld(fs)
	delete(fs.entities, "warehouse")
	delete(fs.entities, workOrderBlocksEntity)
	got, err := NewService(fs).GetOrder(context.Background(), "20")
	if err != nil {
		t.Fatal(err)
	}
	if got.MobileWarehouse != nil || len(got.Blocks) != 0 {
		t.Fatalf("got %+v %+v", got.MobileWarehouse, got.Blocks)
	}
}

func TestGetOrder_LegacyOrderHistory(t *testing.T) {
	fs := newFake()
	svc := NewService(fs)
	o, err := svc.CreateOrder(context.Background(), CreateInput{Title: "Legacy"})
	if err != nil {
		t.Fatal(err)
	}
	machineID := idString(fs.entities[EntityType][o.ID]["machine_id"])
	fs.history = map[string][]HistoryEntry{machineID: {
		{At: "2026-11-01T10:00:00Z", To: "draft", Note: "machine created"},
		{At: "2026-11-01T11:00:00Z", From: "draft", Input: "start", To: "in_progress"},
	}}
	got, err := svc.GetOrder(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.History) != 2 || got.History[1].Input != "start" || got.History[1].From != "draft" || got.History[1].To != "in_progress" {
		t.Fatalf("history = %+v", got.History)
	}
	if got.Signed || got.MobileWarehouse != nil || len(got.Blocks) != 0 {
		t.Fatalf("extras = %+v", got)
	}
}

func TestGetOrder_SignedIsABooleanNotTheImage(t *testing.T) {
	fs := newFake()
	seedDetailWorld(fs)
	srv := newTestServer(fs)
	defer srv.Close()
	resp, out := post(t, srv.URL+"/api/v1/orders/get", testToken, `{"order_id":"20"}`)
	if resp.StatusCode != 200 || out["signed"] != true {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "base64") {
		t.Fatalf("the signature image must not travel: %s", raw)
	}
	if blocks, _ := out["blocks"].([]any); len(blocks) != 2 {
		t.Fatalf("blocks = %v", out["blocks"])
	}
	wh, _ := out["mobile_warehouse"].(map[string]any)
	if wh == nil || len(wh["stock"].([]any)) != 3 {
		t.Fatalf("warehouse = %v", out["mobile_warehouse"])
	}
}

func TestGetOrder_SubstrateDownWhileReadingBlocks(t *testing.T) {
	fs := newFake()
	seedDetailWorld(fs)
	svc := NewService(&flakySubstrate{fakeSubstrate: fs, failEntity: workOrderBlocksEntity})
	_, err := svc.GetOrder(context.Background(), "20")
	if _, ok := err.(*UnavailableError); !ok {
		t.Fatalf("err = %T %v", err, err)
	}
}

// flakySubstrate fails FindEntities for one entity type.
type flakySubstrate struct {
	*fakeSubstrate
	failEntity string
}

func (f *flakySubstrate) FindEntities(ctx context.Context, entityType string, q EntityQuery) ([]map[string]any, error) {
	if entityType == f.failEntity {
		return nil, &XoluError{Status: 503, Message: "down"}
	}
	return f.fakeSubstrate.FindEntities(ctx, entityType, q)
}

func TestHTTPSubstrate_MachineHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/fsm/machine/7/history" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"machine":7,"entries":[
			{"at":"2026-11-23T17:17:08Z","from":null,"id":1,"input":null,"note":"machine created","payload":null,"to":"draft","vars":{}},
			{"at":"2026-11-23T17:17:09Z","from":"draft","id":2,"input":"start","payload":{"technician_signature":"x"},"to":"in_progress","vars":{}}]}`))
	}))
	defer srv.Close()
	got, err := NewHTTPSubstrate(srv.URL, "", 0).MachineHistory(context.Background(), "7")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if got[0].From != "" || got[0].Input != "" || got[0].To != "draft" || got[0].Note != "machine created" ||
		got[1].From != "draft" || got[1].Input != "start" || got[1].At != "2026-11-23T17:17:09Z" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestHTTPSubstrate_FindEntitiesOrderBy(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		query = body["query"]
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	h := NewHTTPSubstrate(srv.URL, "", 0)
	if _, err := h.FindEntities(context.Background(), "consumable", EntityQuery{Limit: 10, OrderBy: []string{"id"}}); err != nil {
		t.Fatal(err)
	}
	if query != "SELECT TOP 10 * FROM consumable ORDER BY id DESC" {
		t.Errorf("query = %s", query)
	}
	if _, err := h.FindEntities(context.Background(), "consumable", EntityQuery{OrderBy: []string{"id; DROP"}}); err == nil {
		t.Error("unsafe order field accepted")
	}
}

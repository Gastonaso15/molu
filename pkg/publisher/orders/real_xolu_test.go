package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Runs only when MOLU_REAL_XOLU_URL is set; exercises the service against a live xolu.
func TestRealXolu_Lifecycle(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	svc := NewService(NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0))

	o, err := svc.CreateOrder(ctx, CreateInput{Title: "Real xolu", Description: "d", AssignedTo: "ana"})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	t.Logf("created: %+v", o)
	got, err := svc.GetOrder(ctx, o.ID)
	t.Logf("get: %+v err=%v", got, err)
	if err != nil || got.Title != "Real xolu" || got.State != StateDraft || got.AssignedTo != "ana" {
		t.Fatalf("GetOrder mismatch")
	}

	// complete from draft: no such transition.
	_, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, Payload: map[string]any{"technician_signature": "x"}})
	var te *TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("want TransitionError, got %T %v", err, err)
	}
	t.Logf("illegal advance: message=%q details=%v", te.Message, te.Details)

	r, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputStart})
	if err != nil || r.Current != StateInProgress {
		t.Fatalf("start: %+v err=%v", r, err)
	}

	// complete without a signature: the guard rejects it, verbatim.
	_, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete})
	if !errors.As(err, &te) {
		t.Fatalf("want guard TransitionError, got %T %v", err, err)
	}
	t.Logf("guard rejection: message=%q details=%v", te.Message, te.Details)

	r, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, Payload: map[string]any{"technician_signature": "data:image/png;base64,AAAA"}})
	if err != nil || !r.Order.Terminal || r.Order.State != StateCompleted {
		t.Fatalf("complete: %+v err=%v", r, err)
	}
	if again, err := svc.GetOrder(ctx, o.ID); err != nil || again.State != StateCompleted {
		t.Fatalf("state not denormalized: %+v err=%v", again, err)
	}

	_, err = svc.GetOrder(ctx, "99999999")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("unknown order: want NotFoundError, got %T %v", err, err)
	}

	// Templates: store one with seam-CURE's document shape and read it back.
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	if _, err := sub.CreateEntity(ctx, TemplateEntityType, map[string]any{
		"name": "Real xolu template", "version": 1, "latest": true,
		"workflow_states": []map[string]any{{"name": "Open"}, {"name": "Review"}, {"name": "Done"}},
	}); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	tpls, err := svc.ListTemplates(ctx, ListTemplatesInput{})
	t.Logf("templates: %+v err=%v", tpls, err)
	if err != nil || len(tpls.Templates) == 0 {
		t.Fatalf("ListTemplates: %+v %v", tpls, err)
	}
	if first := tpls.Templates[0]; first.Name != "Real xolu template" || len(first.WorkflowStates) != 3 || !first.Latest {
		t.Fatalf("template parsed wrong: %+v", first)
	}
}

// Opens and walks a template order against a live xolu, with a template,
// crew and assets stored the way seam-CURE stores them.
func TestRealXolu_TemplateOrder(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)

	// Entities that do not exist yet must read as "not found", not as an outage.
	_, err := svc.CreateOrder(ctx, CreateInput{TemplateID: "1", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "1"})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("missing template: want ValidationError, got %T %v", err, err)
	}
	t.Logf("missing template: %v", err)

	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	crew := mk("crews", map[string]any{"name": "Cuadrilla real"})
	assetType := int64(77)
	asset := mk("assets", map[string]any{"name": "Bomba real", "type_id": assetType})
	tpl := mk(TemplateEntityType, map[string]any{
		"name": "Plantilla real", "version": 1, "latest": true, "asset_type_id": assetType, "min_assets": 1, "max_assets": 1,
		"workflow_states": []map[string]any{{"name": "Pendiente"}, {"name": "En curso"}, {"name": "Cerrada"}},
		"header_items": []map[string]any{
			{"tag": "prioridad", "type": "number", "default_value": "2", "to_fill_out": "upon_creating", "required": true},
			{"tag": "observacion", "type": "text", "to_fill_out": "upon_executing", "required": true},
		},
		"blocks": []any{}, "consumables_allowed": false,
	})

	listed, err := svc.ListTemplates(ctx, ListTemplatesInput{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tp := range listed.Templates {
		if tp.ID == tpl {
			found = tp.Supported && len(tp.HeaderItems) == 2 && len(tp.WorkflowStates) == 3
		}
	}
	if !found {
		t.Fatalf("seeded template not listed as usable: %+v", listed)
	}

	o, err := svc.CreateOrder(ctx, CreateInput{
		TemplateID: ID(tpl), AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: ID(crew), AssetIDs: []ID{ID(asset)},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	t.Logf("created: %+v", o)
	if o.State != "Pendiente" || o.AssignedTo != "Cuadrilla real" || o.HeaderValues["prioridad"] == nil {
		t.Fatalf("order = %+v", o)
	}

	// The wrong asset type is refused.
	other := mk("assets", map[string]any{"name": "Otra", "type_id": int64(78)})
	_, err = svc.CreateOrder(ctx, CreateInput{TemplateID: ID(tpl), AssignmentType: AssignmentBoard, ResponsibleType: "user", AssetIDs: []ID{ID(other)}})
	if !errors.As(err, &ve) {
		t.Fatalf("wrong asset type: want ValidationError, got %v", err)
	}

	_, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete})
	if !errors.As(err, &ve) {
		t.Fatalf("missing required answer: want ValidationError, got %v", err)
	}
	r, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, HeaderValues: map[string]any{"observacion": "lista"}})
	if err != nil || r.Current != "En curso" {
		t.Fatalf("step 1: %+v %v", r, err)
	}
	var te *TransitionError
	_, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, WorkflowIndex: new(int)})
	if !errors.As(err, &te) || te.Details["rule"] != "stale_state" {
		t.Fatalf("stale step: want stale_state, got %v", err)
	}
	t.Logf("stale step: %q", te.Message)
	r, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, Payload: map[string]any{"technician_signature": "sig"}})
	if err != nil || r.Current != "Cerrada" || !r.Order.Terminal {
		t.Fatalf("step 2: %+v %v", r, err)
	}

	// What xolu holds is what seam-CURE reads.
	doc, err := sub.GetEntity(ctx, EntityType, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stored document: %v", doc)
	if doc["state"] != "Cerrada" || idString(doc["workflow_index"]) != "2" || doc["workflow_mode"] != "template" ||
		doc["technician_signature"] != "sig" || doc["completed_at"] == nil || idString(doc["machine_id"]) != "0" {
		t.Fatalf("document = %v", doc)
	}

	// xolu itself refuses a write made on a stale version.
	ver := idString(doc["_version"])
	if err := sub.PatchEntity(ctx, EntityType, o.ID, map[string]any{"title": "a", "_version": json.Number(ver)}); err != nil {
		t.Fatalf("patch with the current version: %v", err)
	}
	err = sub.PatchEntity(ctx, EntityType, o.ID, map[string]any{"title": "b", "_version": json.Number(ver)})
	var xe *XoluError
	if !errors.As(err, &xe) || xe.Status != 409 {
		t.Fatalf("stale version: want a 409 from xolu, got %v", err)
	}
	t.Logf("stale version: %v", err)
}

// Exercises ListOrders filters through the real OQL engine: string and numeric
// comparisons, the backlog condition and the created_at range.
func TestRealXolu_ListOrders(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	crew := mk("crews", map[string]any{"name": "Cuadrilla list"})
	assetType := int64(78)
	asset := mk("assets", map[string]any{"name": "Equipo list", "type_id": assetType})
	tpl := mk(TemplateEntityType, map[string]any{
		"name": "Plantilla list", "version": 1, "latest": true, "asset_type_id": assetType, "min_assets": 1, "max_assets": 1,
		"workflow_states": []map[string]any{{"name": "Pendiente"}, {"name": "Cerrada"}},
		"header_items":    []map[string]any{}, "blocks": []any{}, "consumables_allowed": false,
	})
	board, err := svc.CreateOrder(ctx, CreateInput{TemplateID: ID(tpl), AssignmentType: AssignmentBoard, ResponsibleType: "crew", AssetIDs: []ID{ID(asset)}})
	if err != nil {
		t.Fatalf("board order: %v", err)
	}
	direct, err := svc.CreateOrder(ctx, CreateInput{TemplateID: ID(tpl), AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: ID(crew), AssetIDs: []ID{ID(asset)}})
	if err != nil {
		t.Fatalf("direct order: %v", err)
	}

	yes := true
	backlog, err := svc.ListOrders(ctx, ListOrdersInput{Backlog: &yes, TemplateID: ID(tpl)})
	if err != nil || backlog.Total != 1 || backlog.Orders[0].ID != board.ID || !backlog.Orders[0].AwaitingClaim {
		t.Fatalf("backlog = %+v err=%v", backlog, err)
	}
	byCrew, err := svc.ListOrders(ctx, ListOrdersInput{ResponsibleType: "crew", ResponsibleID: ID(crew)})
	if err != nil || byCrew.Total != 1 || byCrew.Orders[0].ID != direct.ID {
		t.Fatalf("by crew = %+v err=%v", byCrew, err)
	}
	byAsset, err := svc.ListOrders(ctx, ListOrdersInput{AssetID: ID(asset), State: "Pendiente"})
	if err != nil || byAsset.Total != 2 {
		t.Fatalf("by asset = %+v err=%v", byAsset, err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	ranged, err := svc.ListOrders(ctx, ListOrdersInput{TemplateID: ID(tpl), CreatedFrom: today, CreatedTo: today})
	if err != nil || ranged.Total != 2 {
		t.Fatalf("today = %+v err=%v", ranged, err)
	}
	none, err := svc.ListOrders(ctx, ListOrdersInput{TemplateID: ID(tpl), CreatedTo: "2000-01-01"})
	if err != nil || none.Total != 0 || none.Orders == nil {
		t.Fatalf("none = %+v err=%v", none, err)
	}
	cut, err := svc.ListOrders(ctx, ListOrdersInput{Limit: 1})
	if err != nil || cut.Total != 1 || !cut.Truncated {
		t.Fatalf("limit = %+v err=%v", cut, err)
	}
	t.Logf("backlog=%+v", backlog.Orders[0])
}

// Reads the demo templates of the work order seed (seeds/workorders/seed-wo-templates.py
// in the work_orders branch): blocks, consumables, actions and rules must come through.
func TestRealXolu_GetTemplate(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	svc := NewService(NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0))
	listed, err := svc.ListTemplates(ctx, ListTemplatesInput{})
	if err != nil {
		t.Fatal(err)
	}
	var inspection, maintenance string
	for _, tp := range listed.Templates {
		switch tp.Name {
		case "Demo WO - Inspeccion y mediciones":
			inspection = tp.ID
		case "Demo WO - Mantenimiento preventivo":
			maintenance = tp.ID
		}
	}
	if inspection == "" || maintenance == "" {
		t.Skip("work order seed templates not loaded (run seed-wo-templates.py)")
	}

	m, err := svc.GetTemplate(ctx, GetTemplateInput{TemplateID: ID(maintenance)})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Blocks) != 2 || m.Blocks[1].Predecessor != "Preparacion" || len(m.Consumables) != 2 ||
		m.Consumables[0].Name != "Filtro de prueba WO" || m.Consumables[1].Quantity != 0.5 || !m.ConsumablesEnabled || len(m.Rules) == 0 {
		t.Fatalf("maintenance = %+v", m)
	}
	i, err := svc.GetTemplate(ctx, GetTemplateInput{TemplateID: ID(inspection)})
	if err != nil {
		t.Fatal(err)
	}
	if len(i.Actions) != 2 || i.Actions[0].Name == "" || len(i.Actions[0].MappingValues) == 0 {
		t.Fatalf("inspection actions = %+v", i.Actions)
	}
	t.Logf("inspection action 0: %+v", i.Actions[0])
	if _, err := svc.GetTemplate(ctx, GetTemplateInput{TemplateID: "99999"}); err == nil {
		t.Fatal("want NotFoundError")
	}
}

// Reads an order with blocks, a mobile warehouse (legacy balance plus a
// stock_by_warehouse consumable) and the FSM history of a legacy order.
func TestRealXolu_GetOrderDetail(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}

	// Legacy order: the history comes from its machine.
	legacy, err := svc.CreateOrder(ctx, CreateInput{Title: "Historial real"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: legacy.ID, Input: InputStart}); err != nil {
		t.Fatal(err)
	}
	ld, err := svc.GetOrder(ctx, legacy.ID)
	if err != nil || len(ld.History) != 2 || ld.History[0].To != "draft" || ld.History[1].Input != "start" || ld.History[1].To != "in_progress" {
		t.Fatalf("history = %+v err=%v", ld.History, err)
	}

	// Template order with blocks and a mobile warehouse.
	unit := mk("unit", map[string]any{"symbol": "uD", "name": "unidad detalle"})
	migrated := mk("consumable", map[string]any{"name": "Migrado detalle", "consumable_unit": json.Number(unit), "enabled": true})
	plain := mk("consumable", map[string]any{"name": "Legado detalle", "enabled": true})
	wh := mk("warehouse", map[string]any{
		"name": "Movil detalle", "type": "mobile", "enabled": true,
		"consumables": []map[string]any{{"consumable_id": json.Number(plain), "stock": 2.5}},
	})
	if err := sub.PatchEntity(ctx, "consumable", migrated, map[string]any{"stock_by_warehouse": map[string]any{wh: 150}}); err != nil {
		t.Fatalf("stock_by_warehouse: %v", err)
	}
	order := mk(EntityType, map[string]any{
		"title": "Con bloques", "state": "Pendiente", "workflow_mode": "template", "workflow_index": 0,
		"mobile_warehouse_id": json.Number(wh), "block_count": 2, "created_at": "2026-11-23T10:00:00Z",
	})
	for i, name := range []string{"Preparacion", "Trabajo"} {
		pred := -1
		if i == 1 {
			pred = 0
		}
		mk("work_order_blocks", map[string]any{
			"work_order_id": json.Number(order), "block_index": 1 - i, "predecessor_index": pred, "name": name,
			"machine_id": 0, "state": "pending", "values": map[string]any{},
		})
	}
	d, err := svc.GetOrder(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Blocks) != 2 || d.Blocks[0].Index != 0 || d.Blocks[1].Index != 1 {
		t.Fatalf("blocks = %+v", d.Blocks)
	}
	w := d.MobileWarehouse
	if w == nil || w.Name != "Movil detalle" || len(w.Stock) != 2 {
		t.Fatalf("warehouse = %+v", w)
	}
	// Sorted by name: "Legado detalle" (legacy 2.5), then "Migrado detalle" (1.5 uD).
	if w.Stock[0].Name != "Legado detalle" || w.Stock[0].Quantity != 2.5 || w.Stock[1].Name != "Migrado detalle" ||
		w.Stock[1].Quantity != 1.5 || w.Stock[1].Unit != "uD" {
		t.Fatalf("stock = %+v", w.Stock)
	}
	t.Logf("detail ok: %d blocks, stock %+v", len(d.Blocks), w.Stock)
}

// Reads crews and third parties the way Seam stores them: numeric user ids in
// responsible_user_id and members, names from the users entity.
func TestRealXolu_People(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	ana := mk("users", map[string]any{"name": "Ana personas", "password_hash": "no-debe-salir"})
	beto := mk("users", map[string]any{"name": "Beto personas"})
	crew := mk("crews", map[string]any{
		"name": "Zeta personas " + tag, "description": "d", "responsible_user_id": json.Number(ana),
		"members": []json.Number{json.Number(ana), json.Number(beto), json.Number("99999")},
	})
	third := mk("outsourced", map[string]any{
		"name": "Zeta tercero " + tag, "description": "d", "responsible_user_id": json.Number(beto),
		"signature": "data:image/png;base64,AAAA", "signature_clarification": "Firmante",
	})

	l, err := svc.ListCrews(ctx, ListCrewsInput{Name: "zeta personas " + tag, MemberUserID: ID(ana)})
	if err != nil || l.Total != 1 || l.Crews[0].ID != crew || len(l.Crews[0].Members) != 3 {
		t.Fatalf("list crews = %+v err=%v", l, err)
	}
	c, err := svc.GetCrew(ctx, GetCrewInput{CrewID: ID(crew)})
	if err != nil || c.Responsible.Name != "Ana personas" || c.Members[1].Name != "Beto personas" || c.Members[2].Name != "" {
		t.Fatalf("crew = %+v err=%v", c, err)
	}
	if raw, _ := json.Marshal(c); strings.Contains(string(raw), "no-debe-salir") {
		t.Fatalf("user data leaked: %s", raw)
	}
	o, err := svc.ListOutsourced(ctx, ListOutsourcedInput{Name: "zeta tercero " + tag, ResponsibleUserID: ID(beto)})
	if err != nil || o.Total != 1 || o.Outsourced[0].ID != third || !o.Outsourced[0].HasSignature {
		t.Fatalf("list outsourced = %+v err=%v", o, err)
	}
	g, err := svc.GetOutsourced(ctx, GetOutsourcedInput{OutsourcedID: ID(third)})
	if err != nil || g.Responsible.Name != "Beto personas" || g.SignatureClarification != "Firmante" {
		t.Fatalf("outsourced = %+v err=%v", g, err)
	}
	if _, err := svc.GetCrew(ctx, GetCrewInput{CrewID: "99999"}); err == nil {
		t.Fatal("want NotFoundError")
	}
}

// Warehouses, consumables and minimum-stock alerts against the real OQL
// engine, with the stock stored the way Seam's movements store it.
func TestRealXolu_Inventory(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36) // every run uses its own names
	owner := mk("users", map[string]any{"name": "Responsable inventario"})
	unit := mk("unit", map[string]any{"symbol": "uI", "name": "unidad inventario"})
	ctype := mk("consumable_types", map[string]any{"name": "Tipo inventario"})
	central := mk("warehouse", map[string]any{"name": "Central inventario " + tag, "enabled": true, "location": "-34.9,-56.1", "responsible_user_id": json.Number(owner), "consumables": []any{}})
	movil := mk("warehouse", map[string]any{"name": "Movil inventario " + tag, "type": "mobile", "enabled": true, "consumables": []any{}})
	cons := mk("consumable", map[string]any{
		"name": "Consumible inventario " + tag, "consumable_type_id": json.Number(ctype), "consumable_unit": json.Number(unit),
		"stock_min": 5, "enabled": true,
		"stock_by_warehouse": map[string]any{central: 300, movil: 700},
	})

	ws, err := svc.ListWarehouses(ctx, ListWarehousesInput{Name: "inventario " + tag})
	if err != nil || ws.Total != 2 || ws.Warehouses[0].Name != "Central inventario "+tag || ws.Warehouses[0].Responsible == nil {
		t.Fatalf("warehouses = %+v err=%v", ws, err)
	}
	mobile := true
	if m, _ := svc.ListWarehouses(ctx, ListWarehousesInput{Name: "inventario " + tag, Mobile: &mobile}); m.Total != 1 || !m.Warehouses[0].Mobile {
		t.Fatalf("mobile = %+v", m)
	}
	w, err := svc.GetWarehouse(ctx, GetWarehouseInput{WarehouseID: ID(central)})
	if err != nil || w.Responsible.Name != "Responsable inventario" || len(w.Stock) != 1 || w.Stock[0].Quantity != 3 || w.Stock[0].Unit != "uI" {
		t.Fatalf("warehouse = %+v err=%v", w, err)
	}
	cl, err := svc.ListConsumables(ctx, ListConsumablesInput{Name: "consumible inventario " + tag})
	if err != nil || cl.Total != 1 {
		t.Fatalf("consumables = %+v err=%v", cl, err)
	}
	c := cl.Consumables[0]
	if c.Type == nil || c.Type.Name != "Tipo inventario" || c.Unit == nil || c.Unit.Name != "uI" || c.TotalStock != 10 || !c.LowStock || c.StockMin != 5 {
		t.Fatalf("consumable = %+v", c)
	}
	d, err := svc.GetConsumable(ctx, GetConsumableInput{ConsumableID: ID(cons)})
	if err != nil || len(d.Stock) != 2 || d.Stock[0].WarehouseName != "Central inventario "+tag || d.Stock[1].Mobile != true {
		t.Fatalf("detail = %+v err=%v", d, err)
	}
	// Only the central warehouse (3 <= 5) is an alert; the mobile one holds 7.
	a, err := svc.ListLowStock(ctx, ListLowStockInput{ConsumableID: ID(cons)})
	if err != nil || a.Total != 1 || a.Alerts[0].WarehouseID != central || a.Alerts[0].Shortfall != 2 {
		t.Fatalf("alerts = %+v err=%v", a, err)
	}
}

// Claims a job-board order against the real xolu: eligibility, the _version
// guard (two users race for the same order and exactly one wins), idempotence
// and the order moving again afterwards.
func TestRealXolu_ClaimOrder(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	userA := mk("users", map[string]any{"name": "Reclamante A " + tag})
	userB := mk("users", map[string]any{"name": "Reclamante B " + tag})
	outsider := mk("users", map[string]any{"name": "Sin cuadrilla " + tag})
	crewA := mk("crews", map[string]any{"name": "Cuadrilla A " + tag, "responsible_user_id": json.Number(userA)})
	_ = mk("crews", map[string]any{"name": "Cuadrilla B " + tag, "responsible_user_id": json.Number(userB)})
	assetType := int64(79)
	asset := mk("assets", map[string]any{"name": "Equipo claim", "type_id": assetType})
	tpl := mk(TemplateEntityType, map[string]any{
		"name": "Plantilla claim " + tag, "version": 1, "latest": true, "asset_type_id": assetType, "min_assets": 1, "max_assets": 1,
		"workflow_states": []map[string]any{{"name": "Pendiente"}, {"name": "Cerrada"}},
		"header_items":    []map[string]any{}, "blocks": []any{}, "consumables_allowed": false,
	})
	open := func() string {
		o, err := svc.CreateOrder(ctx, CreateInput{TemplateID: ID(tpl), AssignmentType: AssignmentBoard, ResponsibleType: "crew", AssetIDs: []ID{ID(asset)}})
		if err != nil {
			t.Fatalf("board order: %v", err)
		}
		return o.ID
	}

	order := open()
	if _, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: order, UserID: ID(outsider)}); err == nil {
		t.Fatal("a user without a crew must not claim")
	} else if te, ok := err.(*TransitionError); !ok || te.Details["rule"] != "ineligible" {
		t.Fatalf("err = %T %v", err, err)
	}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: order, Input: InputComplete}); err == nil {
		t.Fatal("an unclaimed board order must not advance")
	}

	res, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: order, UserID: ID(userA)})
	if err != nil || res.Responsible.ID != crewA || res.Responsible.Name != "Cuadrilla A "+tag || res.AlreadyClaimed {
		t.Fatalf("claim = %+v err=%v", res, err)
	}
	d, err := svc.GetOrder(ctx, order)
	if err != nil || d.ClaimedByUserID != userA || d.ResponsibleID != crewA || d.AssignedTo != "Cuadrilla A "+tag {
		t.Fatalf("detail = %+v err=%v", d, err)
	}
	if again, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: order, UserID: ID(userA)}); err != nil || !again.AlreadyClaimed {
		t.Fatalf("again = %+v err=%v", again, err)
	}
	if _, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: order, UserID: ID(userB)}); err == nil {
		t.Fatal("a second user must not take a claimed order")
	}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: order, Input: InputComplete, Payload: map[string]any{"technician_signature": "firma"}}); err != nil {
		t.Fatalf("a claimed order must advance: %v", err)
	}

	// Two users race for one order: xolu's _version lets exactly one win.
	race := open()
	results := make(chan error, 2)
	for _, u := range []string{userA, userB} {
		go func(u string) {
			_, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: race, UserID: ID(u)})
			results <- err
		}(u)
	}
	var wins, losses int
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			wins++
		} else if te, ok := err.(*TransitionError); ok && te.Details["rule"] == "already_claimed" {
			losses++
		} else {
			t.Fatalf("unexpected race error: %T %v", err, err)
		}
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("race: %d wins, %d losses", wins, losses)
	}
}

// Opens an order from a template with blocks against the real xolu: one
// pending machine per block (bound to its ref), the block records and the order
// saved by a single Commit under a client-chosen id.
func TestRealXolu_CreateOrderWithBlocks(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	crew := mk("crews", map[string]any{"name": "Cuadrilla bloques " + tag})
	assetType := int64(80)
	asset := mk("assets", map[string]any{"name": "Equipo bloques", "type_id": assetType})
	tpl := mk(TemplateEntityType, map[string]any{
		"name": "Plantilla bloques " + tag, "version": 1, "latest": true, "asset_type_id": assetType, "min_assets": 1, "max_assets": 1,
		"workflow_states": []map[string]any{{"name": "Pendiente"}, {"name": "Cerrada"}},
		"header_items":    []map[string]any{},
		"blocks": []map[string]any{
			{"name": "Preparacion", "predecessor": "", "responsible_type": "user", "lines": []map[string]any{{"name": "aislado", "type": "boolean"}}},
			{"name": "Trabajo", "predecessor": "Preparacion", "responsible_type": "crew", "lines": []map[string]any{}},
		},
		"consumables_allowed": false,
	})

	o, err := svc.CreateOrder(ctx, CreateInput{TemplateID: ID(tpl), AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: ID(crew), AssetIDs: []ID{ID(asset)}})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if n := intOf(o.ID); n <= 0 || n >= 1<<52 {
		t.Fatalf("order id %s outside JavaScript's exact range", o.ID)
	}
	d, err := svc.GetOrder(ctx, o.ID)
	if err != nil || d.State != "Pendiente" || len(d.Blocks) != 2 || d.Blocks[0].Name != "Preparacion" || d.Blocks[1].Name != "Trabajo" ||
		d.Blocks[1].PredecessorIndex == nil || *d.Blocks[1].PredecessorIndex != 0 || d.Blocks[0].State != "pending" {
		t.Fatalf("detail = %+v err=%v", d, err)
	}
	doc, err := sub.GetEntity(ctx, EntityType, o.ID)
	if err != nil || intOf(doc["block_count"]) != 2 || intOf(doc["block_execution_version"]) != 1 || doc["creation_token"] == nil {
		t.Fatalf("stored order = %v err=%v", doc, err)
	}
	// Each block has a live machine in its initial state, findable by ref.
	for i := range d.Blocks {
		rows, err := sub.FindEntities(ctx, "work_order_blocks", EntityQuery{Where: []Condition{{Field: "work_order_id", Op: "=", Value: numericOrString(o.ID)}, {Field: "block_index", Op: "=", Value: int64(i)}}, Limit: 1})
		if err != nil || len(rows) != 1 {
			t.Fatalf("block %d record: %v %v", i, rows, err)
		}
		hist, err := sub.MachineHistory(ctx, idString(rows[0]["machine_id"]))
		if err != nil || len(hist) != 1 || hist[0].To != "pending" {
			t.Fatalf("block %d machine history = %+v err=%v", i, hist, err)
		}
	}
	// The block definition is shared: opening a second order reuses it.
	if _, err := svc.CreateOrder(ctx, CreateInput{TemplateID: ID(tpl), AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: ID(crew), AssetIDs: []ID{ID(asset)}}); err != nil {
		t.Fatalf("second order: %v", err)
	}
	listed, _ := svc.ListTemplates(ctx, ListTemplatesInput{})
	for _, tp := range listed.Templates {
		if tp.ID == tpl && !tp.Supported {
			t.Fatalf("template with blocks should be usable: %+v", tp)
		}
	}
}

// A Commit xolu rejects saves nothing: the order id stays free.
func TestRealXolu_CommitIsAtomic(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	orderID := int64(4_000_000_000_000) + time.Now().UnixNano()%1_000_000
	appends := make([]CommitAppend, 26) // one more than a commit can hold
	for i := range appends {
		appends[i] = CommitAppend{Entity: "work_order_blocks", Data: map[string]any{"work_order_id": orderID, "block_index": i}}
	}
	err := sub.Commit(ctx, CommitRequest{Update: CommitUpdate{Entity: EntityType, ID: orderID, Data: map[string]any{"title": "no debe quedar"}}, Append: appends})
	xe, ok := err.(*XoluError)
	if !ok || !commitRejected(xe.Status) {
		t.Fatalf("want a rejected commit, got %T %v", err, err)
	}
	if _, err := sub.GetEntity(ctx, EntityType, strconv.FormatInt(orderID, 10)); !isMissing(err) {
		t.Fatalf("a rejected commit must save nothing, got %v", err)
	}
}

// Advances an order with blocks, answering block lines by name, against the
// real xolu: the answers must land under Seam's keys and read back by name.
func TestRealXolu_AdvanceOrderWithBlockAnswers(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	crew := mk("crews", map[string]any{"name": "Cuadrilla respuestas " + tag})
	assetType := int64(81)
	asset := mk("assets", map[string]any{"name": "Equipo respuestas", "type_id": assetType})
	tpl := mk(TemplateEntityType, map[string]any{
		"name": "Plantilla respuestas " + tag, "version": 1, "latest": true, "asset_type_id": assetType, "min_assets": 1, "max_assets": 1,
		"workflow_states": []map[string]any{{"name": "Pendiente"}, {"name": "En curso"}, {"name": "Cerrada"}},
		"header_items":    []map[string]any{},
		"blocks": []map[string]any{
			{"name": "Preparacion", "predecessor": "", "responsible_type": "user", "lines": []map[string]any{{"name": "aislado", "type": "boolean"}, {"name": "nota", "type": "text"}}},
			{"name": "Trabajo", "predecessor": "Preparacion", "responsible_type": "crew", "lines": []map[string]any{{"name": "horas", "type": "number"}}},
		},
		"consumables_allowed": false,
	})
	o, err := svc.CreateOrder(ctx, CreateInput{TemplateID: ID(tpl), AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: ID(crew), AssetIDs: []ID{ID(asset)}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, BlockValues: map[string]any{"Trabajo": map[string]any{"horas": "muchas"}}}); err == nil {
		t.Fatal("a badly typed answer must be rejected")
	}
	r, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, BlockValues: map[string]any{"Preparacion": map[string]any{"aislado": true, "nota": "listo"}}})
	if err != nil || r.Current != "En curso" {
		t.Fatalf("step 1 = %+v err=%v", r, err)
	}
	r, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete,
		BlockValues: map[string]any{"Trabajo": map[string]any{"horas": 2.5}}, Payload: map[string]any{"technician_signature": "firma"}})
	if err != nil || r.Current != "Cerrada" || !r.Order.Terminal {
		t.Fatalf("step 2 = %+v err=%v", r, err)
	}

	doc, err := sub.GetEntity(ctx, EntityType, o.ID)
	bv, _ := doc["block_values"].(map[string]any)
	if err != nil || bv["template_block_0_line_0"] != true || bv["template_block_0_line_1"] != "listo" || len(bv) != 3 {
		t.Fatalf("stored block_values = %v err=%v", bv, err)
	}
	d, err := svc.GetOrder(ctx, o.ID)
	if err != nil || d.BlockAnswers["Preparacion"]["nota"] != "listo" || d.BlockAnswers["Trabajo"]["horas"] != json.Number("2.5") && d.BlockAnswers["Trabajo"]["horas"] != 2.5 || !d.Signed {
		t.Fatalf("answers = %v signed=%v err=%v", d.BlockAnswers, d.Signed, err)
	}
}

// Verifies AssignOrder against a real xolu: direct assignment to user,
// reassignment to crew and third_party, returning to the board, idempotency,
// and rejection once completed.
func TestRealXolu_AssignOrder(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := mk("users", map[string]any{"name": "Operario " + tag, "email": "op-" + tag + "@example.com", "active": true})
	crew := mk("crews", map[string]any{"name": "Cuadrilla " + tag, "responsible_user_id": intOf(user)})
	out := mk("outsourced", map[string]any{"name": "Proveedor " + tag, "responsible_user_id": intOf(user)})

	order, err := svc.CreateOrder(ctx, CreateInput{Title: "Orden asignable " + tag})
	if err != nil {
		t.Fatal(err)
	}

	// Direct assign to user.
	r1, err := svc.AssignOrder(ctx, AssignInput{OrderID: order.ID, ResponsibleType: "user", ResponsibleID: ID(user)})
	if err != nil || r1.Reassigned || r1.AlreadyAssigned || r1.Order.AssignedTo != "Operario "+tag {
		t.Fatalf("assign user: %+v err=%v", r1, err)
	}
	// Idempotent repeat.
	r1Again, err := svc.AssignOrder(ctx, AssignInput{OrderID: order.ID, ResponsibleType: "user", ResponsibleID: ID(user)})
	if err != nil || !r1Again.AlreadyAssigned {
		t.Fatalf("idempotent assign: %+v err=%v", r1Again, err)
	}
	// Reassign to crew.
	r2, err := svc.AssignOrder(ctx, AssignInput{OrderID: order.ID, ResponsibleType: "crew", ResponsibleID: ID(crew)})
	if err != nil || !r2.Reassigned || r2.Order.ResponsibleID != crew || r2.Order.AssignedTo != "Cuadrilla "+tag {
		t.Fatalf("reassign crew: %+v err=%v", r2, err)
	}
	// Post back to job board.
	r3, err := svc.AssignOrder(ctx, AssignInput{OrderID: order.ID, AssignmentType: AssignmentBoard, ResponsibleType: "third_party"})
	if err != nil || !r3.Reassigned || r3.Order.AssignmentType != AssignmentBoard || r3.Order.ResponsibleID != "" {
		t.Fatalf("board posting: %+v err=%v", r3, err)
	}
	// Reassign to third_party directly.
	r4, err := svc.AssignOrder(ctx, AssignInput{OrderID: order.ID, ResponsibleType: "third_party", ResponsibleID: ID(out)})
	if err != nil || r4.Reassigned || r4.Order.AssignedTo != "Proveedor "+tag {
		t.Fatalf("assign third_party: %+v err=%v", r4, err)
	}
	// Complete legacy order, then assignment must be rejected as final_state.
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: order.ID, Input: InputStart}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: order.ID, Input: InputComplete, Payload: map[string]any{"technician_signature": "firma"}}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: order.ID, ResponsibleType: "crew", ResponsibleID: ID(crew)})
	wantRule(t, err, "final_state")
}

// Verifies RegisterMovement against a real xolu: atomic Commit for entry,
// transfer, exit, adjust, idempotent replay, request_id reuse rejection,
// and insufficient stock rejection.
func TestRealXolu_RegisterMovement(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	unit := mk("unit", map[string]any{"name": "Unidad " + tag, "symbol": "u"})
	w1 := mk(warehouseEntity, map[string]any{"name": "Almacen A " + tag, "enabled": true})
	w2 := mk(warehouseEntity, map[string]any{"name": "Almacen B " + tag, "enabled": true})
	cons := mk(consumableEntity, map[string]any{
		"name": "Insumo " + tag, "unit_id": intOf(unit), "stock_min": 2.0, "enabled": true,
		"stock_by_warehouse": map[string]any{},
	})

	// Entry 10.50 into w1 (with explicit request_id for idempotency test).
	reqID := fmt.Sprintf("%016x%016x", time.Now().UnixNano(), 1)
	e1, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementEntry, ConsumableID: ID(cons), UserID: "1", TargetWarehouseID: ID(w1),
		Quantity: f64(10.5), Reason: "Compra inicial", RequestID: reqID,
	})
	if err != nil || e1.StockBefore != 0 || e1.StockAfter != 10.5 {
		t.Fatalf("entry: %+v err=%v", e1, err)
	}
	// Replay same request_id -> idempotent.
	e1Replay, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementEntry, ConsumableID: ID(cons), UserID: "1", TargetWarehouseID: ID(w1),
		Quantity: f64(10.5), Reason: "Compra inicial", RequestID: reqID,
	})
	if err != nil || !e1Replay.AlreadyProcessed {
		t.Fatalf("replay: %+v err=%v", e1Replay, err)
	}
	// Same request_id with different quantity -> request_reused.
	_, err = svc.RegisterMovement(ctx, MovementInput{
		Type: MovementEntry, ConsumableID: ID(cons), UserID: "1", TargetWarehouseID: ID(w1),
		Quantity: f64(99), Reason: "Compra inicial", RequestID: reqID,
	})
	wantRule(t, err, "request_reused")

	// Transfer 4.25 from w1 to w2.
	tr, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementTransfer, ConsumableID: ID(cons), UserID: "1", OriginWarehouseID: ID(w1), TargetWarehouseID: ID(w2),
		Quantity: f64(4.25), Reason: "Transferencia",
	})
	if err != nil || tr.StockAfter != 6.25 || tr.TargetStockAfter == nil || *tr.TargetStockAfter != 4.25 {
		t.Fatalf("transfer: %+v err=%v", tr, err)
	}
	// Exit 2.00 from w2.
	ex, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementExit, ConsumableID: ID(cons), UserID: "1", OriginWarehouseID: ID(w2),
		Quantity: f64(2), Reason: "Consumo",
	})
	if err != nil || ex.StockAfter != 2.25 {
		t.Fatalf("exit: %+v err=%v", ex, err)
	}
	// Insufficient stock on w2.
	_, err = svc.RegisterMovement(ctx, MovementInput{
		Type: MovementExit, ConsumableID: ID(cons), UserID: "1", OriginWarehouseID: ID(w2),
		Quantity: f64(5), Reason: "Exceso",
	})
	wantRule(t, err, "insufficient_stock")
	// Adjust w1 to 0.
	adj, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementAdjust, ConsumableID: ID(cons), UserID: "1", TargetWarehouseID: ID(w1),
		Quantity: f64(0), Reason: "Ajuste inventario",
	})
	if err != nil || adj.StockBefore != 6.25 || adj.StockAfter != 0 {
		t.Fatalf("adjust: %+v err=%v", adj, err)
	}
}

// Verifies the full mobile warehouse lifecycle against a real xolu: opening an
// order from a template with blocks + consumables, transferring stock into its
// mobile warehouse, consuming, advancing to completion, and returning leftovers.
func TestRealXolu_MobileWarehouseLifecycle(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	unit := mk("unit", map[string]any{"name": "Unidad " + tag, "symbol": "u"})
	central := mk(warehouseEntity, map[string]any{"name": "Central " + tag, "enabled": true})
	cons := mk(consumableEntity, map[string]any{
		"name": "Filtro " + tag, "consumable_unit": intOf(unit), "stock_min": 1.0, "enabled": true,
		"stock_by_warehouse": map[string]any{},
	})
	if _, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementEntry, ConsumableID: ID(cons), UserID: "1", TargetWarehouseID: ID(central),
		Quantity: f64(10), Reason: "Ingreso inicial",
	}); err != nil {
		t.Fatal(err)
	}
	crew := mk("crews", map[string]any{"name": "Cuadrilla movil " + tag})
	assetType := int64(82)
	asset := mk("assets", map[string]any{"name": "Equipo movil " + tag, "type_id": assetType})
	tpl := mk(TemplateEntityType, map[string]any{
		"name": "Plantilla movil " + tag, "version": 1, "latest": true, "asset_type_id": assetType, "min_assets": 1, "max_assets": 1,
		"workflow_states":             []map[string]any{{"name": "Pendiente"}, {"name": "Finalizada"}},
		"header_items":                []map[string]any{},
		"blocks":                      []map[string]any{{"name": "Revision", "predecessor": "", "responsible_type": "crew", "lines": []map[string]any{{"name": "ok", "type": "boolean"}}}},
		"consumables_allowed":         true,
		"add_any_consumables_allowed": false,
		"consumables":                 []map[string]any{{"consumable_id": intOf(cons), "quantity": 2}},
	})

	o, err := svc.CreateOrder(ctx, CreateInput{
		TemplateID: ID(tpl), AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: ID(crew),
		AssetIDs: []ID{ID(asset)}, CreatedByUserID: "1",
	})
	if err != nil || o.MobileWarehouseID != o.ID {
		t.Fatalf("CreateOrder: %+v err=%v", o, err)
	}
	// Load 5 into mobile warehouse.
	if _, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementTransfer, ConsumableID: ID(cons), UserID: "1",
		OriginWarehouseID: ID(central), TargetWarehouseID: ID(o.MobileWarehouseID),
		Quantity: f64(5), Reason: "Carga orden",
	}); err != nil {
		t.Fatal(err)
	}
	// Consume 2 from order.
	consumed, err := svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{
		OrderID: o.ID, ConsumableID: ID(cons), UserID: "1", Quantity: f64(2),
	})
	if err != nil || consumed.Movement.StockAfter != 3 {
		t.Fatalf("ConsumeFromOrder: %+v err=%v", consumed, err)
	}
	// Complete order with block answer + signature.
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{
		OrderID: o.ID, Input: InputComplete,
		BlockValues: map[string]any{"Revision": map[string]any{"ok": true}},
		Payload:     map[string]any{"technician_signature": "firma"},
	}); err != nil {
		t.Fatal(err)
	}
	// Return all remaining leftovers (3) back to central.
	ret, err := svc.ReturnLeftovers(ctx, ReturnLeftoversInput{
		OrderID: o.ID, ConsumableID: ID(cons), TargetWarehouseID: ID(central), UserID: "1",
	})
	if err != nil || ret.Movement.Quantity != 3 || ret.Movement.StockAfter != 0 || *ret.Movement.TargetStockAfter != 8 {
		t.Fatalf("ReturnLeftovers: %+v err=%v", ret, err)
	}
}

// Verifies CreateCrew/UpdateCrew and CreateOutsourced/UpdateOutsourced against
// a real xolu.
func TestRealXolu_PeopleWrite(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	svc := NewService(sub)
	mk := func(entity string, doc map[string]any) string {
		id, err := sub.CreateEntity(ctx, entity, doc)
		if err != nil {
			t.Fatalf("seed %s: %v", entity, err)
		}
		return id
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	u1 := mk(usersEntity, map[string]any{"name": "U1 " + tag, "active": true})
	u2 := mk(usersEntity, map[string]any{"name": "U2 " + tag, "active": true})
	u3 := mk(usersEntity, map[string]any{"name": "U3 " + tag, "active": true})

	crew, err := svc.CreateCrew(ctx, CreateCrewInput{
		Name: "Cuadrilla CRUD " + tag, Description: "Inicial", ResponsibleUserID: ID(u3), MemberUserIDs: []ID{ID(u1), ID(u2)},
	})
	if err != nil || len(crew.Members) != 3 || crew.Responsible.Name != "U3 "+tag {
		t.Fatalf("CreateCrew: %+v err=%v", crew, err)
	}
	desc := "Actualizada"
	crewUpd, err := svc.UpdateCrew(ctx, UpdateCrewInput{CrewID: ID(crew.ID), Description: &desc})
	if err != nil || crewUpd.Description != desc {
		t.Fatalf("UpdateCrew: %+v err=%v", crewUpd, err)
	}

	out, err := svc.CreateOutsourced(ctx, CreateOutsourcedInput{
		Name: "Proveedor CRUD " + tag, ResponsibleUserID: ID(u1), SignatureClarification: "Rep",
	})
	if err != nil || out.Responsible == nil || out.Responsible.UserID != u1 {
		t.Fatalf("CreateOutsourced: %+v err=%v", out, err)
	}
	emptyID := ID("")
	outUpd, err := svc.UpdateOutsourced(ctx, UpdateOutsourcedInput{OutsourcedID: ID(out.ID), ResponsibleUserID: &emptyID})
	if err != nil || outUpd.Responsible != nil {
		t.Fatalf("UpdateOutsourced: %+v err=%v", outUpd, err)
	}
}

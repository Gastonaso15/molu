package orders

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

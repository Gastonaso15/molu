package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func seedMobileWarehouseWorld(fs *fakeSubstrate) {
	seedWorld(fs)
	fs.put(unitEntity, "1", map[string]any{"name": "Unidad", "symbol": "u"})
	fs.put(warehouseEntity, "100", map[string]any{"name": "Deposito Central", "enabled": true, "_version": 1})
	fs.put(consumableEntity, "10", map[string]any{
		"name": "Filtro", "consumable_unit": 1, "stock_min": 2.0, "enabled": true,
		"stock_by_warehouse": map[string]any{"100": 2000}, // 20.00
		"_version":           1,
	})
	fs.put(consumableEntity, "12", map[string]any{
		"name": "Extra", "consumable_unit": 1, "stock_min": 1.0, "enabled": true,
		"stock_by_warehouse": map[string]any{"100": 1000}, // 10.00
		"_version":           1,
	})
	// Template 20: consumables enabled, NO blocks, restricted to consumable 10.
	fs.put(TemplateEntityType, "20", map[string]any{
		"name": "Mantenimiento consumibles", "description": "Con almacen movil", "version": 1, "latest": true,
		"asset_type_id": 5, "min_assets": 1, "max_assets": 1,
		"workflow_states":             []any{map[string]any{"name": "Pendiente"}, map[string]any{"name": "Finalizada"}},
		"header_items":                []any{},
		"blocks":                      []any{},
		"consumables_allowed":         true,
		"add_any_consumables_allowed": false,
		"consumables":                 []any{map[string]any{"consumable_id": 10, "quantity": 2}},
	})
	// Template 21: BOTH blocks AND consumables enabled (extra consumables allowed).
	fs.put(TemplateEntityType, "21", map[string]any{
		"name": "Trabajo completo", "description": "Bloques + almacen movil", "version": 1, "latest": true,
		"asset_type_id": 5, "min_assets": 1, "max_assets": 1,
		"workflow_states":             []any{map[string]any{"name": "Pendiente"}, map[string]any{"name": "Finalizada"}},
		"header_items":                []any{},
		"blocks":                      threeBlocks(),
		"consumables_allowed":         true,
		"add_any_consumables_allowed": true,
		"consumables":                 []any{map[string]any{"consumable_id": 10, "quantity": 1}},
	})
}

func TestCreateOrder_WithMobileWarehouseOnly(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMobileWarehouseWorld(fs)
	svc := NewService(fs)

	o, err := svc.CreateOrder(ctx, CreateInput{
		TemplateID:      "20",
		AssignmentType:  AssignmentDirect,
		ResponsibleType: "crew",
		ResponsibleID:   "7",
		AssetIDs:        []ID{"11"},
		CreatedByUserID: "9",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fs.commits != 1 || fs.machineCalls != 0 {
		t.Fatalf("commits=%d machines=%d, want 1/0", fs.commits, fs.machineCalls)
	}
	if o.MobileWarehouseID != o.ID {
		t.Fatalf("MobileWarehouseID = %q, want %q", o.MobileWarehouseID, o.ID)
	}
	wh := fs.entities[warehouseEntity][o.ID]
	if wh == nil || wh["type"] != "mobile" || wh["enabled"] != true ||
		idString(wh["work_order_id"]) != o.ID || intOf(wh["responsible_user_id"]) != 9 ||
		wh["name"] != "Mantenimiento consumibles - warehouse" {
		t.Fatalf("mobile warehouse doc = %v", wh)
	}

	// GetOrder exposes the mobile warehouse, and AdvanceOrder completes it.
	detail, err := svc.GetOrder(ctx, o.ID)
	if err != nil || detail.MobileWarehouse == nil || detail.MobileWarehouse.ID != o.ID || !detail.MobileWarehouse.Enabled {
		t.Fatalf("detail = %+v err=%v", detail, err)
	}
	adv, err := svc.AdvanceOrder(ctx, AdvanceInput{
		OrderID: o.ID, Input: InputComplete,
		Payload: map[string]any{"technician_signature": "firma"},
	})
	if err != nil || !adv.Order.Terminal || adv.Current != "Finalizada" {
		t.Fatalf("advance = %+v err=%v", adv, err)
	}
}

func TestCreateOrder_WithBlocksAndMobileWarehouse(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMobileWarehouseWorld(fs)
	svc := NewService(fs)

	o, err := svc.CreateOrder(ctx, CreateInput{
		TemplateID:      "21",
		AssignmentType:  AssignmentDirect,
		ResponsibleType: "crew",
		ResponsibleID:   "7",
		AssetIDs:        []ID{"11"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fs.commits != 1 || fs.machineCalls != 3 || len(fs.entities[workOrderBlocksEntity]) != 3 {
		t.Fatalf("commits=%d machines=%d blocks=%d", fs.commits, fs.machineCalls, len(fs.entities[workOrderBlocksEntity]))
	}
	if o.MobileWarehouseID != o.ID || fs.entities[warehouseEntity][o.ID] == nil {
		t.Fatalf("mobile warehouse missing for order %s", o.ID)
	}

	// 25 blocks + 1 mobile warehouse = 26 appends -> exceeds commit limit (25), must fail before creating machines.
	blocks25 := make([]any, 25)
	for i := range blocks25 {
		blocks25[i] = map[string]any{"name": "B" + string(rune('A'+i)), "lines": []any{}}
	}
	fs.entities[TemplateEntityType]["21"]["blocks"] = blocks25
	fs.machineCalls = 0
	_, err = svc.CreateOrder(ctx, CreateInput{
		TemplateID: "21", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7", AssetIDs: []ID{"11"},
	})
	wantInvalid(t, err, "template_id")
	if fs.machineCalls != 0 {
		t.Fatalf("exceeded append limit must fail before creating machines, got %d", fs.machineCalls)
	}

	// ListTemplates marks a 25-block + consumables template as unsupported.
	list, err := svc.ListTemplates(ctx, ListTemplatesInput{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tp := range list.Templates {
		if tp.ID == "21" && (tp.Supported || !strings.Contains(tp.Reason, "25")) {
			t.Fatalf("template 21 should be unsupported due to 25-append limit: %+v", tp)
		}
		if tp.ID == "20" && !tp.Supported {
			t.Fatalf("template 20 (consumables only) should be supported: %+v", tp)
		}
	}
}

func TestConsumeFromOrder_SuccessAllowlistAndIdempotent(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMobileWarehouseWorld(fs)
	svc := NewService(fs)

	// Open order from template 20 (restricted to consumable 10).
	o, err := svc.CreateOrder(ctx, CreateInput{
		TemplateID: "20", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7", AssetIDs: []ID{"11"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Load supplies into the mobile warehouse via RegisterMovement transfer.
	if _, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementTransfer, ConsumableID: "10", UserID: "9",
		OriginWarehouseID: "100", TargetWarehouseID: ID(o.MobileWarehouseID),
		Quantity: f64(5), Reason: "Carga orden",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementTransfer, ConsumableID: "12", UserID: "9",
		OriginWarehouseID: "100", TargetWarehouseID: ID(o.MobileWarehouseID),
		Quantity: f64(3), Reason: "Carga extra",
	}); err != nil {
		t.Fatal(err)
	}

	// Consumable 12 is not allowed by template 20 (add_any_consumables_allowed=false).
	_, err = svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{
		OrderID: o.ID, ConsumableID: "12", UserID: "9", Quantity: f64(1),
	})
	wantRule(t, err, "consumable_not_allowed")

	// Consume 2.5 of consumable 10 with explicit request_id.
	reqID := "1234567890abcdef1234567890abcdef"
	res, err := svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{
		OrderID: o.ID, ConsumableID: "10", UserID: "9", Quantity: f64(2.5), RequestID: reqID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OrderID != o.ID || res.MobileWarehouseID != o.ID || res.Movement.Type != MovementExit ||
		res.Movement.StockBefore != 5 || res.Movement.StockAfter != 2.5 || res.Movement.AlreadyProcessed {
		t.Fatalf("consume result = %+v", res)
	}

	// Replay with same request_id is idempotent.
	replay, err := svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{
		OrderID: o.ID, ConsumableID: "10", UserID: "9", Quantity: f64(2.5), RequestID: reqID,
	})
	if err != nil || !replay.Movement.AlreadyProcessed || replay.Movement.StockAfter != 2.5 {
		t.Fatalf("replay = %+v err=%v", replay, err)
	}

	// Order from template 21 (add_any_consumables_allowed=true) allows consumable 12.
	o2, err := svc.CreateOrder(ctx, CreateInput{
		TemplateID: "21", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7", AssetIDs: []ID{"11"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.RegisterMovement(ctx, MovementInput{
		Type: MovementTransfer, ConsumableID: "12", UserID: "9",
		OriginWarehouseID: "100", TargetWarehouseID: ID(o2.MobileWarehouseID),
		Quantity: f64(2), Reason: "Carga extra",
	})
	res2, err := svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{
		OrderID: o2.ID, ConsumableID: "12", UserID: "9", Quantity: f64(1),
	})
	if err != nil || res2.Movement.StockAfter != 1 {
		t.Fatalf("extra consumable consume = %+v err=%v", res2, err)
	}
}

func TestConsumeFromOrder_RejectionsAndValidation(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMobileWarehouseWorld(fs)
	svc := NewService(fs)

	// Missing order -> NotFoundError
	_, err := svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{OrderID: "999", ConsumableID: "10", UserID: "9", Quantity: f64(1)})
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %T %v", err, err)
	}

	// Order without mobile warehouse -> no_mobile_warehouse
	plain := mustCreate(t, svc, tplInput())
	_, err = svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{OrderID: plain.ID, ConsumableID: "10", UserID: "9", Quantity: f64(1)})
	wantRule(t, err, "no_mobile_warehouse")

	// Completed order -> final_state
	o, _ := svc.CreateOrder(ctx, CreateInput{
		TemplateID: "20", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7", AssetIDs: []ID{"11"},
	})
	_, _ = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, Payload: map[string]any{"technician_signature": "sig"}})
	_, err = svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{OrderID: o.ID, ConsumableID: "10", UserID: "9", Quantity: f64(1)})
	wantRule(t, err, "final_state")

	// Validation errors
	_, err = svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{ConsumableID: "10", UserID: "9", Quantity: f64(1)})
	wantInvalid(t, err, "order_id")
	_, err = svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{OrderID: o.ID, UserID: "9", Quantity: f64(1)})
	wantInvalid(t, err, "consumable_id")
	_, err = svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{OrderID: o.ID, ConsumableID: "10", Quantity: f64(1)})
	wantInvalid(t, err, "user_id")
	_, err = svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{OrderID: o.ID, ConsumableID: "10", UserID: "9"})
	wantInvalid(t, err, "quantity")
	_, err = svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{OrderID: o.ID, ConsumableID: "10", UserID: "9", Quantity: f64(0)})
	wantInvalid(t, err, "quantity")
}

func TestReturnLeftovers_PartialFullReplayAndAfterClose(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMobileWarehouseWorld(fs)
	svc := NewService(fs)

	o, err := svc.CreateOrder(ctx, CreateInput{
		TemplateID: "20", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7", AssetIDs: []ID{"11"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Load 6.00 of consumable 10 into mobile warehouse (central 20 -> 14, mobile 0 -> 6).
	if _, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementTransfer, ConsumableID: "10", UserID: "9",
		OriginWarehouseID: "100", TargetWarehouseID: ID(o.MobileWarehouseID),
		Quantity: f64(6), Reason: "Carga",
	}); err != nil {
		t.Fatal(err)
	}
	// Consume 2.50 (mobile 6 -> 3.5).
	if _, err := svc.ConsumeFromOrder(ctx, ConsumeFromOrderInput{
		OrderID: o.ID, ConsumableID: "10", UserID: "9", Quantity: f64(2.5),
	}); err != nil {
		t.Fatal(err)
	}
	// Complete the order: returning leftovers MUST still work after closure.
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{
		OrderID: o.ID, Input: InputComplete, Payload: map[string]any{"technician_signature": "sig"},
	}); err != nil {
		t.Fatal(err)
	}

	// Partial return: 1.50 back to central (mobile 3.5 -> 2.0, central 14 -> 15.5).
	p1, err := svc.ReturnLeftovers(ctx, ReturnLeftoversInput{
		OrderID: o.ID, ConsumableID: "10", TargetWarehouseID: "100", UserID: "9", Quantity: f64(1.5),
	})
	if err != nil || p1.Movement.StockAfter != 2 || p1.Movement.TargetStockAfter == nil || *p1.Movement.TargetStockAfter != 15.5 {
		t.Fatalf("partial return = %+v err=%v", p1, err)
	}

	// Full return (quantity omitted): returns remaining 2.00 (mobile 2 -> 0, central 15.5 -> 17.5).
	reqID := "fedcba9876543210fedcba9876543210"
	p2, err := svc.ReturnLeftovers(ctx, ReturnLeftoversInput{
		OrderID: o.ID, ConsumableID: "10", TargetWarehouseID: "100", UserID: "9", RequestID: reqID,
	})
	if err != nil || p2.Movement.Quantity != 2 || p2.Movement.StockAfter != 0 || *p2.Movement.TargetStockAfter != 17.5 {
		t.Fatalf("full return = %+v err=%v", p2, err)
	}

	// Replay full return with same request_id -> idempotent (not no_leftovers).
	p2Replay, err := svc.ReturnLeftovers(ctx, ReturnLeftoversInput{
		OrderID: o.ID, ConsumableID: "10", TargetWarehouseID: "100", UserID: "9", RequestID: reqID,
	})
	if err != nil || !p2Replay.Movement.AlreadyProcessed || p2Replay.Movement.Quantity != 2 {
		t.Fatalf("replay full return = %+v err=%v", p2Replay, err)
	}

	// Calling again without stock -> no_leftovers.
	_, err = svc.ReturnLeftovers(ctx, ReturnLeftoversInput{
		OrderID: o.ID, ConsumableID: "10", TargetWarehouseID: "100", UserID: "9",
	})
	wantRule(t, err, "no_leftovers")

	// Order without mobile warehouse -> no_mobile_warehouse.
	plain := mustCreate(t, svc, tplInput())
	_, err = svc.ReturnLeftovers(ctx, ReturnLeftoversInput{
		OrderID: plain.ID, ConsumableID: "10", TargetWarehouseID: "100", UserID: "9", Quantity: f64(1),
	})
	wantRule(t, err, "no_mobile_warehouse")
}

func TestOrderServer_ConsumeAndReturnLeftovers(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMobileWarehouseWorld(fs)
	svc := NewService(fs)
	o, err := svc.CreateOrder(ctx, CreateInput{
		TemplateID: "20", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7", AssetIDs: []ID{"11"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.RegisterMovement(ctx, MovementInput{
		Type: MovementTransfer, ConsumableID: "10", UserID: "9",
		OriginWarehouseID: "100", TargetWarehouseID: ID(o.MobileWarehouseID),
		Quantity: f64(4), Reason: "Carga",
	})

	srv := newTestServer(fs)
	defer srv.Close()

	resp, out := post(t, srv.URL+"/api/v1/orders/consume", testToken,
		`{"order_id":"`+o.ID+`","consumable_id":10,"user_id":9,"quantity":1.5}`)
	if resp.StatusCode != 200 || out["mobile_warehouse_id"] != o.ID {
		t.Fatalf("consume: %d %v", resp.StatusCode, out)
	}

	resp, out = post(t, srv.URL+"/api/v1/orders/return-leftovers", testToken,
		`{"order_id":"`+o.ID+`","consumable_id":10,"target_warehouse_id":100,"user_id":9}`)
	if resp.StatusCode != 200 || out["movement"].(map[string]any)["quantity"] != 2.5 {
		t.Fatalf("return: %d %v", resp.StatusCode, out)
	}

	// No leftovers left -> 409 XOLU-INVALID_TRANSITION
	resp, out = post(t, srv.URL+"/api/v1/orders/return-leftovers", testToken,
		`{"order_id":"`+o.ID+`","consumable_id":10,"target_warehouse_id":100,"user_id":9}`)
	if resp.StatusCode != 409 || out["code"] != "XOLU-INVALID_TRANSITION" || out["details"].(map[string]any)["rule"] != "no_leftovers" {
		t.Fatalf("no leftovers: %d %v", resp.StatusCode, out)
	}

	// Missing token -> 401
	resp, _ = post(t, srv.URL+"/api/v1/orders/consume", "", `{}`)
	if resp.StatusCode != 401 {
		t.Errorf("consume no token: %d", resp.StatusCode)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/return-leftovers", "", `{}`)
	if resp.StatusCode != 401 {
		t.Errorf("return no token: %d", resp.StatusCode)
	}
}

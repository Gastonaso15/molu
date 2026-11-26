package orders

import (
	"context"
	"strings"
	"testing"
)

func seedMobileWarehouseWorld(fs *fakeSubstrate) {
	seedWorld(fs)
	fs.put(usersEntity, "9", map[string]any{"name": "Ana", "active": true})
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
		wh["name"] != "Mantenimiento consumibles - "+warehouseDate() {
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
		TemplateID:        "21",
		AssignmentType:    AssignmentDirect,
		ResponsibleType:   "crew",
		ResponsibleID:     "7",
		AssetIDs:          []ID{"11"},
		BlockResponsibles: map[string]ID{"A": "9", "B": "7", "C": "7"},
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
		blocks25[i] = map[string]any{"name": "B" + string(rune('A'+i)), "responsible_type": "user", "lines": []any{}}
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

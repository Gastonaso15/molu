package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func seedInventoryWrite(fs *fakeSubstrate) {
	fs.put(usersEntity, "1", map[string]any{"name": "Ana", "active": true})
	fs.put(usersEntity, "2", map[string]any{"name": "Luis", "active": true})
	fs.put(usersEntity, "4", map[string]any{"name": "Inactivo", "active": false})
	fs.put(unitEntity, "1", map[string]any{"name": "Metro", "symbol": "m"})
	fs.put(unitEntity, "2", map[string]any{"name": "Litro", "symbol": "L"})
	fs.put(consumableTypesEntity, "10", map[string]any{"name": "Electrico"})
	fs.put(consumableTypesEntity, "11", map[string]any{"name": "Lubricante"})
}

func TestCreateWarehouse_SuccessAndDefaults(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedInventoryWrite(fs)
	svc := NewService(fs)

	w, err := svc.CreateWarehouse(ctx, CreateWarehouseInput{
		Name:              "Deposito Central",
		Description:       "Nave principal",
		Location:          " -34.9011 , -56.1645 ",
		ResponsibleUserID: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.ID == "" || w.Name != "Deposito Central" || !w.Enabled || w.Mobile ||
		w.Location != "-34.9011,-56.1645" || w.Responsible == nil || w.Responsible.UserID != "1" || w.Responsible.Name != "Ana" {
		t.Fatalf("warehouse = %+v", w)
	}
	doc := fs.entities[warehouseEntity][w.ID]
	if doc["enabled"] != true || doc["location"] != "-34.9011,-56.1645" || intOf(doc["responsible_user_id"]) != 1 {
		t.Fatalf("stored warehouse = %v", doc)
	}

	// Explicit enabled=false
	w2, err := svc.CreateWarehouse(ctx, CreateWarehouseInput{
		Name:              "En obra",
		Location:          "0,0",
		ResponsibleUserID: "2",
		Enabled:           boolPtr(false),
	})
	if err != nil || w2.Enabled {
		t.Fatalf("w2 = %+v err=%v", w2, err)
	}
}

func TestCreateWarehouse_Validation(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedInventoryWrite(fs)
	svc := NewService(fs)

	cases := []struct {
		name  string
		in    CreateWarehouseInput
		field string
	}{
		{"empty name", CreateWarehouseInput{Name: " ", Location: "0,0", ResponsibleUserID: "1"}, "name"},
		{"name too long", CreateWarehouseInput{Name: strings.Repeat("a", 201), Location: "0,0", ResponsibleUserID: "1"}, "name"},
		{"missing location", CreateWarehouseInput{Name: "W", ResponsibleUserID: "1"}, "location"},
		{"bad location format", CreateWarehouseInput{Name: "W", Location: "montevideo", ResponsibleUserID: "1"}, "location"},
		{"lat out of range", CreateWarehouseInput{Name: "W", Location: "95,10", ResponsibleUserID: "1"}, "location"},
		{"lng out of range", CreateWarehouseInput{Name: "W", Location: "10,-181", ResponsibleUserID: "1"}, "location"},
		{"missing responsible", CreateWarehouseInput{Name: "W", Location: "0,0"}, "responsible_user_id"},
		{"unknown responsible", CreateWarehouseInput{Name: "W", Location: "0,0", ResponsibleUserID: "99"}, "responsible_user_id"},
		{"inactive responsible", CreateWarehouseInput{Name: "W", Location: "0,0", ResponsibleUserID: "4"}, "responsible_user_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateWarehouse(ctx, tc.in)
			wantInvalid(t, err, tc.field)
		})
	}
}

func TestUpdateWarehouse_PartialDisableGuardAndErrors(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedInventoryWrite(fs)
	fs.put(warehouseEntity, "50", map[string]any{
		"name": "Central", "description": "d", "location": "-34.9,-56.1",
		"responsible_user_id": int64(1), "enabled": true, "_version": int64(1),
	})
	fs.put(consumableEntity, "10", map[string]any{
		"name": "Cable", "consumable_unit": int64(1), "stock_min": 1.0, "enabled": true,
		"stock_by_warehouse": map[string]any{"50": 250}, "_version": int64(1),
	})
	svc := NewService(fs)

	// Disabling while stock > 0 is blocked with warehouse_has_stock.
	_, err := svc.UpdateWarehouse(ctx, UpdateWarehouseInput{WarehouseID: "50", Enabled: boolPtr(false)})
	wantRule(t, err, "warehouse_has_stock")

	// Partial update of metadata works even while warehouse holds stock.
	upd, err := svc.UpdateWarehouse(ctx, UpdateWarehouseInput{
		WarehouseID:       "50",
		Name:              strPtr("Central Norte"),
		Location:          strPtr(" -34.8 , -56.2 "),
		ResponsibleUserID: idPtr("2"),
	})
	if err != nil || upd.Name != "Central Norte" || upd.Location != "-34.8,-56.2" || upd.Responsible.UserID != "2" || upd.Responsible.Name != "Luis" {
		t.Fatalf("updated = %+v err=%v", upd, err)
	}

	// Empty the stock -> now disabling succeeds!
	fs.entities[consumableEntity]["10"]["stock_by_warehouse"] = map[string]any{"50": 0}
	disabled, err := svc.UpdateWarehouse(ctx, UpdateWarehouseInput{WarehouseID: "50", Enabled: boolPtr(false)})
	if err != nil || disabled.Enabled {
		t.Fatalf("disabled = %+v err=%v", disabled, err)
	}

	// Re-enabling succeeds.
	enabled, err := svc.UpdateWarehouse(ctx, UpdateWarehouseInput{WarehouseID: "50", Enabled: boolPtr(true)})
	if err != nil || !enabled.Enabled {
		t.Fatalf("enabled = %+v err=%v", enabled, err)
	}

	// Empty update rejected.
	_, err = svc.UpdateWarehouse(ctx, UpdateWarehouseInput{WarehouseID: "50"})
	wantInvalid(t, err, "warehouse_id")

	// Missing warehouse -> NotFoundError.
	_, err = svc.UpdateWarehouse(ctx, UpdateWarehouseInput{WarehouseID: "999", Name: strPtr("X")})
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Kind != "warehouse" {
		t.Fatalf("want NotFoundError(warehouse), got %T %v", err, err)
	}

	// Version conflict -> stale_state.
	fs.patchErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "conflict"}
	_, err = svc.UpdateWarehouse(ctx, UpdateWarehouseInput{WarehouseID: "50", Description: strPtr("otra")})
	wantRule(t, err, "stale_state")
}

func TestCreateConsumable_SuccessAndValidation(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedInventoryWrite(fs)
	svc := NewService(fs)

	c, err := svc.CreateConsumable(ctx, CreateConsumableInput{
		Name:             "Cable 2.5mm",
		Description:      "Cobre",
		ConsumableTypeID: "10",
		UnitID:           "1",
		StockMin:         f64(5.5),
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.Name != "Cable 2.5mm" || !c.Enabled || c.StockMin != 5.5 ||
		c.Type == nil || c.Type.ID != "10" || c.Type.Name != "Electrico" ||
		c.Unit == nil || c.Unit.ID != "1" || c.Unit.Name != "m" {
		t.Fatalf("consumable = %+v", c)
	}
	doc := fs.entities[consumableEntity][c.ID]
	if intOf(doc["consumable_unit"]) != 1 || intOf(doc["consumable_type_id"]) != 10 || doc["enabled"] != true {
		t.Fatalf("stored consumable = %v", doc)
	}
	if _, hasMap := doc["stock_by_warehouse"]; !hasMap {
		t.Fatalf("stock_by_warehouse should be initialized: %v", doc)
	}

	// Without consumable_type_id and stock_min is valid (defaults stock_min=0, enabled=true).
	c2, err := svc.CreateConsumable(ctx, CreateConsumableInput{Name: "Aceite", UnitID: "2", Enabled: boolPtr(false)})
	if err != nil || c2.Type != nil || c2.StockMin != 0 || c2.Enabled {
		t.Fatalf("c2 = %+v err=%v", c2, err)
	}

	cases := []struct {
		name  string
		in    CreateConsumableInput
		field string
	}{
		{"empty name", CreateConsumableInput{Name: " ", UnitID: "1"}, "name"},
		{"long name", CreateConsumableInput{Name: strings.Repeat("x", 201), UnitID: "1"}, "name"},
		{"missing unit", CreateConsumableInput{Name: "A"}, "unit_id"},
		{"unknown unit", CreateConsumableInput{Name: "A", UnitID: "99"}, "unit_id"},
		{"unknown type", CreateConsumableInput{Name: "A", UnitID: "1", ConsumableTypeID: "99"}, "consumable_type_id"},
		{"negative stock_min", CreateConsumableInput{Name: "A", UnitID: "1", StockMin: f64(-1)}, "stock_min"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateConsumable(ctx, tc.in)
			wantInvalid(t, err, tc.field)
		})
	}
}

func TestUpdateConsumable_PartialClearTypeAndErrors(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedInventoryWrite(fs)
	fs.put(consumableEntity, "20", map[string]any{
		"name": "Aceite", "description": "d", "consumable_type_id": int64(11), "consumable_unit": int64(2),
		"stock_min": 2.0, "enabled": true, "stock_by_warehouse": map[string]any{}, "_version": int64(1),
	})
	svc := NewService(fs)

	// Partial update: rename, change stock_min, clear type via "".
	upd, err := svc.UpdateConsumable(ctx, UpdateConsumableInput{
		ConsumableID:     "20",
		Name:             strPtr("Aceite sintetico"),
		ConsumableTypeID: idPtr(""),
		StockMin:         f64(4.5),
		Enabled:          boolPtr(false),
	})
	if err != nil || upd.Name != "Aceite sintetico" || upd.Type != nil || upd.StockMin != 4.5 || upd.Enabled {
		t.Fatalf("upd = %+v err=%v", upd, err)
	}

	// Empty update rejected.
	_, err = svc.UpdateConsumable(ctx, UpdateConsumableInput{ConsumableID: "20"})
	wantInvalid(t, err, "consumable_id")

	// Missing consumable -> NotFoundError.
	_, err = svc.UpdateConsumable(ctx, UpdateConsumableInput{ConsumableID: "999", Name: strPtr("X")})
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Kind != "consumable" {
		t.Fatalf("want NotFoundError(consumable), got %T %v", err, err)
	}

	// Version conflict -> stale_state.
	fs.patchErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "conflict"}
	_, err = svc.UpdateConsumable(ctx, UpdateConsumableInput{ConsumableID: "20", Description: strPtr("otra")})
	wantRule(t, err, "stale_state")
}

func TestOrderServer_InventoryWrite(t *testing.T) {
	fs := newFake()
	seedInventoryWrite(fs)
	srv := newTestServer(fs)
	defer srv.Close()

	// CreateWarehouse
	resp, out := post(t, srv.URL+"/api/v1/orders/warehouses/create", testToken,
		`{"name":"Deposito Sur","location":"-34.9,-56.1","responsible_user_id":1}`)
	if resp.StatusCode != 200 || out["name"] != "Deposito Sur" {
		t.Fatalf("create warehouse: %d %v", resp.StatusCode, out)
	}
	whID := out["id"].(string)

	// CreateConsumable
	resp, out = post(t, srv.URL+"/api/v1/orders/consumables/create", testToken,
		`{"name":"Grasa","unit_id":2,"consumable_type_id":11,"stock_min":3}`)
	if resp.StatusCode != 200 || out["name"] != "Grasa" {
		t.Fatalf("create consumable: %d %v", resp.StatusCode, out)
	}
	consID := out["id"].(string)

	// Seed stock in whID so disabling returns 409 warehouse_has_stock.
	fs.entities[consumableEntity][consID]["stock_by_warehouse"] = map[string]any{whID: 100}
	resp, out = post(t, srv.URL+"/api/v1/orders/warehouses/update", testToken,
		`{"warehouse_id":"`+whID+`","enabled":false}`)
	if resp.StatusCode != 409 || out["code"] != "XOLU-INVALID_TRANSITION" || out["details"].(map[string]any)["rule"] != "warehouse_has_stock" {
		t.Fatalf("disable stocked warehouse: %d %v", resp.StatusCode, out)
	}

	// UpdateConsumable
	resp, out = post(t, srv.URL+"/api/v1/orders/consumables/update", testToken,
		`{"consumable_id":"`+consID+`","stock_min":5}`)
	if resp.StatusCode != 200 || out["stock_min"] != 5.0 {
		t.Fatalf("update consumable: %d %v", resp.StatusCode, out)
	}

	// Not found & unauthorized
	resp, _ = post(t, srv.URL+"/api/v1/orders/warehouses/update", testToken, `{"warehouse_id":999,"name":"X"}`)
	if resp.StatusCode != 404 {
		t.Errorf("missing warehouse: %d", resp.StatusCode)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/consumables/create", "", `{}`)
	if resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}

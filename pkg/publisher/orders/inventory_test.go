package orders

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func seedInventory(fs *fakeSubstrate) {
	fs.put("warehouse", "1", map[string]any{"name": "Central", "description": "Deposito principal", "location": "-34.9,-56.1", "enabled": true, "responsible_user_id": 10, "consumables": []any{}})
	fs.put("warehouse", "2", map[string]any{"name": "Deposito norte", "enabled": false, "consumables": []any{}})
	fs.put("warehouse", "20", map[string]any{
		"name": "OT 20 - warehouse", "type": "mobile", "work_order_id": 20, "enabled": true,
		"consumables": []any{map[string]any{"consumable_id": 3, "stock": 1}},
	})
	fs.put("consumable", "1", map[string]any{"name": "Filtro", "description": "Filtro de aceite", "consumable_type_id": 1, "consumable_unit": 1, "stock_min": 5, "enabled": true,
		"stock_by_warehouse": map[string]any{"1": 300, "2": 500, "20": 150}})
	fs.put("consumable", "2", map[string]any{"name": "Kit de limpieza", "consumable_type_id": 1, "consumable_unit": 1, "stock_min": 1, "enabled": true,
		"stock_by_warehouse": map[string]any{"1": 1000}})
	fs.put("consumable", "3", map[string]any{"name": "Precinto", "consumable_type_id": 1, "consumable_unit": 1, "stock_min": 2, "enabled": true}) // legacy stock
	fs.put("consumable", "4", map[string]any{"name": "Sin stock", "stock_min": 0, "enabled": false, "stock_by_warehouse": map[string]any{}})
	fs.put("consumable_types", "1", map[string]any{"name": "Insumos"})
	fs.put("unit", "1", map[string]any{"symbol": "u", "name": "unidad"})
	fs.put("users", "10", map[string]any{"name": "Ana", "password_hash": "SECRET-HASH"})
}

func whNames(l WarehouseList) string {
	var n []string
	for _, w := range l.Warehouses {
		n = append(n, w.Name)
	}
	return strings.Join(n, ",")
}

func consNames(l ConsumableList) string {
	var n []string
	for _, c := range l.Consumables {
		n = append(n, c.Name)
	}
	return strings.Join(n, ",")
}

func TestListWarehouses(t *testing.T) {
	fs := newFake()
	seedInventory(fs)
	svc := NewService(fs)
	yes, no := true, false
	cases := []struct {
		name string
		in   ListWarehousesInput
		want string
	}{
		{"all", ListWarehousesInput{}, "Central,Deposito norte,OT 20 - warehouse"},
		{"enabled", ListWarehousesInput{Enabled: &yes}, "Central,OT 20 - warehouse"},
		{"disabled", ListWarehousesInput{Enabled: &no}, "Deposito norte"},
		{"mobile", ListWarehousesInput{Mobile: &yes}, "OT 20 - warehouse"},
		{"not mobile", ListWarehousesInput{Mobile: &no}, "Central,Deposito norte"},
		{"name", ListWarehousesInput{Name: "NORTE"}, "Deposito norte"},
		{"limit", ListWarehousesInput{Limit: 1}, "Central"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := svc.ListWarehouses(context.Background(), c.in)
			if err != nil || whNames(out) != c.want || out.Total != len(out.Warehouses) {
				t.Errorf("got %q total=%d err=%v, want %q", whNames(out), out.Total, err, c.want)
			}
		})
	}
	out, _ := svc.ListWarehouses(context.Background(), ListWarehousesInput{})
	c, m := out.Warehouses[0], out.Warehouses[2]
	if c.ID != "1" || c.Location != "-34.9,-56.1" || !c.Enabled || c.Mobile || c.Responsible == nil || c.Responsible.UserID != "10" || c.Responsible.Name != "" {
		t.Errorf("central = %+v", c)
	}
	if !m.Mobile || m.WorkOrderID != "20" {
		t.Errorf("mobile = %+v", m)
	}
}

func TestGetWarehouse_StockAndResponsible(t *testing.T) {
	fs := newFake()
	seedInventory(fs)
	svc := NewService(fs)
	w, err := svc.GetWarehouse(context.Background(), GetWarehouseInput{WarehouseID: "1"})
	if err != nil || w.Responsible.Name != "Ana" || w.Description != "Deposito principal" {
		t.Fatalf("warehouse = %+v err=%v", w, err)
	}
	if len(w.Stock) != 2 || w.Stock[0].Name != "Filtro" || w.Stock[0].Quantity != 3 || w.Stock[0].Unit != "u" ||
		w.Stock[1].Name != "Kit de limpieza" || w.Stock[1].Quantity != 10 {
		t.Fatalf("stock = %+v", w.Stock)
	}
	m, err := svc.GetWarehouse(context.Background(), GetWarehouseInput{WarehouseID: "20"})
	if err != nil || len(m.Stock) != 2 || m.Stock[0].Name != "Filtro" || m.Stock[0].Quantity != 1.5 || m.Stock[1].Name != "Precinto" || m.Stock[1].Quantity != 1 {
		t.Fatalf("mobile = %+v err=%v", m, err)
	}
	raw, _ := json.Marshal(w)
	if strings.Contains(string(raw), "SECRET-HASH") {
		t.Fatalf("leak: %s", raw)
	}
	fs.put("warehouse", "3", map[string]any{"name": "Vacio", "enabled": true})
	empty, _ := svc.GetWarehouse(context.Background(), GetWarehouseInput{WarehouseID: "3"})
	if empty.Stock == nil || len(empty.Stock) != 0 {
		t.Errorf("empty warehouse stock = %#v", empty.Stock)
	}
	_, err = svc.GetWarehouse(context.Background(), GetWarehouseInput{WarehouseID: "404"})
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Error() != `warehouse "404" not found` {
		t.Fatalf("err = %T %v", err, err)
	}
	_, err = svc.GetWarehouse(context.Background(), GetWarehouseInput{})
	wantInvalid(t, err, "warehouse_id")
}

func TestListConsumables(t *testing.T) {
	fs := newFake()
	seedInventory(fs)
	svc := NewService(fs)
	yes, no := true, false
	cases := []struct {
		name string
		in   ListConsumablesInput
		want string
	}{
		{"all", ListConsumablesInput{}, "Filtro,Kit de limpieza,Precinto,Sin stock"},
		{"low stock", ListConsumablesInput{LowStock: &yes}, "Filtro,Precinto"},
		{"not low", ListConsumablesInput{LowStock: &no}, "Kit de limpieza,Sin stock"},
		{"disabled", ListConsumablesInput{Enabled: &no}, "Sin stock"},
		{"type", ListConsumablesInput{ConsumableTypeID: "1"}, "Filtro,Kit de limpieza,Precinto"},
		{"name", ListConsumablesInput{Name: "kit"}, "Kit de limpieza"},
		{"limit", ListConsumablesInput{Limit: 2}, "Filtro,Kit de limpieza"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := svc.ListConsumables(context.Background(), c.in)
			if err != nil || consNames(out) != c.want {
				t.Errorf("got %q err=%v, want %q", consNames(out), err, c.want)
			}
		})
	}
	out, _ := svc.ListConsumables(context.Background(), ListConsumablesInput{})
	f, k := out.Consumables[0], out.Consumables[1]
	if f.ID != "1" || f.Type == nil || f.Type.Name != "Insumos" || f.Unit == nil || f.Unit.Name != "u" || f.StockMin != 5 ||
		f.TotalStock != 9.5 || !f.LowStock || !f.Enabled || f.Description != "Filtro de aceite" {
		t.Errorf("filtro = %+v", f)
	}
	if k.LowStock || k.TotalStock != 10 {
		t.Errorf("kit = %+v", k)
	}
	if out.Consumables[3].Type != nil || out.Consumables[3].Unit != nil {
		t.Errorf("no type/unit expected: %+v", out.Consumables[3])
	}
}

func TestGetConsumable_StockPerWarehouse(t *testing.T) {
	fs := newFake()
	seedInventory(fs)
	svc := NewService(fs)
	c, err := svc.GetConsumable(context.Background(), GetConsumableInput{ConsumableID: "1"})
	if err != nil || c.Name != "Filtro" || !c.LowStock || c.TotalStock != 9.5 {
		t.Fatalf("consumable = %+v err=%v", c, err)
	}
	type row struct {
		id, name string
		mobile   bool
		qty      float64
	}
	var got []row
	for _, s := range c.Stock {
		got = append(got, row{s.WarehouseID, s.WarehouseName, s.Mobile, s.Quantity})
	}
	want := []row{{"1", "Central", false, 3}, {"2", "Deposito norte", false, 5}, {"20", "OT 20 - warehouse", true, 1.5}}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("stock = %+v", got)
	}
	legacy, _ := svc.GetConsumable(context.Background(), GetConsumableInput{ConsumableID: "3"})
	if len(legacy.Stock) != 1 || legacy.Stock[0].WarehouseID != "20" || legacy.Stock[0].Quantity != 1 || !legacy.LowStock {
		t.Fatalf("legacy = %+v", legacy)
	}
	none, _ := svc.GetConsumable(context.Background(), GetConsumableInput{ConsumableID: "4"})
	if none.Stock == nil || len(none.Stock) != 0 || none.LowStock {
		t.Fatalf("none = %+v", none)
	}
	_, err = svc.GetConsumable(context.Background(), GetConsumableInput{ConsumableID: "404"})
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Error() != `consumable "404" not found` {
		t.Fatalf("err = %T %v", err, err)
	}
	_, err = svc.GetConsumable(context.Background(), GetConsumableInput{})
	wantInvalid(t, err, "consumable_id")
}

func alertKeys(l LowStockList) string {
	var k []string
	for _, a := range l.Alerts {
		k = append(k, a.ConsumableName+"@"+a.WarehouseName)
	}
	return strings.Join(k, ",")
}

func TestListLowStock(t *testing.T) {
	fs := newFake()
	seedInventory(fs)
	svc := NewService(fs)
	out, err := svc.ListLowStock(context.Background(), ListLowStockInput{})
	if err != nil {
		t.Fatal(err)
	}
	// Largest shortfall first: reaching the minimum exactly (Deposito norte) counts, with shortfall 0.
	want := "Filtro@OT 20 - warehouse,Filtro@Central,Precinto@OT 20 - warehouse,Filtro@Deposito norte"
	if alertKeys(out) != want || out.Total != 4 {
		t.Fatalf("alerts = %s total=%d", alertKeys(out), out.Total)
	}
	a := out.Alerts[0]
	if a.ConsumableID != "1" || a.WarehouseID != "20" || !a.WarehouseMobile || a.Quantity != 1.5 || a.StockMin != 5 || a.Shortfall != 3.5 || a.Unit != "u" {
		t.Errorf("first alert = %+v", a)
	}
	for in, want := range map[string]ListLowStockInput{
		"Filtro@Central":             {WarehouseID: "1"},
		"Precinto@OT 20 - warehouse": {ConsumableID: "3"},
		"Filtro@OT 20 - warehouse":   {WarehouseID: "20", ConsumableID: "1"},
		"Filtro@OT 20 - warehouse,Filtro@Central,Filtro@Deposito norte": {ConsumableID: "1"},
	} {
		got, err := svc.ListLowStock(context.Background(), want)
		if err != nil || alertKeys(got) != in {
			t.Errorf("%+v -> %s err=%v, want %s", want, alertKeys(got), err, in)
		}
	}
	cut, _ := svc.ListLowStock(context.Background(), ListLowStockInput{Limit: 2})
	if cut.Total != 2 || !cut.Truncated {
		t.Errorf("limit = %+v", cut)
	}
	none, err := svc.ListLowStock(context.Background(), ListLowStockInput{WarehouseID: "2", ConsumableID: "2"})
	if err != nil || none.Total != 0 || none.Alerts == nil {
		t.Errorf("none = %+v err=%v", none, err)
	}
}

func TestInventory_EmptyMissingEntitiesAndValidation(t *testing.T) {
	svc := NewService(newFake())
	ctx := context.Background()
	if w, err := svc.ListWarehouses(ctx, ListWarehousesInput{}); err != nil || w.Total != 0 || w.Warehouses == nil {
		t.Errorf("warehouses = %+v err=%v", w, err)
	}
	if c, err := svc.ListConsumables(ctx, ListConsumablesInput{}); err != nil || c.Total != 0 || c.Consumables == nil {
		t.Errorf("consumables = %+v err=%v", c, err)
	}
	if l, err := svc.ListLowStock(ctx, ListLowStockInput{}); err != nil || l.Total != 0 || l.Alerts == nil {
		t.Errorf("low stock = %+v err=%v", l, err)
	}
	_, err := svc.ListConsumables(ctx, ListConsumablesInput{ConsumableTypeID: "x", Limit: 999})
	wantInvalid(t, err, "consumable_type_id", "limit")
	_, err = svc.ListLowStock(ctx, ListLowStockInput{WarehouseID: "0", ConsumableID: "-2"})
	wantInvalid(t, err, "warehouse_id", "consumable_id")
	fs := newFake()
	fs.failWith = &XoluError{Status: 503, Message: "down"}
	_, err = NewService(fs).ListLowStock(ctx, ListLowStockInput{})
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %T %v", err, err)
	}
}

func TestOrderServer_InventoryRoutes(t *testing.T) {
	fs := newFake()
	seedInventory(fs)
	srv := newTestServer(fs)
	defer srv.Close()
	for path, body := range map[string]string{
		"/warehouses/list":  `{"mobile":false}`,
		"/warehouses/get":   `{"warehouse_id":1}`,
		"/consumables/list": `{"low_stock":true}`,
		"/consumables/get":  `{"consumable_id":"1"}`,
		"/stock/low":        `{}`,
	} {
		resp, out := post(t, srv.URL+"/api/v1/orders"+path, testToken, body)
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d %v", path, resp.StatusCode, out)
		}
	}
	_, out := post(t, srv.URL+"/api/v1/orders/stock/low", testToken, `{}`)
	if out["total"].(float64) != 4 {
		t.Errorf("low stock = %v", out)
	}
	resp, _ := post(t, srv.URL+"/api/v1/orders/warehouses/get", testToken, `{"warehouse_id":404}`)
	if resp.StatusCode != 404 {
		t.Errorf("404: %d", resp.StatusCode)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/stock/low", "", `{}`)
	if resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}

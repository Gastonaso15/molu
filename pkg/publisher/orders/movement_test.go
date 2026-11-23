package orders

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
)

func f64(v float64) *float64 { return &v }

func seedMovements(fs *fakeSubstrate) {
	fs.put("unit", "1", map[string]any{"name": "Metro", "symbol": "m"})
	fs.put(warehouseEntity, "1", map[string]any{"name": "Deposito Central", "enabled": true, "_version": 1})
	fs.put(warehouseEntity, "2", map[string]any{"name": "Deposito Norte", "enabled": true, "_version": 1})
	fs.put(warehouseEntity, "3", map[string]any{"name": "Deposito Cerrado", "enabled": false, "_version": 1})

	fs.put(consumableEntity, "10", map[string]any{
		"name": "Cable", "unit_id": 1, "stock_min": 5.0, "enabled": true,
		"stock_by_warehouse": map[string]any{"1": 1000}, // 10.00
		"_version":           2,
	})
	fs.put(consumableEntity, "11", map[string]any{
		"name": "Cinta inactiva", "unit_id": 1, "stock_min": 1.0, "enabled": false,
		"stock_by_warehouse": map[string]any{},
		"_version":           1,
	})
	// Consumable without stock_by_warehouse yet (legacy balance on warehouse 1).
	fs.put(consumableEntity, "12", map[string]any{
		"name": "Tornillo legacy", "unit_id": 1, "stock_min": 1.0, "enabled": true,
		"_version": 1,
	})
	fs.entities[warehouseEntity]["1"]["consumables"] = []any{
		map[string]any{"consumable_id": 12, "stock": 3.5},
	}
}

func TestRegisterMovement_EntryCreatesStockAndReceipt(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMovements(fs)
	svc := NewService(fs)

	reqID := "0123456789abcdef0123456789abcdef"
	res, err := svc.RegisterMovement(ctx, MovementInput{
		Type:              MovementEntry,
		ConsumableID:      "10",
		UserID:            "9",
		TargetWarehouseID: "1",
		Quantity:          f64(2.5),
		Reason:            "Reposicion",
		Reference:         "REM-001",
		RequestID:         reqID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyProcessed || res.RequestID != reqID || res.Type != MovementEntry ||
		res.ConsumableID != "10" || res.TargetWarehouseID != "1" || res.OriginWarehouseID != "" ||
		res.Quantity != 2.5 || res.Delta != 2.5 || res.StockBefore != 10 || res.StockAfter != 12.5 {
		t.Fatalf("result = %+v", res)
	}
	if fs.commits != 1 {
		t.Fatalf("commits = %d, want 1", fs.commits)
	}

	// Verify consumable stock_by_warehouse updated in hundredths and version bumped.
	cons := fs.entities[consumableEntity]["10"]
	if intOf(cons["_version"]) != 3 {
		t.Fatalf("consumable _version = %v, want 3", cons["_version"])
	}
	var stock map[string]any
	if err := roundTrip(cons["stock_by_warehouse"], &stock); err != nil || intOf(stock["1"]) != 1250 {
		t.Fatalf("stock_by_warehouse = %v err=%v", cons["stock_by_warehouse"], err)
	}

	// Verify movement record and receipt record were appended atomically.
	if len(fs.entities[movementEntity]) != 1 {
		t.Fatalf("movements = %d, want 1", len(fs.entities[movementEntity]))
	}
	for _, m := range fs.entities[movementEntity] {
		if m["request_id"] != reqID || m["type"] != "entry" || m["reason"] != "Reposicion" || m["reference"] != "REM-001" ||
			intOf(m["consumable_id"]) != 10 || intOf(m["target_warehouse_id"]) != 1 || intOf(m["user_id"]) != 9 ||
			m["quantity"] != 2.5 || m["stock_before"] != 10.0 || m["stock_after"] != 12.5 || m["delta"] != 2.5 {
			t.Fatalf("movement doc = %v", m)
		}
	}
	if len(fs.entities[movementRequestEntity]) != 1 {
		t.Fatalf("receipts = %d, want 1", len(fs.entities[movementRequestEntity]))
	}

	// GetConsumable immediately reflects the new stock.
	detail, err := svc.GetConsumable(ctx, GetConsumableInput{ConsumableID: "10"})
	if err != nil || detail.TotalStock != 12.5 {
		t.Fatalf("GetConsumable = %+v err=%v", detail, err)
	}
}

func TestRegisterMovement_ExitAndInsufficientStock(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMovements(fs)
	svc := NewService(fs)

	res, err := svc.RegisterMovement(ctx, MovementInput{
		Type:              MovementExit,
		ConsumableID:      "10",
		UserID:            "9",
		OriginWarehouseID: "1",
		Quantity:          f64(2.25),
		Reason:            "Consumo taller",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RequestID) != 32 || res.StockBefore != 10 || res.StockAfter != 7.75 || res.Delta != -2.25 {
		t.Fatalf("exit result = %+v", res)
	}

	// Requesting more than available (7.75) fails with insufficient_stock.
	_, err = svc.RegisterMovement(ctx, MovementInput{
		Type:              MovementExit,
		ConsumableID:      "10",
		UserID:            "9",
		OriginWarehouseID: "1",
		Quantity:          f64(8),
		Reason:            "Exceso",
	})
	wantRule(t, err, "insufficient_stock")
	if fs.commits != 1 {
		t.Fatalf("failed exit must not commit, commits=%d", fs.commits)
	}
}

func TestRegisterMovement_TransferAtomic(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMovements(fs)
	svc := NewService(fs)

	res, err := svc.RegisterMovement(ctx, MovementInput{
		Type:              MovementTransfer,
		ConsumableID:      "10",
		UserID:            "9",
		OriginWarehouseID: "1",
		TargetWarehouseID: "2",
		Quantity:          f64(4.5),
		Reason:            "Traslado a norte",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.StockBefore != 10 || res.StockAfter != 5.5 || res.Delta != -4.5 ||
		res.TargetStockBefore == nil || *res.TargetStockBefore != 0 ||
		res.TargetStockAfter == nil || *res.TargetStockAfter != 4.5 {
		t.Fatalf("transfer result = %+v", res)
	}

	var stock map[string]any
	_ = roundTrip(fs.entities[consumableEntity]["10"]["stock_by_warehouse"], &stock)
	if intOf(stock["1"]) != 550 || intOf(stock["2"]) != 450 {
		t.Fatalf("stock_by_warehouse = %v", stock)
	}

	// Transfer with insufficient stock fails cleanly.
	_, err = svc.RegisterMovement(ctx, MovementInput{
		Type:              MovementTransfer,
		ConsumableID:      "10",
		UserID:            "9",
		OriginWarehouseID: "1",
		TargetWarehouseID: "2",
		Quantity:          f64(6),
		Reason:            "Exceso",
	})
	wantRule(t, err, "insufficient_stock")
}

func TestRegisterMovement_AdjustIncludingZero(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMovements(fs)
	svc := NewService(fs)

	res, err := svc.RegisterMovement(ctx, MovementInput{
		Type:              MovementAdjust,
		ConsumableID:      "10",
		UserID:            "9",
		TargetWarehouseID: "1",
		Quantity:          f64(7.25),
		Reason:            "Conteo fisico",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.StockBefore != 10 || res.StockAfter != 7.25 || res.Delta != -2.75 {
		t.Fatalf("adjust result = %+v", res)
	}

	// Adjust to zero is valid.
	resZero, err := svc.RegisterMovement(ctx, MovementInput{
		Type:              MovementAdjust,
		ConsumableID:      "10",
		UserID:            "9",
		TargetWarehouseID: "1",
		Quantity:          f64(0),
		Reason:            "Baja total",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resZero.StockBefore != 7.25 || resZero.StockAfter != 0 || resZero.Delta != -7.25 {
		t.Fatalf("adjust zero result = %+v", resZero)
	}
}

func TestRegisterMovement_LegacyStockMigration(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMovements(fs)
	svc := NewService(fs)

	// Consumable 12 has no stock_by_warehouse yet, only legacy 3.50 in warehouse 1.
	res, err := svc.RegisterMovement(ctx, MovementInput{
		Type:              MovementEntry,
		ConsumableID:      "12",
		UserID:            "9",
		TargetWarehouseID: "1",
		Quantity:          f64(1.5),
		Reason:            "Suma a saldo previo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.StockBefore != 3.5 || res.StockAfter != 5 {
		t.Fatalf("legacy migration result = %+v", res)
	}
	var stock map[string]any
	_ = roundTrip(fs.entities[consumableEntity]["12"]["stock_by_warehouse"], &stock)
	if intOf(stock["1"]) != 500 {
		t.Fatalf("migrated stock_by_warehouse = %v", stock)
	}
}

func TestRegisterMovement_IdempotentReplayAndRequestReused(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMovements(fs)
	svc := NewService(fs)

	reqID := "abcdef0123456789abcdef0123456789"
	in := MovementInput{
		Type:              MovementEntry,
		ConsumableID:      "10",
		UserID:            "9",
		TargetWarehouseID: "1",
		Quantity:          f64(3),
		Reason:            "Compra",
		Reference:         "OC-99",
		RequestID:         reqID,
	}
	first, err := svc.RegisterMovement(ctx, in)
	if err != nil || first.AlreadyProcessed || first.StockAfter != 13 {
		t.Fatalf("first = %+v err=%v", first, err)
	}

	// Exact replay: returns AlreadyProcessed=true without committing again.
	replay, err := svc.RegisterMovement(ctx, in)
	if err != nil || !replay.AlreadyProcessed || replay.RequestID != reqID || replay.StockAfter != 13 {
		t.Fatalf("replay = %+v err=%v", replay, err)
	}
	if fs.commits != 1 {
		t.Fatalf("replay must not commit, commits=%d", fs.commits)
	}

	// Same request_id with different content -> request_reused.
	different := in
	different.Quantity = f64(4)
	_, err = svc.RegisterMovement(ctx, different)
	wantRule(t, err, "request_reused")
	if fs.commits != 1 {
		t.Fatalf("conflict must not commit, commits=%d", fs.commits)
	}
}

func TestRegisterMovement_CommitRecoveryVersionConflictAndSubstrateDown(t *testing.T) {
	ctx := context.Background()

	// 1. Commit succeeds on server but transport fails -> receipt lookup recovers cleanly.
	fs1 := newFake()
	seedMovements(fs1)
	fs1.commitSavesThenErr = errors.New("connection reset by peer")
	res, err := NewService(fs1).RegisterMovement(ctx, MovementInput{
		Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "1",
		Quantity: f64(2), Reason: "Alta", RequestID: "11111111111111111111111111111111",
	})
	if err != nil || res.StockAfter != 12 {
		t.Fatalf("recovery failed: res=%+v err=%v", res, err)
	}

	// 2. Commit rejected with 409 version conflict -> stale_state.
	fs2 := newFake()
	seedMovements(fs2)
	fs2.commitErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "version conflict"}
	_, err = NewService(fs2).RegisterMovement(ctx, MovementInput{
		Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "1",
		Quantity: f64(2), Reason: "Alta",
	})
	wantRule(t, err, "stale_state")

	// 3. Commit fails with 503 and receipt absent -> UnavailableError.
	fs3 := newFake()
	seedMovements(fs3)
	fs3.commitErr = &XoluError{Status: 503, Message: "down"}
	_, err = NewService(fs3).RegisterMovement(ctx, MovementInput{
		Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "1",
		Quantity: f64(2), Reason: "Alta",
	})
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %T %v, want UnavailableError", err, err)
	}
}

func TestRegisterMovement_DisabledEntities(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMovements(fs)
	svc := NewService(fs)

	// Disabled consumable
	_, err := svc.RegisterMovement(ctx, MovementInput{
		Type: MovementEntry, ConsumableID: "11", UserID: "9", TargetWarehouseID: "1",
		Quantity: f64(1), Reason: "Prueba",
	})
	wantRule(t, err, "consumable_disabled")

	// Disabled target warehouse
	_, err = svc.RegisterMovement(ctx, MovementInput{
		Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "3",
		Quantity: f64(1), Reason: "Prueba",
	})
	wantRule(t, err, "warehouse_disabled")

	// Disabled origin warehouse on transfer
	_, err = svc.RegisterMovement(ctx, MovementInput{
		Type: MovementTransfer, ConsumableID: "10", UserID: "9", OriginWarehouseID: "3", TargetWarehouseID: "1",
		Quantity: f64(1), Reason: "Prueba",
	})
	wantRule(t, err, "warehouse_disabled")

	// Disabled target warehouse on transfer
	_, err = svc.RegisterMovement(ctx, MovementInput{
		Type: MovementTransfer, ConsumableID: "10", UserID: "9", OriginWarehouseID: "1", TargetWarehouseID: "3",
		Quantity: f64(1), Reason: "Prueba",
	})
	wantRule(t, err, "warehouse_disabled")
}

func TestRegisterMovement_Validation(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMovements(fs)
	svc := NewService(fs)

	cases := []struct {
		name  string
		in    MovementInput
		field string
	}{
		{"bad type", MovementInput{Type: "other", ConsumableID: "10", UserID: "9", TargetWarehouseID: "1", Quantity: f64(1), Reason: "r"}, "type"},
		{"missing consumable", MovementInput{Type: MovementEntry, UserID: "9", TargetWarehouseID: "1", Quantity: f64(1), Reason: "r"}, "consumable_id"},
		{"missing user", MovementInput{Type: MovementEntry, ConsumableID: "10", TargetWarehouseID: "1", Quantity: f64(1), Reason: "r"}, "user_id"},
		{"missing reason", MovementInput{Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "1", Quantity: f64(1)}, "reason"},
		{"missing quantity", MovementInput{Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "1", Reason: "r"}, "quantity"},
		{"zero entry quantity", MovementInput{Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "1", Quantity: f64(0), Reason: "r"}, "quantity"},
		{"negative quantity", MovementInput{Type: MovementAdjust, ConsumableID: "10", UserID: "9", TargetWarehouseID: "1", Quantity: f64(-1), Reason: "r"}, "quantity"},
		{"three decimals", MovementInput{Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "1", Quantity: f64(1.234), Reason: "r"}, "quantity"},
		{"entry with origin", MovementInput{Type: MovementEntry, ConsumableID: "10", UserID: "9", OriginWarehouseID: "1", TargetWarehouseID: "2", Quantity: f64(1), Reason: "r"}, "origin_warehouse_id"},
		{"entry without target", MovementInput{Type: MovementEntry, ConsumableID: "10", UserID: "9", Quantity: f64(1), Reason: "r"}, "target_warehouse_id"},
		{"exit with target", MovementInput{Type: MovementExit, ConsumableID: "10", UserID: "9", OriginWarehouseID: "1", TargetWarehouseID: "2", Quantity: f64(1), Reason: "r"}, "target_warehouse_id"},
		{"exit without origin", MovementInput{Type: MovementExit, ConsumableID: "10", UserID: "9", Quantity: f64(1), Reason: "r"}, "origin_warehouse_id"},
		{"transfer same warehouse", MovementInput{Type: MovementTransfer, ConsumableID: "10", UserID: "9", OriginWarehouseID: "1", TargetWarehouseID: "1", Quantity: f64(1), Reason: "r"}, "target_warehouse_id"},
		{"adjust with origin", MovementInput{Type: MovementAdjust, ConsumableID: "10", UserID: "9", OriginWarehouseID: "1", TargetWarehouseID: "2", Quantity: f64(1), Reason: "r"}, "origin_warehouse_id"},
		{"bad request_id", MovementInput{Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "1", Quantity: f64(1), Reason: "r", RequestID: "not-hex"}, "request_id"},
		{"unknown consumable", MovementInput{Type: MovementEntry, ConsumableID: "999", UserID: "9", TargetWarehouseID: "1", Quantity: f64(1), Reason: "r"}, "consumable_id"},
		{"unknown warehouse", MovementInput{Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "999", Quantity: f64(1), Reason: "r"}, "target_warehouse_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.RegisterMovement(ctx, tc.in)
			wantInvalid(t, err, tc.field)
		})
	}
}

func TestRegisterMovement_ConcurrentEntriesSerialized(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedMovements(fs)
	svc := NewService(fs)

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			reqID := fmt.Sprintf("%013x%019x", i+1, i+1)
			if _, err := svc.RegisterMovement(ctx, MovementInput{
				Type: MovementEntry, ConsumableID: "10", UserID: "9", TargetWarehouseID: "1",
				Quantity: f64(1.25), Reason: "Lote " + strconv.Itoa(i), RequestID: reqID,
			}); err != nil {
				t.Errorf("worker %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	var stock map[string]any
	_ = roundTrip(fs.entities[consumableEntity]["10"]["stock_by_warehouse"], &stock)
	if got := intOf(stock["1"]); got != 1000+n*125 {
		t.Fatalf("final stock = %d, want %d", got, 1000+n*125)
	}
}

func TestOrderServer_RegisterMovement(t *testing.T) {
	fs := newFake()
	seedMovements(fs)
	srv := newTestServer(fs)
	defer srv.Close()

	body := `{"type":"entry","consumable_id":10,"user_id":9,"target_warehouse_id":1,"quantity":5,"reason":"Ingreso","request_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	resp, out := post(t, srv.URL+"/api/v1/orders/movements/register", testToken, body)
	if resp.StatusCode != 200 || out["stock_after"] != 15.0 {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}

	// Idempotent replay -> 200 with already_processed=true
	resp, out = post(t, srv.URL+"/api/v1/orders/movements/register", testToken, body)
	if resp.StatusCode != 200 || out["already_processed"] != true {
		t.Fatalf("replay: %d %v", resp.StatusCode, out)
	}

	// Reused request_id with different quantity -> 409 request_reused
	conflict := `{"type":"entry","consumable_id":10,"user_id":9,"target_warehouse_id":1,"quantity":6,"reason":"Ingreso","request_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	resp, out = post(t, srv.URL+"/api/v1/orders/movements/register", testToken, conflict)
	if resp.StatusCode != 409 || out["code"] != "XOLU-INVALID_TRANSITION" || out["details"].(map[string]any)["rule"] != "request_reused" {
		t.Fatalf("conflict: %d %v", resp.StatusCode, out)
	}

	// Insufficient stock -> 409 insufficient_stock
	exit := `{"type":"exit","consumable_id":10,"user_id":9,"origin_warehouse_id":1,"quantity":99,"reason":"Mucho"}`
	resp, out = post(t, srv.URL+"/api/v1/orders/movements/register", testToken, exit)
	if resp.StatusCode != 409 || out["code"] != "XOLU-INVALID_TRANSITION" || out["details"].(map[string]any)["rule"] != "insufficient_stock" {
		t.Fatalf("insufficient: %d %v", resp.StatusCode, out)
	}

	// Bad request -> 400
	resp, _ = post(t, srv.URL+"/api/v1/orders/movements/register", testToken, `{"type":"entry"}`)
	if resp.StatusCode != 400 {
		t.Errorf("bad request: %d", resp.StatusCode)
	}

	// Unauthorized -> 401
	resp, _ = post(t, srv.URL+"/api/v1/orders/movements/register", "", `{}`)
	if resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}

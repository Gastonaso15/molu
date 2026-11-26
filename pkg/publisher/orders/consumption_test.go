package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// consumeWorld opens an order from template 22 (consumables on, extras
// allowed, no blocks, states Pendiente -> Finalizada) planning consumable 10
// (template) and 12 (extra), assigned to crew 7 (led by user 5). Central
// warehouse 100 holds 20 of consumable 10 and 10 of 12; warehouse 101 is empty.
func consumeWorld(t *testing.T) (*fakeSubstrate, *Service, string) {
	t.Helper()
	fs := newExecFake()
	seedMobileWarehouseWorld(fs)
	fs.put(usersEntity, "5", map[string]any{"name": "Luis", "active": true})
	fs.put(crewEntity, "7", map[string]any{"name": "Cuadrilla A", "responsible_user_id": 5, "members": []any{5, 6}})
	fs.put(warehouseEntity, "101", map[string]any{"name": "Deposito 2", "enabled": true, "_version": 1})
	fs.put(consumableEntity, "13", map[string]any{
		"name": "Otro", "consumable_unit": 1, "stock_min": 0.0, "enabled": true,
		"stock_by_warehouse": map[string]any{"100": 500}, "_version": 1,
	})
	fs.put(TemplateEntityType, "22", map[string]any{
		"name": "Con extras", "description": "d", "version": 1, "latest": true,
		"asset_type_id": 5, "min_assets": 1, "max_assets": 1,
		"workflow_states":             []any{map[string]any{"name": "Pendiente"}, map[string]any{"name": "Finalizada"}},
		"header_items":                []any{},
		"blocks":                      []any{},
		"consumables_allowed":         true,
		"add_any_consumables_allowed": true,
		"consumables":                 []any{map[string]any{"consumable_id": 10, "quantity": 2}},
	})
	svc := NewService(fs)
	o, err := svc.CreateOrder(context.Background(), CreateInput{
		TemplateID: "22", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7",
		AssetIDs: []ID{"11"}, CreatedByUserID: "9", ExtraConsumableIDs: []ID{"12"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[EntityType][o.ID]["_version"] = int64(1)
	return fs, svc, o.ID
}

func stockOf(fs *fakeSubstrate, consumable, warehouse string) int64 {
	m, _ := fs.entities[consumableEntity][consumable][movementStockField].(map[string]int64)
	if m != nil {
		return m[warehouse]
	}
	raw, _ := fs.entities[consumableEntity][consumable][movementStockField].(map[string]any)
	return intOf(raw[warehouse])
}

func record(svc *Service, order, cons, intent, user, origin string, qty *float64) (RecordConsumableResult, error) {
	return svc.RecordOrderConsumable(context.Background(), RecordConsumableInput{
		OrderID: order, ConsumableID: ID(cons), Intent: intent, UserID: ID(user), OriginWarehouseID: ID(origin), Quantity: qty,
	})
}

func openForConsumables(t *testing.T, svc *Service, id string) {
	t.Helper()
	if _, err := svc.ConfirmExecutionHeader(context.Background(), ConfirmHeaderInput{OrderID: id, UserID: "5"}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateOrder_ExtraConsumables(t *testing.T) {
	fs, svc, id := consumeWorld(t)
	if got := idList(fs.entities[EntityType][id]["extra_consumable_ids"]); len(got) != 1 || got[0] != "12" {
		t.Fatalf("extra_consumable_ids = %v", fs.entities[EntityType][id]["extra_consumable_ids"])
	}
	base := CreateInput{TemplateID: "22", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7", AssetIDs: []ID{"11"}}
	for name, extras := range map[string][]ID{"unknown": {"404"}, "repeated": {"12", "12"}, "bad id": {"x"}} {
		in := base
		in.ExtraConsumableIDs = extras
		var ve *ValidationError
		if _, err := svc.CreateOrder(context.Background(), in); !errors.As(err, &ve) || !strings.HasPrefix(ve.Fields[0], "extra_consumable_ids") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A template that does not allow extras refuses them.
	in := base
	in.TemplateID, in.ExtraConsumableIDs = "20", []ID{"12"}
	var ve *ValidationError
	if _, err := svc.CreateOrder(context.Background(), in); !errors.As(err, &ve) || ve.Fields[0] != "extra_consumable_ids" {
		t.Errorf("template without extras: err = %v", err)
	}
}

func TestRecordOrderConsumable_GatesAndValidation(t *testing.T) {
	_, svc, id := consumeWorld(t)
	q := floatPtr(5)
	if _, err := record(svc, id, "10", ConsumeConfirm, "5", "100", q); ruleOf(err) != "header_not_confirmed" {
		t.Fatalf("before the header: %v", err)
	}
	openForConsumables(t, svc, id)
	if _, err := record(svc, id, "10", ConsumeConfirm, "9", "100", q); ruleOf(err) != "not_responsible" {
		t.Fatalf("a stranger: %v", err)
	}
	var ve *ValidationError
	for name, in := range map[string]RecordConsumableInput{
		"intent":        {OrderID: id, ConsumableID: "10", Intent: "take", UserID: "5", OriginWarehouseID: "100", Quantity: q},
		"user":          {OrderID: id, ConsumableID: "10", Intent: ConsumeConfirm, OriginWarehouseID: "100", Quantity: q},
		"no quantity":   {OrderID: id, ConsumableID: "10", Intent: ConsumeConfirm, UserID: "5", OriginWarehouseID: "100"},
		"zero quantity": {OrderID: id, ConsumableID: "10", Intent: ConsumeConfirm, UserID: "5", OriginWarehouseID: "100", Quantity: floatPtr(0)},
		"3 decimals":    {OrderID: id, ConsumableID: "10", Intent: ConsumeConfirm, UserID: "5", OriginWarehouseID: "100", Quantity: floatPtr(1.234)},
		"no origin":     {OrderID: id, ConsumableID: "10", Intent: ConsumeConfirm, UserID: "5", Quantity: q},
		"origin=mobile": {OrderID: id, ConsumableID: "10", Intent: ConsumeConfirm, UserID: "5", OriginWarehouseID: ID(id), Quantity: q},
		"not planned":   {OrderID: id, ConsumableID: "13", Intent: ConsumeConfirm, UserID: "5", OriginWarehouseID: "100", Quantity: q},
		"request_id":    {OrderID: id, ConsumableID: "10", Intent: ConsumeConfirm, UserID: "5", OriginWarehouseID: "100", Quantity: q, RequestID: strings.Repeat("a", 32)},
	} {
		if _, err := svc.RecordOrderConsumable(context.Background(), in); !errors.As(err, &ve) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := record(svc, id, "10", ConsumeConfirm, "5", "100", floatPtr(500)); ruleOf(err) != "insufficient_stock" {
		t.Fatalf("not enough in the origin: %v", err)
	}
	if _, err := record(svc, id, "10", ConsumeMore, "5", "100", q); ruleOf(err) != "not_recorded" {
		t.Fatalf("more before any load: %v", err)
	}
}

func TestRecordOrderConsumable_ConfirmMoreAndIdempotency(t *testing.T) {
	fs, svc, id := consumeWorld(t)
	openForConsumables(t, svc, id)

	res, err := record(svc, id, "10", ConsumeConfirm, "5", "100", floatPtr(5))
	if err != nil || res.AlreadyRecorded || res.Movement == nil || res.Movement.Quantity != 5 || res.Movement.StockAfter != 15 || *res.Movement.TargetStockAfter != 5 {
		t.Fatalf("confirm: %+v err=%v", res, err)
	}
	if c := res.Consumable; c.Status != "used" || c.Loaded != 5 || c.Name != "Filtro" || c.Unit != "u" || c.Suggested != 2 || c.Settled {
		t.Fatalf("line = %+v", c)
	}
	if stockOf(fs, "10", "100") != 1500 || stockOf(fs, "10", id) != 500 {
		t.Fatalf("stock: central=%d mobile=%d", stockOf(fs, "10", "100"), stockOf(fs, "10", id))
	}
	var rec map[string]any
	for _, r := range fs.entities[consumptionsEntity] {
		rec = r
	}
	if rec == nil || rec["status"] != "used" || intOf(rec["origin_warehouse_id"]) != 100 || idString(rec["target_warehouse_id"]) != id || intOf(rec["user_id"]) != 5 || len(fs.entities[consumptionsEntity]) != 1 {
		t.Fatalf("record = %v", rec)
	}
	// The movement is the work order's: reference and reason name it.
	for _, m := range fs.entities[movementEntity] {
		if m["reference"] != "WO #"+id || m["type"] != "transfer" {
			t.Errorf("movement = %v", m)
		}
	}

	// The same request again changes nothing; other content is a conflict.
	again, err := record(svc, id, "10", ConsumeConfirm, "5", "100", floatPtr(5))
	if err != nil || !again.AlreadyRecorded || stockOf(fs, "10", "100") != 1500 || len(fs.entities[consumptionsEntity]) != 1 || len(fs.entities[movementEntity]) != 1 {
		t.Fatalf("repeat: %+v err=%v", again, err)
	}
	if _, err := record(svc, id, "10", ConsumeConfirm, "5", "100", floatPtr(6)); ruleOf(err) != "already_recorded" {
		t.Fatalf("different content: %v", err)
	}

	// more: another load with its own request id; replaying it changes nothing.
	more, err := record(svc, id, "10", ConsumeMore, "5", "100", floatPtr(2))
	if err != nil || more.RequestID == "" || more.Consumable.Loaded != 7 || stockOf(fs, "10", id) != 700 {
		t.Fatalf("more: %+v err=%v", more, err)
	}
	replay, err := svc.RecordOrderConsumable(context.Background(), RecordConsumableInput{
		OrderID: id, ConsumableID: "10", Intent: ConsumeMore, UserID: "5", OriginWarehouseID: "100", Quantity: floatPtr(2), RequestID: more.RequestID,
	})
	if err != nil || !replay.AlreadyRecorded || replay.Consumable.Loaded != 7 || stockOf(fs, "10", id) != 700 {
		t.Fatalf("replay of more: %+v err=%v", replay, err)
	}

	// GetOrder reports the lines, including the extra one nobody touched.
	d, err := svc.GetOrder(context.Background(), id)
	if err != nil || len(d.Consumables) != 2 || d.Consumables[0].ConsumableID != "10" || d.Consumables[0].Loaded != 7 || d.Consumables[1].ConsumableID != "12" || d.Consumables[1].Status != "" {
		t.Fatalf("GetOrder consumables = %+v err=%v", d.Consumables, err)
	}
}

func TestRecordOrderConsumable_AddSkipAndExtras(t *testing.T) {
	fs, svc, id := consumeWorld(t)
	openForConsumables(t, svc, id)

	// 12 was chosen as an extra at creation: it is planned, so add is refused and confirm works.
	if _, err := record(svc, id, "12", ConsumeAdd, "5", "100", floatPtr(1)); ruleOf(err) != "already_listed" {
		t.Fatalf("add a planned one: %v", err)
	}
	if _, err := record(svc, id, "12", ConsumeConfirm, "5", "100", floatPtr(1)); err != nil {
		t.Fatalf("confirm an extra: %v", err)
	}
	// 13 is new: add loads it.
	added, err := record(svc, id, "13", ConsumeAdd, "5", "100", floatPtr(1.5))
	if err != nil || added.Consumable.Loaded != 1.5 || stockOf(fs, "13", id) != 150 {
		t.Fatalf("add: %+v err=%v", added, err)
	}
	// skip needs no quantity or origin, touches the order and is idempotent.
	before := fs.entities[EntityType][id]["_version"]
	sk, err := svc.RecordOrderConsumable(context.Background(), RecordConsumableInput{OrderID: id, ConsumableID: "10", Intent: ConsumeSkip, UserID: "5"})
	if err != nil || sk.Consumable.Status != "skipped" || sk.Movement != nil || fs.entities[EntityType][id]["_version"] == before {
		t.Fatalf("skip: %+v err=%v", sk, err)
	}
	if again, err := svc.RecordOrderConsumable(context.Background(), RecordConsumableInput{OrderID: id, ConsumableID: "10", Intent: ConsumeSkip, UserID: "5"}); err != nil || !again.AlreadyRecorded {
		t.Fatalf("skip again: %+v err=%v", again, err)
	}
	if _, err := record(svc, id, "10", ConsumeConfirm, "5", "100", floatPtr(1)); ruleOf(err) != "already_recorded" {
		t.Fatalf("confirm after skip: %v", err)
	}
}

func TestRecordOrderConsumable_TemplateRules(t *testing.T) {
	fs := newExecFake()
	seedMobileWarehouseWorld(fs)
	fs.put(usersEntity, "5", map[string]any{"name": "Luis", "active": true})
	fs.put(crewEntity, "7", map[string]any{"name": "Cuadrilla A", "responsible_user_id": 5})
	fs.put(consumableEntity, "13", map[string]any{"name": "Otro", "consumable_unit": 1, "enabled": true, "stock_by_warehouse": map[string]any{"100": 500}, "_version": 1})
	svc := NewService(fs)
	// Template 20 plans consumable 10 and does not allow extras.
	o, err := svc.CreateOrder(context.Background(), CreateInput{TemplateID: "20", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7", AssetIDs: []ID{"11"}})
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[EntityType][o.ID]["_version"] = int64(1)
	openForConsumables(t, svc, o.ID)
	if _, err := record(svc, o.ID, "13", ConsumeAdd, "5", "100", floatPtr(1)); ruleOf(err) != "extras_not_allowed" {
		t.Fatalf("extras: %v", err)
	}
	// A template without consumables has no mobile warehouse to load.
	fs.put(TemplateEntityType, "30", map[string]any{
		"name": "Sin insumos", "version": 1, "latest": true, "min_assets": 0, "max_assets": 0,
		"workflow_states": []any{map[string]any{"name": "A"}, map[string]any{"name": "B"}}, "header_items": []any{}, "blocks": []any{}, "consumables_allowed": false,
	})
	p, err := svc.CreateOrder(context.Background(), CreateInput{TemplateID: "30", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7"})
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[EntityType][p.ID]["_version"] = int64(1)
	if _, err := record(svc, p.ID, "10", ConsumeConfirm, "5", "100", floatPtr(1)); ruleOf(err) != "consumables_disabled" {
		t.Fatalf("no consumables: %v", err)
	}
}

func TestSettleOrderConsumable(t *testing.T) {
	ctx := context.Background()
	fs, svc, id := consumeWorld(t)
	openForConsumables(t, svc, id)
	if _, err := record(svc, id, "10", ConsumeConfirm, "5", "100", floatPtr(5)); err != nil {
		t.Fatal(err)
	}
	settle := func(cons string, used *float64, ret string) (SettleConsumableResult, error) {
		return svc.SettleOrderConsumable(ctx, SettleConsumableInput{OrderID: id, ConsumableID: ID(cons), UserID: "5", Used: used, ReturnWarehouseID: ID(ret)})
	}
	if _, err := settle("10", floatPtr(3), "101"); ruleOf(err) != "not_finished" {
		t.Fatalf("an open order: %v", err)
	}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: id, Input: InputComplete, UserID: "5"}); err != nil {
		t.Fatal(err)
	}
	// Nothing was loaded for 12; a stranger cannot settle; arguments are checked.
	if _, err := settle("12", floatPtr(0), ""); ruleOf(err) != "nothing_loaded" {
		t.Fatalf("12: %v", err)
	}
	if _, err := svc.SettleOrderConsumable(ctx, SettleConsumableInput{OrderID: id, ConsumableID: "10", UserID: "9", Used: floatPtr(3), ReturnWarehouseID: "101"}); ruleOf(err) != "not_responsible" {
		t.Fatalf("a stranger: %v", err)
	}
	var ve *ValidationError
	for name, c := range map[string]struct {
		used *float64
		ret  string
	}{"no used": {nil, "101"}, "over loaded": {floatPtr(6), "101"}, "no return": {floatPtr(3), ""}, "return = mobile": {floatPtr(3), id}, "negative": {floatPtr(-1), "101"}} {
		if _, err := settle("10", c.used, c.ret); !errors.As(err, &ve) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(fs.entities[movementEntity]) != 1 {
		t.Fatalf("a refused settlement wrote movements: %d", len(fs.entities[movementEntity]))
	}

	// Used 3 of 5: 3 leave (exit), 2 go back (transfer), one Commit.
	commits := fs.commits
	res, err := settle("10", floatPtr(3), "101")
	if err != nil || res.AlreadySettled || !res.Consumable.Settled || res.Consumable.Used != 3 || res.Consumable.Returned != 2 || res.Consumable.ReturnWarehouseID != "101" {
		t.Fatalf("settle: %+v err=%v", res, err)
	}
	if fs.commits != commits+1 {
		t.Fatalf("commits = %d, want one more than %d", fs.commits, commits)
	}
	if stockOf(fs, "10", id) != 0 || stockOf(fs, "10", "101") != 200 || stockOf(fs, "10", "100") != 1500 {
		t.Fatalf("stock: mobile=%d 101=%d central=%d", stockOf(fs, "10", id), stockOf(fs, "10", "101"), stockOf(fs, "10", "100"))
	}
	types := map[string]int{}
	for _, m := range fs.entities[movementEntity] {
		types[m["type"].(string)]++
	}
	if types["exit"] != 1 || types["transfer"] != 2 {
		t.Fatalf("movements = %v (want the load plus the settlement's exit and return)", types)
	}
	// Repeating changes nothing; other numbers are a conflict.
	again, err := settle("10", floatPtr(3), "101")
	if err != nil || !again.AlreadySettled || len(fs.entities[movementEntity]) != 3 {
		t.Fatalf("repeat: %+v err=%v", again, err)
	}
	if _, err := settle("10", floatPtr(4), "101"); ruleOf(err) != "already_recorded" {
		t.Fatalf("different content: %v", err)
	}
	d, err := svc.GetOrder(ctx, id)
	if err != nil || !d.Consumables[0].Settled || d.Consumables[0].Used != 3 || d.Consumables[0].Returned != 2 {
		t.Fatalf("GetOrder = %+v err=%v", d.Consumables, err)
	}
	// Recording after the order closed is refused.
	if _, err := record(svc, id, "12", ConsumeConfirm, "5", "100", floatPtr(1)); ruleOf(err) != "final_state" {
		t.Fatalf("record after closing: %v", err)
	}
}

func TestSettleOrderConsumable_EverythingUsedNeedsNoReturnWarehouse(t *testing.T) {
	ctx := context.Background()
	fs, svc, id := consumeWorld(t)
	openForConsumables(t, svc, id)
	if _, err := record(svc, id, "10", ConsumeConfirm, "5", "100", floatPtr(4)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: id, Input: InputComplete, UserID: "5"}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.SettleOrderConsumable(ctx, SettleConsumableInput{OrderID: id, ConsumableID: "10", UserID: "6", Used: floatPtr(4)})
	if err != nil || res.Consumable.Used != 4 || res.Consumable.Returned != 0 || stockOf(fs, "10", id) != 0 || stockOf(fs, "10", "100") != 1600 {
		t.Fatalf("settle: %+v err=%v stock=%d/%d", res, err, stockOf(fs, "10", id), stockOf(fs, "10", "100"))
	}
	// Nothing used: everything goes back.
	_, err = svc.RecordOrderConsumable(ctx, RecordConsumableInput{OrderID: id, ConsumableID: "12", Intent: ConsumeConfirm, UserID: "5", OriginWarehouseID: "100", Quantity: floatPtr(1)})
	if ruleOf(err) != "final_state" {
		t.Fatalf("a closed order takes no more: %v", err)
	}
}

func TestSettleOrderConsumable_NothingUsedReturnsAll(t *testing.T) {
	ctx := context.Background()
	fs, svc, id := consumeWorld(t)
	openForConsumables(t, svc, id)
	if _, err := record(svc, id, "10", ConsumeConfirm, "5", "100", floatPtr(5)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: id, Input: InputComplete, UserID: "5"}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.SettleOrderConsumable(ctx, SettleConsumableInput{OrderID: id, ConsumableID: "10", UserID: "5", Used: floatPtr(0), ReturnWarehouseID: "100"})
	if err != nil || res.Consumable.Used != 0 || res.Consumable.Returned != 5 || stockOf(fs, "10", "100") != 2000 || stockOf(fs, "10", id) != 0 {
		t.Fatalf("settle: %+v err=%v", res, err)
	}
	exits := 0
	for _, m := range fs.entities[movementEntity] {
		if m["type"] == "exit" {
			exits++
		}
	}
	if exits != 0 {
		t.Fatalf("nothing was used, but %d exit(s) were written", exits)
	}
}

func TestSettleOrderConsumable_RefusedCommitSavesNothing(t *testing.T) {
	ctx := context.Background()
	fs, svc, id := consumeWorld(t)
	openForConsumables(t, svc, id)
	if _, err := record(svc, id, "10", ConsumeConfirm, "5", "100", floatPtr(5)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: id, Input: InputComplete, UserID: "5"}); err != nil {
		t.Fatal(err)
	}
	movements := len(fs.entities[movementEntity])
	fs.commitErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "version conflict"}
	if _, err := svc.SettleOrderConsumable(ctx, SettleConsumableInput{OrderID: id, ConsumableID: "10", UserID: "5", Used: floatPtr(2), ReturnWarehouseID: "101"}); ruleOf(err) != "stale_state" {
		t.Fatalf("conflict: %v", err)
	}
	fs.commitErr = nil
	if len(fs.entities[movementEntity]) != movements || stockOf(fs, "10", id) != 500 {
		t.Fatal("a refused settlement changed something")
	}
}

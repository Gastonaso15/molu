package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// execWorld opens an order from a three-block template (A for user 9, B and C
// for crew 7; B and C wait for A) under the execution model. Crew 7 is led by
// user 5 and has user 6 as a member. The order is assigned to crew 7.
func execWorld(t *testing.T) (*fakeSubstrate, *Service, string) {
	t.Helper()
	fs := newExecFake()
	seedWorld(fs)
	fs.put(usersEntity, "9", map[string]any{"name": "Ana", "active": true})
	fs.put(usersEntity, "5", map[string]any{"name": "Luis", "active": true})
	fs.put(crewEntity, "7", map[string]any{"name": "Cuadrilla A", "responsible_user_id": 5, "members": []any{5, 6}})
	blockTemplate(fs, "10", threeBlocks())
	blockWorld = fs
	svc := NewService(fs)
	o, err := svc.CreateOrder(context.Background(), blockInput())
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[EntityType][o.ID]["_version"] = int64(1)
	for _, b := range fs.entities[workOrderBlocksEntity] {
		b["_version"] = int64(1)
	}
	return fs, svc, o.ID
}

func confirm(t *testing.T, svc *Service, id string) {
	t.Helper()
	if _, err := svc.ConfirmExecutionHeader(context.Background(), ConfirmHeaderInput{OrderID: id, UserID: "5", HeaderValues: map[string]any{"observacion": "ok"}}); err != nil {
		t.Fatal(err)
	}
}

func runBlock(t *testing.T, svc *Service, id, block, user string, values map[string]any) {
	t.Helper()
	for _, intent := range []string{IntentStart, IntentComplete} {
		in := ExecuteBlockInput{OrderID: id, Block: block, Intent: intent, UserID: ID(user)}
		if intent == IntentComplete {
			in.Values = values
		}
		if _, err := svc.ExecuteBlock(context.Background(), in); err != nil {
			t.Fatalf("%s %s: %v", intent, block, err)
		}
	}
}

func TestCreateOrder_WithBlocks_RecordsResponsiblesAndItsOwnMachine(t *testing.T) {
	fs, _, id := execWorld(t)
	doc := fs.entities[EntityType][id]
	mid := idString(doc["machine_id"])
	if intOf(doc["machine_id"]) == 0 || fs.machineDef[mid] != "def:"+TemplateDefinitionName("10", 2) {
		t.Fatalf("machine_id=%v machineDef=%v", doc["machine_id"], fs.machineDef)
	}
	if fs.machines[mid].State != "Pendiente" {
		t.Fatalf("workflow machine state = %q", fs.machines[mid].State)
	}
	want := map[string]struct {
		typ string
		id  int64
	}{"A": {"user", 9}, "B": {"crew", 7}, "C": {"crew", 7}}
	for _, b := range fs.entities[workOrderBlocksEntity] {
		w := want[b["name"].(string)]
		if b["responsible_type"] != w.typ || intOf(b["responsible_id"]) != w.id {
			t.Errorf("block %v responsible = %v/%v, want %v/%d", b["name"], b["responsible_type"], b["responsible_id"], w.typ, w.id)
		}
	}
}

func TestCreateOrder_WithBlocks_ResponsiblesAreValidated(t *testing.T) {
	fs, svc, _ := execWorld(t)
	fs.put(usersEntity, "8", map[string]any{"name": "Baja", "active": false})
	for name, mutate := range map[string]func(*CreateInput){
		"missing":      func(in *CreateInput) { delete(in.BlockResponsibles, "B") },
		"none at all":  func(in *CreateInput) { in.BlockResponsibles = nil },
		"unknown crew": func(in *CreateInput) { in.BlockResponsibles["B"] = "404" },
		"inactive":     func(in *CreateInput) { in.BlockResponsibles["A"] = "8" },
		"not a block":  func(in *CreateInput) { in.BlockResponsibles["Z"] = "7" },
		"bad id":       func(in *CreateInput) { in.BlockResponsibles["A"] = "x" },
	} {
		in := blockInput()
		mutate(&in)
		before := len(fs.entities[EntityType])
		_, err := svc.CreateOrder(context.Background(), in)
		var ve *ValidationError
		if !errors.As(err, &ve) || len(ve.Fields) == 0 || !strings.HasPrefix(ve.Fields[0], "block_responsibles") {
			t.Errorf("%s: err = %v", name, err)
		}
		if len(fs.entities[EntityType]) != before {
			t.Errorf("%s: an order was written", name)
		}
	}
}

func TestConfirmExecutionHeader(t *testing.T) {
	ctx := context.Background()
	fs, svc, id := execWorld(t)

	// Only the order's responsible: the crew's leader or a member.
	_, err := svc.ConfirmExecutionHeader(ctx, ConfirmHeaderInput{OrderID: id, UserID: "9", HeaderValues: map[string]any{"observacion": "x"}})
	if ruleOf(err) != "not_responsible" {
		t.Fatalf("a stranger: err = %v", err)
	}
	// A required item must be answered.
	_, err = svc.ConfirmExecutionHeader(ctx, ConfirmHeaderInput{OrderID: id, UserID: "5"})
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(strings.Join(ve.Fields, ","), "observacion") {
		t.Fatalf("missing required: err = %v", err)
	}
	if _, err := svc.ConfirmExecutionHeader(ctx, ConfirmHeaderInput{OrderID: id}); err == nil {
		t.Fatal("user_id is required")
	}

	res, err := svc.ConfirmExecutionHeader(ctx, ConfirmHeaderInput{OrderID: id, UserID: "6", HeaderValues: map[string]any{"observacion": "Equipo aislado"}})
	if err != nil || res.ConfirmedAt == "" || res.Order.HeaderValues["observacion"] != "Equipo aislado" {
		t.Fatalf("member confirms: res=%+v err=%v", res, err)
	}
	doc := fs.entities[EntityType][id]
	if doc["execution_header_completed_at"] == "" || intOf(doc["execution_header_completed_by_user_id"]) != 6 {
		t.Fatalf("stored: %v", doc)
	}
	// Frozen: a second confirmation is refused.
	_, err = svc.ConfirmExecutionHeader(ctx, ConfirmHeaderInput{OrderID: id, UserID: "5", HeaderValues: map[string]any{"observacion": "otra"}})
	if ruleOf(err) != "header_already_confirmed" {
		t.Fatalf("twice: err = %v", err)
	}
	if fs.entities[EntityType][id]["header_values"].(map[string]any)["observacion"] != "Equipo aislado" {
		t.Fatal("the frozen header changed")
	}
}

func TestConfirmExecutionHeader_BoardOrderMustBeClaimedFirst(t *testing.T) {
	fs, svc, _ := execWorld(t)
	in := blockInput()
	in.AssignmentType, in.ResponsibleID = AssignmentBoard, ""
	o, err := svc.CreateOrder(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[EntityType][o.ID]["_version"] = int64(1)
	_, err = svc.ConfirmExecutionHeader(context.Background(), ConfirmHeaderInput{OrderID: o.ID, UserID: "5", HeaderValues: map[string]any{"observacion": "x"}})
	if ruleOf(err) != "awaiting_claim" {
		t.Fatalf("err = %v", err)
	}
}

func TestExecuteBlock_Gates(t *testing.T) {
	ctx := context.Background()
	_, svc, id := execWorld(t)
	do := func(block, intent, user string) error {
		_, err := svc.ExecuteBlock(ctx, ExecuteBlockInput{OrderID: id, Block: block, Intent: intent, UserID: ID(user)})
		return err
	}
	if rule := ruleOf(do("A", "start", "9")); rule != "header_not_confirmed" {
		t.Fatalf("before the header: %q", rule)
	}
	confirm(t, svc, id)
	if rule := ruleOf(do("B", "start", "5")); rule != "predecessor_pending" {
		t.Fatalf("predecessor: %q", rule)
	}
	if rule := ruleOf(do("A", "start", "6")); rule != "" {
		// user 6 is a crew member: the order's responsible may run any block.
		t.Fatalf("an order's responsible runs block A: %q", rule)
	}
	if rule := ruleOf(do("A", "start", "9")); rule != "invalid_block_state" {
		t.Fatalf("start twice: %q", rule)
	}
	if rule := ruleOf(do("A", "save", "3")); rule != "not_responsible" {
		t.Fatalf("a stranger: %q", rule)
	}
	var ve *ValidationError
	for name, in := range map[string]ExecuteBlockInput{
		"intent":     {OrderID: id, Block: "A", Intent: "finish", UserID: "9"},
		"block":      {OrderID: id, Block: "Z", Intent: "save", UserID: "9"},
		"user":       {OrderID: id, Block: "A", Intent: "save"},
		"bad answer": {OrderID: id, Block: "A", Intent: "save", UserID: "9", Values: map[string]any{"a1": "maybe"}},
		"bad line":   {OrderID: id, Block: "A", Intent: "save", UserID: "9", Values: map[string]any{"nope": true}},
	} {
		if _, err := svc.ExecuteBlock(ctx, in); !errors.As(err, &ve) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Completing a block that is not in progress is refused.
	if rule := ruleOf(do("C", "complete", "5")); rule != "predecessor_pending" {
		t.Fatalf("C: %q", rule)
	}
}

func TestExecuteBlock_LifecycleMachineAndAnswers(t *testing.T) {
	ctx := context.Background()
	fs, svc, id := execWorld(t)
	confirm(t, svc, id)

	start, err := svc.ExecuteBlock(ctx, ExecuteBlockInput{OrderID: id, Block: " a ", Intent: IntentStart, UserID: "9"})
	if err != nil || start.Block.State != blockInProgress || start.Previous != blockPending || start.Block.StartedAt == "" {
		t.Fatalf("start: %+v err=%v", start, err)
	}
	var rec map[string]any
	for _, b := range fs.entities[workOrderBlocksEntity] {
		if b["name"] == "A" {
			rec = b
		}
	}
	if rec["state"] != blockInProgress || intOf(rec["started_by_user_id"]) != 9 || fs.machines[idString(rec["machine_id"])].State != blockInProgress {
		t.Fatalf("record/machine after start: %v", rec)
	}

	// save keeps it in progress and keeps what is already saved.
	if _, err := svc.ExecuteBlock(ctx, ExecuteBlockInput{OrderID: id, Block: "A", Intent: IntentSave, UserID: "9", Values: map[string]any{"a1": true}}); err != nil {
		t.Fatal(err)
	}
	if rec["state"] != blockInProgress || rec["values"].(map[string]any)[blockLineKey(0, 0)] != true {
		t.Fatalf("after save: %v", rec)
	}
	done, err := svc.ExecuteBlock(ctx, ExecuteBlockInput{OrderID: id, Block: "A", Intent: IntentComplete, UserID: "9"})
	if err != nil || done.Block.State != blockCompleted || done.Block.CompletedAt == "" || done.Block.Values[blockLineKey(0, 0)] != true {
		t.Fatalf("complete: %+v err=%v", done, err)
	}
	if fs.machines[idString(rec["machine_id"])].State != blockCompleted || !fs.machines[idString(rec["machine_id"])].Terminal {
		t.Fatal("the block's machine should be completed")
	}
	if _, err := svc.ExecuteBlock(ctx, ExecuteBlockInput{OrderID: id, Block: "A", Intent: IntentSave, UserID: "9"}); ruleOf(err) != "invalid_block_state" {
		t.Fatalf("save a completed block: %v", err)
	}
	// B is now open, for its own crew's responsible.
	if _, err := svc.ExecuteBlock(ctx, ExecuteBlockInput{OrderID: id, Block: "B", Intent: IntentStart, UserID: "5"}); err != nil {
		t.Fatalf("B after A: %v", err)
	}
}

func TestExecuteBlock_RefusedTransitionSavesNothing(t *testing.T) {
	ctx := context.Background()
	fs, svc, id := execWorld(t)
	confirm(t, svc, id)
	var rec map[string]any
	for _, b := range fs.entities[workOrderBlocksEntity] {
		if b["name"] == "A" {
			rec = b
		}
	}
	// The record says pending but its machine is somewhere else: xolu refuses
	// "start" and the record must stay as it was.
	fs.machines[idString(rec["machine_id"])].State = blockCompleted
	fs.machines[idString(rec["machine_id"])].Terminal = true
	if _, err := svc.ExecuteBlock(ctx, ExecuteBlockInput{OrderID: id, Block: "A", Intent: IntentStart, UserID: "9"}); err == nil {
		t.Fatal("want the machine's refusal")
	}
	if rec["state"] != blockPending || rec["started_at"] != "" {
		t.Fatalf("a refused transition changed the record: %v", rec)
	}
}

func TestAdvanceOrder_ExecutionModel(t *testing.T) {
	ctx := context.Background()
	fs, svc, id := execWorld(t)
	adv := func(user string) (AdvanceResult, error) {
		return svc.AdvanceOrder(ctx, AdvanceInput{OrderID: id, Input: InputComplete, UserID: ID(user)})
	}
	if _, err := adv(""); err == nil {
		t.Fatal("user_id is required")
	}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: id, Input: InputComplete, UserID: "5", HeaderValues: map[string]any{"observacion": "x"}}); err == nil || ruleOf(err) != "" {
		t.Fatalf("answers belong to the other tools: %v", err)
	}
	if _, err := adv("9"); ruleOf(err) != "not_responsible" {
		t.Fatalf("a stranger: %v", err)
	}
	if _, err := adv("5"); ruleOf(err) != "header_not_confirmed" {
		t.Fatalf("header: %v", err)
	}
	confirm(t, svc, id)

	// Pendiente -> En curso needs no block; the machine and the document move together.
	res, err := adv("5")
	if err != nil || res.Previous != "Pendiente" || res.Current != "En curso" || res.Order.WorkflowIndex == nil || *res.Order.WorkflowIndex != 1 {
		t.Fatalf("step 1: %+v err=%v", res, err)
	}
	doc := fs.entities[EntityType][id]
	if doc["state"] != "En curso" || intOf(doc["workflow_index"]) != 1 || fs.machines[idString(doc["machine_id"])].State != "En curso" || doc["completed_at"] != nil {
		t.Fatalf("after step 1: %v", doc)
	}

	// The step into the last state needs every block completed.
	_, err = adv("5")
	if ruleOf(err) != "blocks_incomplete" || !strings.Contains(err.Error(), "A") {
		t.Fatalf("blocks: %v", err)
	}
	runBlock(t, svc, id, "A", "9", map[string]any{"a1": true})
	runBlock(t, svc, id, "B", "5", nil)
	if _, err := adv("5"); ruleOf(err) != "blocks_incomplete" {
		t.Fatalf("C pending: %v", err)
	}
	runBlock(t, svc, id, "C", "6", nil)
	fin, err := adv("5")
	if err != nil || fin.Current != "Cerrada" || !fin.Order.Terminal || fin.Order.CompletedAt == "" {
		t.Fatalf("close: %+v err=%v", fin, err)
	}
	if !fs.machines[idString(doc["machine_id"])].Terminal {
		t.Fatal("the order's machine should be terminal")
	}
	if _, err := adv("5"); ruleOf(err) != "final_state" {
		t.Fatalf("after closing: %v", err)
	}
	if _, err := svc.ExecuteBlock(ctx, ExecuteBlockInput{OrderID: id, Block: "A", Intent: IntentSave, UserID: "9"}); ruleOf(err) != "final_state" {
		t.Fatalf("block after closing: %v", err)
	}

	// GetOrder shows the execution: header confirmation, block answers, responsibles.
	d, err := svc.GetOrder(ctx, id)
	if err != nil || d.HeaderConfirmedAt == "" || d.BlockAnswers["A"]["a1"] != true || len(d.Blocks) != 3 || d.Blocks[0].ResponsibleType != "user" || d.Blocks[0].ResponsibleID != "9" {
		t.Fatalf("GetOrder: %+v err=%v", d, err)
	}
}

func TestAdvanceOrder_ExecutionModel_StaleAndRefusedWalk(t *testing.T) {
	ctx := context.Background()
	fs, svc, id := execWorld(t)
	confirm(t, svc, id)
	one := 1
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: id, Input: InputComplete, UserID: "5", WorkflowIndex: &one}); ruleOf(err) != "stale_state" {
		t.Fatalf("stale index: %v", err)
	}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: id, Input: "start", UserID: "5"}); ruleOf(err) != "invalid_input" {
		t.Fatalf("input: %v", err)
	}
	// A concurrent change of the order is stale_state and nothing moves.
	fs.entities[EntityType][id]["_version"] = int64(7)
	fs.commitErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "version conflict"}
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: id, Input: InputComplete, UserID: "5"}); ruleOf(err) != "stale_state" {
		t.Fatalf("conflict: %v", err)
	}
	fs.commitErr = nil
	if fs.entities[EntityType][id]["state"] != "Pendiente" || fs.machines[idString(fs.entities[EntityType][id]["machine_id"])].State != "Pendiente" {
		t.Fatal("a refused advance moved something")
	}
}

func TestExecutionTools_RefuseOrdersWithoutTheExecutionModel(t *testing.T) {
	ctx := context.Background()
	fs := newFake() // its template orders are stored without a machine
	seedWorld(fs)
	svc := NewService(fs)
	o := mustCreate(t, svc, tplInput())
	if _, err := svc.ConfirmExecutionHeader(ctx, ConfirmHeaderInput{OrderID: o.ID, UserID: "5"}); ruleOf(err) != "unsupported_execution_model" {
		t.Fatalf("confirm: %v", err)
	}
	if _, err := svc.ExecuteBlock(ctx, ExecuteBlockInput{OrderID: o.ID, Block: "A", Intent: IntentStart, UserID: "5"}); ruleOf(err) != "unsupported_execution_model" {
		t.Fatalf("block: %v", err)
	}
	// Such an order still advances the older way (single write, no user needed).
	if _, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, HeaderValues: map[string]any{"observacion": "ok"}}); err != nil {
		t.Fatalf("older advance: %v", err)
	}
}

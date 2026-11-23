package orders

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func (f *fakeSubstrate) CreateMachineWithRef(_ context.Context, _ string, ref string) (Machine, error) {
	if f.failWith != nil {
		return Machine{}, f.failWith
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.machineCalls++
	if f.failMachineAt > 0 && f.machineCalls == f.failMachineAt {
		return Machine{}, &XoluError{Status: 500, Message: "machine creation failed"}
	}
	f.next++
	id := strconv.Itoa(2000 + f.next)
	f.machines[id] = &Machine{ID: id, State: "pending"}
	if f.refs == nil {
		f.refs = map[string]string{}
	}
	f.refs[id] = ref
	return *f.machines[id], nil
}

func (f *fakeSubstrate) DeleteMachine(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.machines[id]; !ok {
		return &XoluError{Status: 404, Message: "machine not found"}
	}
	delete(f.machines, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeSubstrate) Commit(_ context.Context, req CommitRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits++
	if f.commitErr != nil {
		return f.commitErr
	}
	if len(req.Append) > 25 {
		return &XoluError{Status: 400, Message: "too many appends"}
	}
	f.put(req.Update.Entity, strconv.FormatInt(req.Update.ID, 10), req.Update.Data)
	for _, a := range req.Append {
		id := ""
		if a.ID != nil {
			id = strconv.FormatInt(*a.ID, 10)
		} else {
			f.next++
			id = strconv.Itoa(f.next)
		}
		f.put(a.Entity, id, a.Data)
	}
	return f.commitSavesThenErr
}

// blockTemplate is a template with three blocks: B and C wait for A.
func blockTemplate(fs *fakeSubstrate, id string, blocks []any) {
	fs.put(TemplateEntityType, id, map[string]any{
		"name": "Con bloques", "description": "d", "version": 2, "latest": true,
		"asset_type_id": 5, "min_assets": 1, "max_assets": 2,
		"workflow_states":     []any{map[string]any{"name": "Pendiente"}, map[string]any{"name": "En curso"}, map[string]any{"name": "Cerrada"}},
		"header_items":        []any{map[string]any{"tag": "observacion", "type": "text", "to_fill_out": "upon_executing", "required": true}},
		"blocks":              blocks,
		"consumables_allowed": false,
	})
}

func threeBlocks() []any {
	return []any{
		map[string]any{"name": "A", "responsible_type": "user", "predecessor": "", "lines": []any{map[string]any{"name": "a1", "type": "boolean"}}},
		map[string]any{"name": "B", "responsible_type": "crew", "predecessor": "A", "lines": []any{}},
		map[string]any{"name": "C", "responsible_type": "crew", "predecessor": " a ", "lines": []any{}}, // trimmed, case-insensitive
	}
}

func blockInput() CreateInput {
	return CreateInput{TemplateID: "10", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7", AssetIDs: []ID{"11"}}
}

func newBlockWorld(t *testing.T, blocks []any) (*fakeSubstrate, *Service) {
	t.Helper()
	fs := newFake()
	seedWorld(fs)
	blockTemplate(fs, "10", blocks)
	return fs, NewService(fs)
}

func TestCreateOrder_WithBlocks_WritesOrderBlocksAndMachinesInOneCommit(t *testing.T) {
	fs, svc := newBlockWorld(t, threeBlocks())
	o, err := svc.CreateOrder(context.Background(), blockInput())
	if err != nil {
		t.Fatal(err)
	}
	id := intOf(o.ID)
	if id <= 0 || id >= 1<<52 {
		t.Fatalf("order id %d must fit JavaScript's exact integers", id)
	}
	if fs.commits != 1 {
		t.Fatalf("commits = %d, want exactly one", fs.commits)
	}
	doc := fs.entities[EntityType][o.ID]
	if doc == nil || doc["workflow_mode"] != "template" || intOf(doc["block_count"]) != 3 || intOf(doc["block_execution_version"]) != 1 ||
		intOf(doc["machine_id"]) != 0 || doc["state"] != "Pendiente" || doc["creation_token"] == "" || doc["creation_token"] == nil {
		t.Fatalf("order document = %v", doc)
	}
	if bv, ok := doc["block_values"].(map[string]any); !ok || len(bv) != 0 {
		t.Errorf("block_values = %v", doc["block_values"])
	}
	blocks := fs.entities[workOrderBlocksEntity]
	if len(blocks) != 3 || len(fs.machines) != 3 {
		t.Fatalf("blocks=%d machines=%d", len(blocks), len(fs.machines))
	}
	var names []string
	pred := map[string]int64{}
	for _, b := range blocks {
		names = append(names, b["name"].(string))
		pred[b["name"].(string)] = intOf(b["predecessor_index"])
		if intOf(b["work_order_id"]) != id || intOf(b["template_id"]) != 10 || intOf(b["template_version"]) != 2 ||
			b["state"] != "pending" || b["started_at"] != "" || b["completed_at"] != "" || b["created_at"] == nil {
			t.Errorf("block = %v", b)
		}
		if vals, ok := b["values"].(map[string]any); !ok || len(vals) != 0 {
			t.Errorf("block values = %v", b["values"])
		}
		mid := idString(b["machine_id"])
		want := "work_order:" + o.ID + ":block:" + strconv.FormatInt(intOf(b["block_index"]), 10)
		if fs.refs[mid] != want {
			t.Errorf("machine %s ref = %q, want %q", mid, fs.refs[mid], want)
		}
	}
	if pred["A"] != -1 || pred["B"] != 0 || pred["C"] != 0 {
		t.Errorf("predecessors = %v (names %v)", pred, names)
	}
	// GetOrder reads them back in order.
	d, err := svc.GetOrder(context.Background(), o.ID)
	if err != nil || len(d.Blocks) != 3 || d.Blocks[0].Name != "A" || d.Blocks[2].Name != "C" || d.Blocks[1].PredecessorIndex == nil {
		t.Fatalf("detail = %+v err=%v", d.Blocks, err)
	}
}

func TestCreateOrder_WithBlocks_EnsuresTheBlockDefinitionOnce(t *testing.T) {
	fs, svc := newBlockWorld(t, threeBlocks())
	rec := &namesSubstrate{fakeSubstrate: fs}
	svc = NewService(rec)
	for i := 0; i < 2; i++ {
		if _, err := svc.CreateOrder(context.Background(), blockInput()); err != nil {
			t.Fatal(err)
		}
	}
	if len(rec.names) != 1 || rec.names[0] != BlockDefinitionName {
		t.Fatalf("definitions ensured = %v", rec.names)
	}
	d := rec.defs[0]
	if d["initial"] != "pending" || d["determinism"] != "firstmatch" {
		t.Fatalf("definition = %v", d)
	}
}

type namesSubstrate struct {
	*fakeSubstrate
	names []string
	defs  []Definition
}

func (n *namesSubstrate) EnsureDefinition(ctx context.Context, name string, def Definition) (string, error) {
	n.names = append(n.names, name)
	n.defs = append(n.defs, def)
	return n.fakeSubstrate.EnsureDefinition(ctx, name, def)
}

func TestBlockDefinition_Lifecycle(t *testing.T) {
	d := BlockLifecycleDefinition()
	states := d["states"].(map[string]any)
	if len(states) != 3 || states["completed"].(map[string]any)["terminal"] != true || states["pending"].(map[string]any)["terminal"] != false {
		t.Fatalf("states = %v", states)
	}
	tr := d["transitions"].([]any)
	if len(tr) != 2 {
		t.Fatalf("transitions = %v", tr)
	}
	a, b := tr[0].(map[string]any), tr[1].(map[string]any)
	if a["from"] != "pending" || a["input"] != "start" || a["to"] != "in_progress" || b["from"] != "in_progress" || b["input"] != "complete" || b["to"] != "completed" {
		t.Fatalf("transitions = %v", tr)
	}
}

func TestCreateOrder_WithBlocks_InvalidDependencies(t *testing.T) {
	cases := map[string][]any{
		"empty name":          {map[string]any{"name": "  ", "lines": []any{}}},
		"duplicate name":      {map[string]any{"name": "A"}, map[string]any{"name": " a "}},
		"unknown predecessor": {map[string]any{"name": "A", "predecessor": "Z"}},
		"self predecessor":    {map[string]any{"name": "A", "predecessor": "A"}},
		"cycle of two":        {map[string]any{"name": "A", "predecessor": "B"}, map[string]any{"name": "B", "predecessor": "A"}},
		"cycle of three":      {map[string]any{"name": "A", "predecessor": "C"}, map[string]any{"name": "B", "predecessor": "A"}, map[string]any{"name": "C", "predecessor": "B"}},
	}
	for name, blocks := range cases {
		t.Run(name, func(t *testing.T) {
			fs, svc := newBlockWorld(t, blocks)
			_, err := svc.CreateOrder(context.Background(), blockInput())
			ve := wantInvalid(t, err, "template_id")
			if !strings.Contains(ve.Reason, "block") {
				t.Errorf("reason = %q", ve.Reason)
			}
			if fs.machineCalls != 0 || fs.commits != 0 || len(fs.entities[EntityType]) != 0 {
				t.Errorf("nothing may be created: machines=%d commits=%d", fs.machineCalls, fs.commits)
			}
		})
	}
}

func TestCreateOrder_WithBlocks_ForwardPredecessorIsFine(t *testing.T) {
	blocks := []any{map[string]any{"name": "A", "predecessor": "B"}, map[string]any{"name": "B"}}
	fs, svc := newBlockWorld(t, blocks)
	if _, err := svc.CreateOrder(context.Background(), blockInput()); err != nil {
		t.Fatal(err)
	}
	for _, b := range fs.entities[workOrderBlocksEntity] {
		if b["name"] == "A" && intOf(b["predecessor_index"]) != 1 {
			t.Errorf("A predecessor = %v", b["predecessor_index"])
		}
	}
}

func TestCreateOrder_WithBlocks_TooManyForOneCommit(t *testing.T) {
	var blocks []any
	for i := 0; i < 26; i++ {
		blocks = append(blocks, map[string]any{"name": "B" + strconv.Itoa(i)})
	}
	fs, svc := newBlockWorld(t, blocks)
	_, err := svc.CreateOrder(context.Background(), blockInput())
	ve := wantInvalid(t, err, "template_id")
	if !strings.Contains(ve.Reason, "25") || fs.machineCalls != 0 {
		t.Fatalf("reason=%q machines=%d", ve.Reason, fs.machineCalls)
	}
	// Exactly 25 is allowed.
	fs, svc = newBlockWorld(t, blocks[:25])
	if _, err := svc.CreateOrder(context.Background(), blockInput()); err != nil {
		t.Fatalf("25 blocks: %v", err)
	}
	if len(fs.entities[workOrderBlocksEntity]) != 25 {
		t.Fatalf("blocks = %d", len(fs.entities[workOrderBlocksEntity]))
	}
}

func TestCreateOrder_WithBlocks_MachineFailureCleansUp(t *testing.T) {
	fs, svc := newBlockWorld(t, threeBlocks())
	fs.failMachineAt = 3
	_, err := svc.CreateOrder(context.Background(), blockInput())
	if err == nil {
		t.Fatal("want an error")
	}
	if fs.commits != 0 || len(fs.deleted) != 2 || len(fs.machines) != 0 || len(fs.entities[EntityType]) != 0 {
		t.Fatalf("commits=%d deleted=%v machines=%d orders=%d", fs.commits, fs.deleted, len(fs.machines), len(fs.entities[EntityType]))
	}
}

func TestCreateOrder_WithBlocks_RejectedCommitDeletesTheMachines(t *testing.T) {
	for _, status := range []int{400, 409, 422} {
		fs, svc := newBlockWorld(t, threeBlocks())
		fs.commitErr = &XoluError{Status: status, Code: "XOLU-X", Message: "rejected"}
		_, err := svc.CreateOrder(context.Background(), blockInput())
		if err == nil {
			t.Fatalf("%d: want an error", status)
		}
		var ue *UnavailableError
		if errors.As(err, &ue) {
			t.Errorf("%d: a rejection is not an outage: %v", status, err)
		}
		if len(fs.deleted) != 3 || len(fs.machines) != 0 || len(fs.entities[EntityType]) != 0 {
			t.Errorf("%d: deleted=%v machines=%d orders=%d", status, fs.deleted, len(fs.machines), len(fs.entities[EntityType]))
		}
	}
}

func TestCreateOrder_WithBlocks_UncertainCommitThatWasSavedIsSuccess(t *testing.T) {
	fs, svc := newBlockWorld(t, threeBlocks())
	fs.commitSavesThenErr = &XoluError{Status: 503, Message: "response lost"}
	o, err := svc.CreateOrder(context.Background(), blockInput())
	if err != nil {
		t.Fatalf("the commit was saved: %v", err)
	}
	if len(fs.deleted) != 0 || len(fs.machines) != 3 || fs.entities[EntityType][o.ID] == nil {
		t.Fatalf("deleted=%v machines=%d", fs.deleted, len(fs.machines))
	}
}

func TestCreateOrder_WithBlocks_UncertainCommitNotSavedKeepsMachinesAndSaysWhichOrder(t *testing.T) {
	fs, svc := newBlockWorld(t, threeBlocks())
	fs.commitErr = &XoluError{Status: 503, Message: "timeout"}
	_, err := svc.CreateOrder(context.Background(), blockInput())
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "may have been created") {
		t.Errorf("the error should say the order may exist: %v", err)
	}
	// The commit may still land: its machines must not be deleted.
	if len(fs.deleted) != 0 || len(fs.machines) != 3 {
		t.Fatalf("deleted=%v machines=%d", fs.deleted, len(fs.machines))
	}
}

func TestCreateOrder_ConsumablesTemplateIsStillRejected(t *testing.T) {
	fs, svc := newBlockWorld(t, threeBlocks())
	fs.entities[TemplateEntityType]["10"]["consumables_allowed"] = true
	_, err := svc.CreateOrder(context.Background(), blockInput())
	ve := wantInvalid(t, err, "template_id")
	if !strings.Contains(ve.Reason, "consumables") || fs.machineCalls != 0 {
		t.Fatalf("reason=%q machines=%d", ve.Reason, fs.machineCalls)
	}
}

func TestListTemplates_BlockTemplatesAreUsable(t *testing.T) {
	fs, svc := newBlockWorld(t, threeBlocks())
	out, err := svc.ListTemplates(context.Background(), ListTemplatesInput{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tp := range out.Templates {
		if tp.ID == "10" && (!tp.Supported || len(tp.Blocks) != 3) {
			t.Fatalf("block template = %+v", tp)
		}
	}
	_ = fs
}

func TestCreateOrder_WithoutBlocksStaysASingleWrite(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	o := mustCreate(t, NewService(fs), tplInput())
	if fs.commits != 0 || fs.machineCalls != 0 {
		t.Fatalf("commits=%d machines=%d", fs.commits, fs.machineCalls)
	}
	if bv, ok := fs.entities[EntityType][o.ID]["block_values"].(map[string]any); !ok || len(bv) != 0 {
		t.Errorf("block_values = %v", fs.entities[EntityType][o.ID]["block_values"])
	}
}

func TestCreateOrder_LegacyOrderCarriesBlockValues(t *testing.T) {
	fs := newFake()
	o, err := NewService(fs).CreateOrder(context.Background(), CreateInput{Title: "Legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if bv, ok := fs.entities[EntityType][o.ID]["block_values"].(map[string]any); !ok || len(bv) != 0 {
		t.Errorf("block_values = %v", fs.entities[EntityType][o.ID]["block_values"])
	}
}

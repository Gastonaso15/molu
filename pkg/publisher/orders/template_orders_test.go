package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// seedWorld stores what a template order points at: a template with three
// states, a crew and two assets of different types.
//
//	template 1: asks "prioridad" (number, at creation, required, default 1),
//	"observacion" (text, while executing, required) and "fecha" (date, while executing);
//	needs 1 or 2 assets of type 5.
func seedWorld(fs *fakeSubstrate) {
	fs.put(TemplateEntityType, "1", map[string]any{
		"name": "Pump check", "description": "Check the pump", "version": 3, "latest": true,
		"asset_type_id": 5, "min_assets": 1, "max_assets": 2,
		"workflow_states": []any{map[string]any{"name": "Pending"}, map[string]any{"name": "Working"}, map[string]any{"name": "Done"}},
		"header_items": []any{
			map[string]any{"tag": "prioridad", "type": "number", "default_value": "1", "to_fill_out": "upon_creating", "required": true},
			map[string]any{"tag": "observacion", "type": "text", "to_fill_out": "upon_executing", "required": true},
			map[string]any{"tag": "fecha", "type": "date", "to_fill_out": "upon_executing"},
		},
		"blocks": []any{}, "consumables_allowed": false,
	})
	fs.put("crews", "7", map[string]any{"name": "Cuadrilla A"})
	fs.put("outsourced", "8", map[string]any{"name": "Tercero SA"})
	fs.put(assetEntity, "11", map[string]any{"name": "Bomba 1", "type_id": 5})
	fs.put(assetEntity, "12", map[string]any{"name": "Valvula", "type_id": 6})
}

func tplInput() CreateInput {
	return CreateInput{
		TemplateID: "1", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7",
		AssetIDs: []ID{"11"},
	}
}

func mustCreate(t *testing.T, svc *Service, in CreateInput) WorkOrder {
	t.Helper()
	o, err := svc.CreateOrder(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	return o
}

func wantInvalid(t *testing.T, err error, fields ...string) *ValidationError {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError on %v, got %T %v", fields, err, err)
	}
	for _, f := range fields {
		found := false
		for _, got := range ve.Fields {
			found = found || got == f
		}
		if !found {
			t.Fatalf("ValidationError fields = %v, want %q among them (reason: %s)", ve.Fields, f, ve.Reason)
		}
	}
	return ve
}

func wantRule(t *testing.T, err error, rule string) *TransitionError {
	t.Helper()
	var te *TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("want TransitionError rule %q, got %T %v", rule, err, err)
	}
	if te.Code != "XOLU-INVALID_TRANSITION" || te.Details["rule"] != rule || te.Details["source"] != publisherSourceID {
		t.Fatalf("TransitionError = %+v, want rule %q from the publisher", te, rule)
	}
	return te
}

// ---- creation ----

func TestTemplateOrder_Create_WritesSeamDocument(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	o := mustCreate(t, NewService(fs), tplInput())

	doc := fs.entities[EntityType][o.ID]
	checks := map[string]any{
		"state": "Pending", "machine_id": int64(0), "workflow_mode": "template", "workflow_index": 0,
		"template_id": int64(1), "template_version": 3, "assignment_type": AssignmentDirect,
		"responsible_type": "crew", "responsible_id": int64(7), "assigned_to": "Cuadrilla A",
		"title": "Pump check", "description": "Check the pump", // defaults from the template
	}
	for k, want := range checks {
		if doc[k] != want {
			t.Errorf("document[%s] = %#v, want %#v", k, doc[k], want)
		}
	}
	if hv, _ := doc["header_values"].(map[string]any); hv["prioridad"] != float64(1) {
		t.Errorf("header default not applied: %v", doc["header_values"])
	}
	if ids, _ := doc["asset_ids"].([]int64); len(ids) != 1 || ids[0] != 11 {
		t.Errorf("asset_ids = %#v", doc["asset_ids"])
	}
	if o.TemplateID != "1" || o.WorkflowIndex == nil || *o.WorkflowIndex != 0 || o.Terminal || o.State != "Pending" {
		t.Errorf("order = %+v", o)
	}
}

func TestTemplateOrder_Create_ExplicitValuesWin(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	in := tplInput()
	in.Title, in.Description = "My title", "My text"
	in.TemplateVersion = 3
	in.HeaderValues = map[string]any{"prioridad": "5"} // a numeric string is accepted
	o := mustCreate(t, NewService(fs), in)
	if o.Title != "My title" || o.Description != "My text" || o.HeaderValues["prioridad"] != float64(5) {
		t.Fatalf("order = %+v", o)
	}
}

func TestTemplateOrder_Create_BoardPosting(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	in := tplInput()
	in.AssignmentType, in.ResponsibleType, in.ResponsibleID = AssignmentBoard, "user", ""
	o := mustCreate(t, NewService(fs), in)
	if o.AssignmentType != AssignmentBoard || o.ResponsibleType != "user" || o.ResponsibleID != "" || o.AssignedTo != "" {
		t.Fatalf("order = %+v", o)
	}
}

func TestTemplateOrder_Create_ThirdPartyAssignment(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	in := tplInput()
	in.ResponsibleType, in.ResponsibleID = "third_party", "8"
	if o := mustCreate(t, NewService(fs), in); o.AssignedTo != "Tercero SA" || o.ResponsibleID != "8" {
		t.Fatalf("order = %+v", o)
	}
}

func TestTemplateOrder_Create_Validation(t *testing.T) {
	cases := map[string]struct {
		mutate func(*CreateInput)
		fields []string
		reason string
	}{
		"assignment is required": {func(in *CreateInput) { in.AssignmentType, in.ResponsibleType, in.ResponsibleID = "", "", "" },
			[]string{"assignment_type", "responsible_type"}, "required"},
		"unknown assignment type": {func(in *CreateInput) { in.AssignmentType = "whatever" },
			[]string{"assignment_type"}, "direct_assignment or board_posting"},
		"user cannot be assigned directly": {func(in *CreateInput) { in.ResponsibleType = "user" },
			[]string{"responsible_type"}, "users are not in xolu"},
		"direct assignment needs an id": {func(in *CreateInput) { in.ResponsibleID = "" },
			[]string{"responsible_id"}, "required for a direct assignment"},
		"crew must exist": {func(in *CreateInput) { in.ResponsibleID = "99" },
			[]string{"responsible_id"}, "does not exist"},
		"board posting has no responsible": {func(in *CreateInput) { in.AssignmentType = AssignmentBoard },
			[]string{"responsible_id"}, "no responsible until someone claims"},
		"template must exist": {func(in *CreateInput) { in.TemplateID = "404" },
			[]string{"template_id"}, "template not found"},
		"template id must be numeric": {func(in *CreateInput) { in.TemplateID = "abc" },
			[]string{"template_id"}, "positive id"},
		"template version must match": {func(in *CreateInput) { in.TemplateVersion = 2 },
			[]string{"template_version"}, "version 3, not 2"},
		"unknown header tag": {func(in *CreateInput) { in.HeaderValues = map[string]any{"nope": "x"} },
			[]string{"header_values.nope"}, "not a header item"},
		"a header filled later is not accepted now": {func(in *CreateInput) { in.HeaderValues = map[string]any{"observacion": "x"} },
			[]string{"header_values.observacion"}, "upon_creating"},
		"number header must be a number": {func(in *CreateInput) { in.HeaderValues = map[string]any{"prioridad": "alta"} },
			[]string{"header_values.prioridad"}, "must be a number"},
		"assets: at least min": {func(in *CreateInput) { in.AssetIDs = nil },
			[]string{"asset_ids"}, "at least 1"},
		"assets: at most max": {func(in *CreateInput) { in.AssetIDs = []ID{"11", "11", "11"} },
			[]string{"asset_ids"}, "at most 2"},
		"assets: must exist": {func(in *CreateInput) { in.AssetIDs = []ID{"99"} },
			[]string{"asset_ids"}, "does not exist"},
		"assets: right type": {func(in *CreateInput) { in.AssetIDs = []ID{"12"} },
			[]string{"asset_ids"}, "asset type"},
		"assets: no repeats": {func(in *CreateInput) { in.AssetIDs = []ID{"11", "11"} },
			[]string{"asset_ids"}, "repeated"},
		"assets: ids are numbers": {func(in *CreateInput) { in.AssetIDs = []ID{"x"} },
			[]string{"asset_ids"}, "not an asset id"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fs := newFake()
			seedWorld(fs)
			in := tplInput()
			tc.mutate(&in)
			_, err := NewService(fs).CreateOrder(context.Background(), in)
			ve := wantInvalid(t, err, tc.fields...)
			if !strings.Contains(ve.Reason, tc.reason) {
				t.Fatalf("reason = %q, want it to contain %q", ve.Reason, tc.reason)
			}
			if len(fs.entities[EntityType]) != 0 {
				t.Fatal("a rejected request must not store an order")
			}
		})
	}
}

func TestTemplateOrder_Create_ReportsEveryProblemAtOnce(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	in := tplInput()
	in.ResponsibleID = "99"
	in.HeaderValues = map[string]any{"prioridad": "alta"}
	in.AssetIDs = []ID{"12"}
	_, err := NewService(fs).CreateOrder(context.Background(), in)
	wantInvalid(t, err, "responsible_id", "header_values.prioridad", "asset_ids")
}

func TestTemplateOrder_Create_RequiredHeaderWithoutDefault(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	tpl := fs.entities[TemplateEntityType]["1"]
	tpl["header_items"] = []any{map[string]any{"tag": "serie", "type": "text", "to_fill_out": "upon_creating", "required": true}}
	_, err := NewService(fs).CreateOrder(context.Background(), tplInput())
	wantInvalid(t, err, "header_values.serie")

	in := tplInput()
	in.HeaderValues = map[string]any{"serie": "AB-1"}
	if o := mustCreate(t, NewService(fs), in); o.HeaderValues["serie"] != "AB-1" {
		t.Fatalf("order = %+v", o)
	}
}

func TestTemplateOrder_Create_HeaderTypes(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	fs.entities[TemplateEntityType]["1"]["header_items"] = []any{
		map[string]any{"tag": "ok", "type": "boolean", "to_fill_out": "upon_creating"},
		map[string]any{"tag": "dia", "type": "date", "to_fill_out": "upon_creating"},
		map[string]any{"tag": "donde", "type": "gps", "to_fill_out": "upon_creating"},
		map[string]any{"tag": "equipo", "type": "asset", "to_fill_out": "upon_creating"},
	}
	svc := NewService(fs)

	in := tplInput()
	in.HeaderValues = map[string]any{"ok": true, "dia": "2026-11-17", "donde": "-34.9011,-56.1645", "equipo": 11}
	o := mustCreate(t, svc, in)
	if o.HeaderValues["ok"] != true || o.HeaderValues["dia"] != "2026-11-17" || o.HeaderValues["donde"] != "-34.9011,-56.1645" || o.HeaderValues["equipo"] != int64(11) {
		t.Fatalf("header_values = %v", o.HeaderValues)
	}

	// An unchecked box is stored as false, like the form posts it.
	if o := mustCreate(t, svc, tplInput()); o.HeaderValues["ok"] != false {
		t.Fatalf("absent boolean = %v", o.HeaderValues["ok"])
	}

	for name, hv := range map[string]map[string]any{
		"bad date":    {"dia": "17/11/2026"},
		"bad gps":     {"donde": "91,0"},
		"gps format":  {"donde": "abc"},
		"bad bool":    {"ok": "maybe"},
		"asset id":    {"equipo": "x"},
		"asset exist": {"equipo": 999},
	} {
		in := tplInput()
		in.HeaderValues = hv
		if _, err := svc.CreateOrder(context.Background(), in); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func TestTemplateOrder_Create_UnsupportedTemplates(t *testing.T) {
	for name, patch := range map[string]map[string]any{
		"consumables": {"consumables_allowed": true},
	} {
		t.Run(name, func(t *testing.T) {
			fs := newFake()
			seedWorld(fs)
			for k, v := range patch {
				fs.entities[TemplateEntityType]["1"][k] = v
			}
			_, err := NewService(fs).CreateOrder(context.Background(), tplInput())
			ve := wantInvalid(t, err, "template_id")
			if !strings.Contains(ve.Reason, "does not support yet") {
				t.Fatalf("reason = %q", ve.Reason)
			}
			if len(fs.entities[EntityType]) != 0 {
				t.Fatal("must not store an order")
			}
		})
	}
}

func TestTemplateOrder_Create_InvalidWorkflow(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	fs.entities[TemplateEntityType]["1"]["workflow_states"] = []any{map[string]any{"name": "Only"}}
	_, err := NewService(fs).CreateOrder(context.Background(), tplInput())
	wantInvalid(t, err, "template_id")
}

func TestOrder_Create_LegacyWithAssignment(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	o := mustCreate(t, NewService(fs), CreateInput{Title: "Legacy", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "7"})
	doc := fs.entities[EntityType][o.ID]
	if o.AssignedTo != "Cuadrilla A" || doc["responsible_id"] != int64(7) || doc["state"] != StateDraft || doc["workflow_mode"] != nil {
		t.Fatalf("order = %+v doc = %v", o, doc)
	}
}

func TestOrder_Create_LegacyRejectsTemplateOnlyFields(t *testing.T) {
	svc := NewService(newFake())
	for name, in := range map[string]CreateInput{
		"header_values":    {Title: "t", HeaderValues: map[string]any{"a": 1}},
		"asset_ids":        {Title: "t", AssetIDs: []ID{"1"}},
		"template_version": {Title: "t", TemplateVersion: 2},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.CreateOrder(context.Background(), in)
			wantInvalid(t, err, name)
		})
	}
}

// ---- advancing ----

func newTemplateOrder(t *testing.T) (*fakeSubstrate, *Service, WorkOrder) {
	t.Helper()
	fs := newFake()
	seedWorld(fs)
	fs.entities[EntityType] = map[string]map[string]any{}
	svc := NewService(fs)
	o := mustCreate(t, svc, tplInput())
	fs.entities[EntityType][o.ID]["_version"] = int64(1) // what xolu stamps on a stored document
	return fs, svc, o
}

func advance(svc *Service, id string, mut func(*AdvanceInput)) (AdvanceResult, error) {
	in := AdvanceInput{OrderID: id, Input: InputComplete, HeaderValues: map[string]any{"observacion": "ok"}}
	if mut != nil {
		mut(&in)
	}
	return svc.AdvanceOrder(context.Background(), in)
}

func TestTemplateOrder_Advance_WalksTheWholeWorkflow(t *testing.T) {
	fs, svc, o := newTemplateOrder(t)

	res, err := advance(svc, o.ID, func(in *AdvanceInput) {
		in.HeaderValues["fecha"] = "2026-11-18"
		in.WorkflowIndex = ptr(0)
	})
	if err != nil {
		t.Fatalf("step 1: %v", err)
	}
	if res.Previous != "Pending" || res.Current != "Working" || res.Order.State != "Working" || *res.Order.WorkflowIndex != 1 || res.Order.Terminal {
		t.Fatalf("step 1 result = %+v", res)
	}
	doc := fs.entities[EntityType][o.ID]
	hv, _ := doc["header_values"].(map[string]any)
	if doc["state"] != "Working" || doc["workflow_index"] != 1 || hv["observacion"] != "ok" || hv["fecha"] != "2026-11-18" || hv["prioridad"] != float64(1) {
		t.Fatalf("document after step 1 = %v", doc)
	}
	if doc["technician_signature"] != nil || doc["completed_at"] != nil {
		t.Fatalf("no sign-off yet: %v", doc)
	}
	if doc["_version"] != int64(2) {
		t.Fatalf("the update must go through the optimistic _version, doc version = %v", doc["_version"])
	}

	// The last step is the sign-off.
	res, err = advance(svc, o.ID, func(in *AdvanceInput) { in.Payload = map[string]any{"technician_signature": "sig"} })
	if err != nil {
		t.Fatalf("step 2: %v", err)
	}
	if res.Current != "Done" || !res.Order.Terminal || res.Order.CompletedAt == "" || *res.Order.WorkflowIndex != 2 {
		t.Fatalf("step 2 result = %+v", res)
	}
	doc = fs.entities[EntityType][o.ID]
	if doc["state"] != "Done" || doc["technician_signature"] != "sig" || doc["completed_at"] == nil {
		t.Fatalf("document after step 2 = %v", doc)
	}

	// It reads back as finished.
	got, _ := svc.GetOrder(context.Background(), o.ID)
	if !got.Terminal || got.State != "Done" {
		t.Fatalf("GetOrder = %+v", got)
	}

	// And there is nowhere left to go.
	_, err = advance(svc, o.ID, nil)
	wantRule(t, err, "final_state")
}

func ptr(n int) *int { return &n }

func TestTemplateOrder_Advance_RequiredExecutionHeader(t *testing.T) {
	fs, svc, o := newTemplateOrder(t)
	_, err := advance(svc, o.ID, func(in *AdvanceInput) { in.HeaderValues = nil })
	wantInvalid(t, err, "header_values.observacion")
	if fs.entities[EntityType][o.ID]["state"] != "Pending" {
		t.Fatal("a rejected step must not change the order")
	}
}

func TestTemplateOrder_Advance_KeepsStoredAnswersAcrossSteps(t *testing.T) {
	_, svc, o := newTemplateOrder(t)
	if _, err := advance(svc, o.ID, nil); err != nil {
		t.Fatal(err)
	}
	// The required answer is already stored: the next step needs only the signature.
	res, err := advance(svc, o.ID, func(in *AdvanceInput) {
		in.HeaderValues = nil
		in.Payload = map[string]any{"technician_signature": "sig"}
	})
	if err != nil || res.Order.HeaderValues["observacion"] != "ok" {
		t.Fatalf("res = %+v err = %v", res, err)
	}
}

func TestTemplateOrder_Advance_HeaderValidation(t *testing.T) {
	_, svc, o := newTemplateOrder(t)
	for name, hv := range map[string]map[string]any{
		"bad date":                  {"observacion": "ok", "fecha": "ayer"},
		"a creation item is closed": {"observacion": "ok", "prioridad": 3},
		"unknown":                   {"observacion": "ok", "zzz": 1},
	} {
		_, err := advance(svc, o.ID, func(in *AdvanceInput) { in.HeaderValues = hv })
		if err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func TestTemplateOrder_Advance_LastStepNeedsSignature(t *testing.T) {
	fs, svc, o := newTemplateOrder(t)
	if _, err := advance(svc, o.ID, nil); err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]map[string]any{"missing": nil, "blank": {"technician_signature": "  "}} {
		_, err := advance(svc, o.ID, func(in *AdvanceInput) { in.Payload = payload })
		wantInvalid(t, err, "payload.technician_signature")
		if fs.entities[EntityType][o.ID]["state"] != "Working" {
			t.Fatalf("%s: a rejected sign-off must not change the order", name)
		}
	}
}

func TestTemplateOrder_Advance_Rejections(t *testing.T) {
	t.Run("start is not a template input", func(t *testing.T) {
		_, svc, o := newTemplateOrder(t)
		_, err := advance(svc, o.ID, func(in *AdvanceInput) { in.Input = InputStart })
		te := wantRule(t, err, "invalid_input")
		if te.Message != `no transition for input "start" from state "Pending": template workflows only accept "complete"` {
			t.Fatalf("message = %q", te.Message)
		}
	})
	t.Run("stale workflow_index", func(t *testing.T) {
		_, svc, o := newTemplateOrder(t)
		_, err := advance(svc, o.ID, func(in *AdvanceInput) { in.WorkflowIndex = ptr(1) })
		wantRule(t, err, "stale_state")
	})
	t.Run("stale document version", func(t *testing.T) {
		fs, _, o := newTemplateOrder(t)
		// Someone else updates the order between our read and our write.
		raced := NewService(&racingSubstrate{fakeSubstrate: fs, id: o.ID})
		_, err := advance(raced, o.ID, nil)
		wantRule(t, err, "stale_state")
		if fs.entities[EntityType][o.ID]["state"] != "Pending" {
			t.Fatal("a conflicting update must not be applied")
		}
	})
	t.Run("awaiting claim", func(t *testing.T) {
		fs, svc, o := newTemplateOrder(t)
		d := fs.entities[EntityType][o.ID]
		d["assignment_type"], d["responsible_id"] = AssignmentBoard, int64(0)
		_, err := advance(svc, o.ID, nil)
		wantRule(t, err, "awaiting_claim")
	})
	t.Run("order with a mobile warehouse", func(t *testing.T) {
		fs, svc, o := newTemplateOrder(t)
		fs.entities[EntityType][o.ID]["mobile_warehouse_id"] = int64(5)
		_, err := advance(svc, o.ID, nil)
		wantInvalid(t, err, "order_id")
	})
	t.Run("template version changed", func(t *testing.T) {
		fs, svc, o := newTemplateOrder(t)
		fs.entities[TemplateEntityType]["1"]["version"] = 4
		_, err := advance(svc, o.ID, nil)
		wantRule(t, err, "template_version")
	})
	t.Run("template gone", func(t *testing.T) {
		fs, svc, o := newTemplateOrder(t)
		delete(fs.entities[TemplateEntityType], "1")
		_, err := advance(svc, o.ID, nil)
		wantRule(t, err, "template_unavailable")
	})
	t.Run("state does not match the position", func(t *testing.T) {
		fs, svc, o := newTemplateOrder(t)
		fs.entities[EntityType][o.ID]["state"] = "Done"
		_, err := advance(svc, o.ID, nil)
		wantRule(t, err, "invalid_workflow")
	})
	t.Run("unknown order", func(t *testing.T) {
		_, svc, _ := newTemplateOrder(t)
		_, err := advance(svc, "404", nil)
		var nf *NotFoundError
		if !errors.As(err, &nf) {
			t.Fatalf("want NotFoundError, got %v", err)
		}
	})
}

// racingSubstrate bumps the order's _version right after the service reads it,
// the way a concurrent writer would.
type racingSubstrate struct {
	*fakeSubstrate
	id string
}

func (r *racingSubstrate) GetEntity(ctx context.Context, entityType, id string) (map[string]any, error) {
	doc, err := r.fakeSubstrate.GetEntity(ctx, entityType, id)
	if err == nil && entityType == EntityType && id == r.id {
		r.mu.Lock()
		r.entities[EntityType][id]["_version"] = intOf(doc["_version"]) + 1
		r.mu.Unlock()
	}
	return doc, err
}

func TestTemplateOrder_Advance_PatchFailureIsReported(t *testing.T) {
	fs, svc, o := newTemplateOrder(t)
	fs.patchErr = &XoluError{Status: 500, Code: "XOLU-ST006", Message: "boom"}
	_, err := advance(svc, o.ID, nil)
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("want UnavailableError, got %v", err)
	}
}

func TestOrder_Advance_LegacyRejectsTemplateOnlyFields(t *testing.T) {
	fs := newFake()
	svc := NewService(fs)
	o := mustCreate(t, svc, CreateInput{Title: "t"})
	_, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: InputStart, HeaderValues: map[string]any{"a": 1}})
	wantInvalid(t, err, "header_values")
	_, err = svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: InputStart, WorkflowIndex: ptr(0)})
	wantInvalid(t, err, "workflow_index")
}

// ---- templates listing ----

func TestListTemplates_SaysWhatCanBeUsed(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	fs.put(TemplateEntityType, "2", map[string]any{"name": "With consumables", "version": 1, "latest": true,
		"workflow_states":     []any{map[string]any{"name": "A"}, map[string]any{"name": "B"}},
		"consumables_allowed": true})
	got, err := NewService(fs).ListTemplates(context.Background(), ListTemplatesInput{})
	if err != nil || len(got.Templates) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	byID := map[string]Template{}
	for _, tpl := range got.Templates {
		byID[tpl.ID] = tpl
	}
	ok, blocked := byID["1"], byID["2"]
	if !ok.Supported || ok.Reason != "" || ok.MinAssets != 1 || ok.MaxAssets != 2 || ok.AssetTypeID != "5" || len(ok.HeaderItems) != 3 {
		t.Fatalf("usable template = %+v", ok)
	}
	h := ok.HeaderItems[0]
	if h.Tag != "prioridad" || h.Type != "number" || h.FilledAt != "upon_creating" || !h.Required || h.DefaultValue != "1" {
		t.Fatalf("header item = %+v", h)
	}
	if blocked.Supported || !strings.Contains(blocked.Reason, "consumables") {
		t.Fatalf("template with consumables = %+v", blocked)
	}
}

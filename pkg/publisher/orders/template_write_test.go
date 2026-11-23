package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func seedTemplateWrite(fs *fakeSubstrate) {
	fs.put("asset_types", "5", map[string]any{"name": "Bomba"})
	fs.put(assetEntity, "11", map[string]any{"name": "Bomba 1", "type_id": int64(5)})
	fs.put(crewEntity, "7", map[string]any{"name": "Cuadrilla A"})
	fs.put(consumableEntity, "1", map[string]any{"name": "Filtro", "enabled": true})
	fs.put(consumableEntity, "2", map[string]any{"name": "Aceite", "enabled": true})
	fs.put(woActionEntity, "3", map[string]any{"name": "Notificar", "enabled": true})
}

func validTemplateInput() CreateTemplateInput {
	return CreateTemplateInput{
		Name:        "Inspeccion de bomba",
		Description: "Preventivo mensual",
		Color:       "blue",
		Icon:        "build",
		AssetTypeID: "5",
		MinAssets:   1,
		MaxAssets:   2,
		WorkflowStates: []string{
			"Pendiente",
			"En curso",
			"Finalizada",
		},
		HeaderItems: []TemplateHeaderItem{
			{Tag: "prioridad", Type: "number", FilledAt: "upon_creating", Required: true, DefaultValue: "1"},
			{Tag: "notas", Type: "text", FilledAt: "upon_executing", Required: false},
		},
		Blocks: []TemplateBlock{
			{
				Name:            "Preparacion",
				ResponsibleType: "user",
				Lines:           []TemplateBlockLine{{Name: "aislado", Type: "boolean"}},
			},
			{
				Name:            "Ejecucion",
				ResponsibleType: "crew",
				Predecessor:     "Preparacion",
				Lines:           []TemplateBlockLine{{Name: "horas", Type: "number"}},
			},
		},
		ConsumablesEnabled:      true,
		ExtraConsumablesAllowed: false,
		Consumables:             []TemplateConsumable{{ConsumableID: "1", Quantity: 2}},
		Actions: []TemplateAction{{
			ActionID:       "3",
			TriggerPoint:   "after_block",
			BlockReference: "Ejecucion",
			MappingValues:  []ActionMapping{{Parameter: "msg", Source: "fixed", FixedValue: "done"}},
		}},
		Rules:     []map[string]any{{"name": "Regla 1", "trigger": "on_evaluate_header"}},
		Numbering: &Numbering{Prefix: "INS-", Suffix: "/26", SequenceLength: 4, ResetPolicy: "yearly"},
	}
}

func TestCreateTemplate_FullStructureAndInstantiable(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedTemplateWrite(fs)
	svc := NewService(fs)

	tpl, err := svc.CreateTemplate(ctx, validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	if tpl.ID == "" || tpl.Version != 1 || !tpl.Latest || !tpl.Supported || tpl.Reason != "" {
		t.Fatalf("tpl = %+v", tpl)
	}
	if len(tpl.WorkflowStates) != 3 || len(tpl.HeaderItems) != 2 || len(tpl.Blocks) != 2 ||
		!tpl.ConsumablesEnabled || len(tpl.Consumables) != 1 || tpl.Consumables[0].Name != "Filtro" ||
		len(tpl.Actions) != 1 || tpl.Actions[0].Name != "Notificar" || tpl.Numbering == nil || tpl.Numbering.Prefix != "INS-" {
		t.Fatalf("template details = %+v", tpl)
	}

	// An order can be opened immediately from the created template!
	order, err := svc.CreateOrder(ctx, CreateInput{
		TemplateID:      ID(tpl.ID),
		AssignmentType:  AssignmentDirect,
		ResponsibleType: "crew",
		ResponsibleID:   "7",
		AssetIDs:        []ID{"11"},
	})
	if err != nil {
		t.Fatalf("CreateOrder from created template: %v", err)
	}
	if order.State != "Pendiente" || order.MobileWarehouseID != order.ID {
		t.Fatalf("order = %+v", order)
	}
}

func TestCreateTemplate_Validation(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedTemplateWrite(fs)
	svc := NewService(fs)

	mutate := func(fn func(*CreateTemplateInput)) CreateTemplateInput {
		in := validTemplateInput()
		fn(&in)
		return in
	}

	blocks25 := make([]TemplateBlock, 25)
	for i := range blocks25 {
		blocks25[i] = TemplateBlock{Name: "B" + string(rune('A'+i)), ResponsibleType: "crew"}
	}

	cases := []struct {
		name  string
		in    CreateTemplateInput
		field string
	}{
		{"empty name", mutate(func(i *CreateTemplateInput) { i.Name = "  " }), "name"},
		{"long name", mutate(func(i *CreateTemplateInput) { i.Name = strings.Repeat("a", 201) }), "name"},
		{"one state", mutate(func(i *CreateTemplateInput) { i.WorkflowStates = []string{"Solo"} }), "workflow_states"},
		{"duplicate state", mutate(func(i *CreateTemplateInput) { i.WorkflowStates = []string{"A", "A"} }), "workflow_states"},
		{"min > max assets", mutate(func(i *CreateTemplateInput) { i.MinAssets, i.MaxAssets = 3, 1 }), "max_assets"},
		{"unknown asset type", mutate(func(i *CreateTemplateInput) { i.AssetTypeID = "999" }), "asset_type_id"},
		{"bad header type", mutate(func(i *CreateTemplateInput) { i.HeaderItems[0].Type = "video" }), "header_items"},
		{"bad header filled_at", mutate(func(i *CreateTemplateInput) { i.HeaderItems[0].FilledAt = "never" }), "header_items"},
		{"dup header tag", mutate(func(i *CreateTemplateInput) { i.HeaderItems = append(i.HeaderItems, i.HeaderItems[0]) }), "header_items"},
		{"block cycle", mutate(func(i *CreateTemplateInput) {
			i.Blocks[0].Predecessor = "Ejecucion"
			i.Blocks[1].Predecessor = "Preparacion"
		}), "blocks"},
		{"bad block responsible", mutate(func(i *CreateTemplateInput) { i.Blocks[0].ResponsibleType = "bot" }), "blocks"},
		{"bad block line type", mutate(func(i *CreateTemplateInput) { i.Blocks[0].Lines[0].Type = "blob" }), "blocks"},
		{"consumables when disabled", mutate(func(i *CreateTemplateInput) { i.ConsumablesEnabled = false }), "consumables"},
		{"unknown consumable", mutate(func(i *CreateTemplateInput) { i.Consumables[0].ConsumableID = "999" }), "consumables"},
		{"zero consumable qty", mutate(func(i *CreateTemplateInput) { i.Consumables[0].Quantity = 0 }), "consumables"},
		{"exceeds 25 commit appends", mutate(func(i *CreateTemplateInput) { i.Blocks = blocks25; i.Actions = nil }), "blocks"},
		{"unknown action", mutate(func(i *CreateTemplateInput) { i.Actions[0].ActionID = "999" }), "actions"},
		{"unknown action block ref", mutate(func(i *CreateTemplateInput) { i.Actions[0].BlockReference = "Inexistente" }), "actions"},
		{"bad reset policy", mutate(func(i *CreateTemplateInput) { i.Numbering.ResetPolicy = "hourly" }), "numbering"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateTemplate(ctx, tc.in)
			wantInvalid(t, err, tc.field)
		})
	}
}

func TestPublishTemplateVersion_AtomicVersioningAndGuards(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedTemplateWrite(fs)
	svc := NewService(fs)

	v1, err := svc.CreateTemplate(ctx, validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	fs.entities[TemplateEntityType][v1.ID]["_version"] = int64(1)

	nextIn := validTemplateInput()
	nextIn.Description = "Version 2 mejorada"
	v2, err := svc.PublishTemplateVersion(ctx, PublishTemplateVersionInput{
		PreviousTemplateID:  ID(v1.ID),
		CreateTemplateInput: nextIn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if v2.ID == "" || v2.ID == v1.ID || v2.Version != 2 || !v2.Latest || v2.Description != "Version 2 mejorada" {
		t.Fatalf("v2 = %+v", v2)
	}
	if fs.commits != 1 {
		t.Fatalf("commits = %d, want 1 atomic commit", fs.commits)
	}
	if fs.entities[TemplateEntityType][v1.ID]["latest"] != false {
		t.Fatalf("v1 should have latest=false: %v", fs.entities[TemplateEntityType][v1.ID])
	}

	// ListTemplates returns only v2 by default, and both with IncludeOldVersions.
	latestOnly, err := svc.ListTemplates(ctx, ListTemplatesInput{})
	if err != nil || len(latestOnly.Templates) != 1 || latestOnly.Templates[0].ID != v2.ID {
		t.Fatalf("latest only = %+v err=%v", latestOnly, err)
	}
	all, err := svc.ListTemplates(ctx, ListTemplatesInput{IncludeOldVersions: true})
	if err != nil || len(all.Templates) != 2 {
		t.Fatalf("all = %+v err=%v", all, err)
	}

	// Publishing from v1 (already superseded) fails with not_latest.
	_, err = svc.PublishTemplateVersion(ctx, PublishTemplateVersionInput{
		PreviousTemplateID:  ID(v1.ID),
		CreateTemplateInput: nextIn,
	})
	wantRule(t, err, "not_latest")

	// Missing previous template -> NotFoundError.
	_, err = svc.PublishTemplateVersion(ctx, PublishTemplateVersionInput{
		PreviousTemplateID:  "9999",
		CreateTemplateInput: nextIn,
	})
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Kind != "work order template" {
		t.Fatalf("want NotFoundError(work order template), got %T %v", err, err)
	}

	// Version conflict on commit -> stale_state.
	fs.entities[TemplateEntityType][v2.ID]["_version"] = int64(1)
	fs.commitErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "version conflict"}
	_, err = svc.PublishTemplateVersion(ctx, PublishTemplateVersionInput{
		PreviousTemplateID:  ID(v2.ID),
		CreateTemplateInput: nextIn,
	})
	wantRule(t, err, "stale_state")
}

func TestOrderServer_TemplateWrite(t *testing.T) {
	fs := newFake()
	seedTemplateWrite(fs)
	srv := newTestServer(fs)
	defer srv.Close()

	resp, out := post(t, srv.URL+"/api/v1/orders/templates/create", testToken,
		`{"name":"Basica","workflow_states":["Abierta","Cerrada"]}`)
	if resp.StatusCode != 200 || out["version"] != 1.0 || out["latest"] != true {
		t.Fatalf("create template: %d %v", resp.StatusCode, out)
	}
	v1ID := out["id"].(string)
	fs.entities[TemplateEntityType][v1ID]["_version"] = int64(1)

	resp, out = post(t, srv.URL+"/api/v1/orders/templates/publish-version", testToken,
		`{"previous_template_id":"`+v1ID+`","name":"Basica","workflow_states":["Abierta","Revision","Cerrada"]}`)
	if resp.StatusCode != 200 || out["version"] != 2.0 || out["latest"] != true {
		t.Fatalf("publish version: %d %v", resp.StatusCode, out)
	}

	// Publishing again from v1 -> 409 not_latest.
	resp, out = post(t, srv.URL+"/api/v1/orders/templates/publish-version", testToken,
		`{"previous_template_id":"`+v1ID+`","name":"Basica","workflow_states":["Abierta","Cerrada"]}`)
	if resp.StatusCode != 409 || out["code"] != "XOLU-INVALID_TRANSITION" || out["details"].(map[string]any)["rule"] != "not_latest" {
		t.Fatalf("not_latest: %d %v", resp.StatusCode, out)
	}

	// Bad request / unauthorized.
	resp, _ = post(t, srv.URL+"/api/v1/orders/templates/create", testToken, `{"name":"X","workflow_states":["Solo"]}`)
	if resp.StatusCode != 400 {
		t.Errorf("bad template: %d", resp.StatusCode)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/templates/create", "", `{}`)
	if resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}

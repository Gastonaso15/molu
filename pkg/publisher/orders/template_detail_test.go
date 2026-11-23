package orders

import (
	"context"
	"errors"
	"testing"
)

func seedRichTemplate(fs *fakeSubstrate) {
	fs.put(TemplateEntityType, "9", map[string]any{
		"name": "Mantenimiento", "description": "Preventivo", "version": 2, "latest": true,
		"asset_type_id": 5, "min_assets": 1, "max_assets": 1,
		"color": "blue", "icon": "build",
		"prefix": "MNT-", "suffix": "/26", "sequence_length": 4, "reset_policy": "yearly",
		"workflow_states": []any{map[string]any{"name": "Pendiente"}, map[string]any{"name": "Finalizada"}},
		"header_items": []any{
			map[string]any{"tag": "prioridad", "type": "number", "default_value": "1", "to_fill_out": "upon_creating", "required": true},
		},
		"blocks": []any{
			map[string]any{"name": "Preparacion", "predecessor": "", "responsible_type": "user",
				"lines": []any{map[string]any{"name": "equipo_aislado", "type": "boolean"}}},
			map[string]any{"name": "Mantenimiento", "predecessor": "Preparacion", "responsible_type": "crew",
				"lines": []any{map[string]any{"name": "filtro_cambiado", "type": "boolean"}, map[string]any{"name": "horas", "type": "number"}}},
		},
		"consumables_allowed": true, "add_any_consumables_allowed": true,
		"consumables": []any{
			map[string]any{"consumable_id": 1, "quantity": 1},
			map[string]any{"consumable_id": 2, "quantity": 0.5},
		},
		"actions": []any{
			map[string]any{"action": 3, "trigger_point": "after_block", "block_reference": "Mantenimiento",
				"mapping_values": []any{
					map[string]any{"parameter": "readings", "source": "context", "context_path": "block:Mantenimiento:line:horas", "transform": "wrap_in_array"},
					map[string]any{"parameter": "labels", "source": "fixed", "fixed_value": []any{"a", "b"}},
				}},
		},
		"rule_item": []any{
			map[string]any{"name": "Prioridad alta", "trigger": "on_evaluate_header", "action_type": "set_mandatory",
				"target_field": "header:justificacion", "message_type": "WARNING", "message_text": "Justificar",
				"conditions": []any{map[string]any{"field": "header:prioridad", "operator": "greater_than", "comparison_value": "3"}}},
		},
	})
	fs.put("consumable", "1", map[string]any{"name": "Filtro", "enabled": true})
	fs.put("consumable", "2", map[string]any{"name": "Kit de limpieza", "enabled": true})
	fs.put("wo_actions", "3", map[string]any{"name": "Registrar mediciones", "code": "register_measurement_series", "enabled": true})
}

func findTemplate(t *testing.T, l TemplateList, id string) Template {
	t.Helper()
	for _, tp := range l.Templates {
		if tp.ID == id {
			return tp
		}
	}
	t.Fatalf("template %s not listed: %+v", id, l)
	return Template{}
}

func checkRichTemplate(t *testing.T, tp Template) {
	t.Helper()
	if !tp.Supported || tp.Reason != "" {
		t.Errorf("blocks + consumables should now be openable: %+v", tp)
	}
	if len(tp.Blocks) != 2 || tp.Blocks[1].Name != "Mantenimiento" || tp.Blocks[1].Predecessor != "Preparacion" ||
		tp.Blocks[1].ResponsibleType != "crew" || len(tp.Blocks[1].Lines) != 2 || tp.Blocks[1].Lines[1].Type != "number" {
		t.Errorf("blocks = %+v", tp.Blocks)
	}
	if !tp.ConsumablesEnabled || !tp.ExtraConsumablesAllowed || len(tp.Consumables) != 2 ||
		tp.Consumables[0].ConsumableID != "1" || tp.Consumables[1].Quantity != 0.5 {
		t.Errorf("consumables = %+v enabled=%v extra=%v", tp.Consumables, tp.ConsumablesEnabled, tp.ExtraConsumablesAllowed)
	}
	if len(tp.Actions) != 1 || tp.Actions[0].ActionID != "3" || tp.Actions[0].TriggerPoint != "after_block" ||
		tp.Actions[0].BlockReference != "Mantenimiento" || len(tp.Actions[0].MappingValues) != 2 ||
		tp.Actions[0].MappingValues[0].ContextPath != "block:Mantenimiento:line:horas" ||
		tp.Actions[0].MappingValues[0].Transform != "wrap_in_array" || tp.Actions[0].MappingValues[1].FixedValue == nil {
		t.Errorf("actions = %+v", tp.Actions)
	}
	if len(tp.Rules) != 1 || tp.Rules[0]["name"] != "Prioridad alta" {
		t.Errorf("rules = %+v", tp.Rules)
	}
	if tp.Numbering == nil || tp.Numbering.Prefix != "MNT-" || tp.Numbering.Suffix != "/26" ||
		tp.Numbering.SequenceLength != 4 || tp.Numbering.ResetPolicy != "yearly" {
		t.Errorf("numbering = %+v", tp.Numbering)
	}
}

func TestListTemplates_IncludesBlocksConsumablesActionsRules(t *testing.T) {
	fs := newFake()
	seedRichTemplate(fs)
	out, err := NewService(fs).ListTemplates(context.Background(), ListTemplatesInput{})
	if err != nil {
		t.Fatal(err)
	}
	tp := findTemplate(t, out, "9")
	checkRichTemplate(t, tp)
	// The list does not resolve names: that costs one read per reference.
	if tp.Consumables[0].Name != "" || tp.Actions[0].Name != "" {
		t.Errorf("list should not resolve names: %+v %+v", tp.Consumables, tp.Actions)
	}
}

func TestListTemplates_PlainTemplateHasNoExtras(t *testing.T) {
	fs := newFake()
	seedWorld(fs)
	out, err := NewService(fs).ListTemplates(context.Background(), ListTemplatesInput{})
	if err != nil {
		t.Fatal(err)
	}
	tp := findTemplate(t, out, "1")
	if !tp.Supported || len(tp.Blocks) != 0 || len(tp.Consumables) != 0 || len(tp.Actions) != 0 || len(tp.Rules) != 0 || tp.Numbering != nil {
		t.Errorf("plain template = %+v", tp)
	}
}

func TestGetTemplate_ResolvesNames(t *testing.T) {
	fs := newFake()
	seedRichTemplate(fs)
	tp, err := NewService(fs).GetTemplate(context.Background(), GetTemplateInput{TemplateID: "9"})
	if err != nil {
		t.Fatal(err)
	}
	checkRichTemplate(t, tp)
	if tp.Consumables[0].Name != "Filtro" || tp.Consumables[1].Name != "Kit de limpieza" || tp.Actions[0].Name != "Registrar mediciones" {
		t.Errorf("names: %+v %+v", tp.Consumables, tp.Actions)
	}
}

func TestGetTemplate_ToleratesUnreadableReferences(t *testing.T) {
	fs := newFake()
	seedRichTemplate(fs)
	delete(fs.entities["consumable"], "2")
	delete(fs.entities, "wo_actions")
	tp, err := NewService(fs).GetTemplate(context.Background(), GetTemplateInput{TemplateID: "9"})
	if err != nil {
		t.Fatal(err)
	}
	if tp.Consumables[0].Name != "Filtro" || tp.Consumables[1].Name != "" || tp.Actions[0].Name != "" {
		t.Errorf("names: %+v %+v", tp.Consumables, tp.Actions)
	}
}

func TestGetTemplate_NotFoundAndValidation(t *testing.T) {
	svc := NewService(newFake())
	_, err := svc.GetTemplate(context.Background(), GetTemplateInput{TemplateID: "404"})
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Error() != `work order template "404" not found` {
		t.Fatalf("err = %T %v", err, err)
	}
	for _, id := range []ID{"", "abc", "0", "-3"} {
		_, err := svc.GetTemplate(context.Background(), GetTemplateInput{TemplateID: id})
		wantInvalid(t, err, "template_id")
	}
}

func TestGetTemplate_SubstrateDown(t *testing.T) {
	fs := newFake()
	fs.failWith = &XoluError{Status: 503, Message: "down"}
	_, err := NewService(fs).GetTemplate(context.Background(), GetTemplateInput{TemplateID: "9"})
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %T %v", err, err)
	}
}

func TestOrderServer_GetTemplate(t *testing.T) {
	fs := newFake()
	seedRichTemplate(fs)
	srv := newTestServer(fs)
	defer srv.Close()
	resp, out := post(t, srv.URL+"/api/v1/orders/template", testToken, `{"template_id":9}`)
	if resp.StatusCode != 200 || out["name"] != "Mantenimiento" {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	cons := out["consumables"].([]any)[0].(map[string]any)
	if cons["name"] != "Filtro" || cons["consumable_id"] != "1" {
		t.Errorf("consumable = %v", cons)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/template", testToken, `{"template_id":404}`)
	if resp.StatusCode != 404 {
		t.Errorf("missing: %d", resp.StatusCode)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/template", testToken, `{}`)
	if resp.StatusCode != 400 {
		t.Errorf("no id: %d", resp.StatusCode)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/template", "", `{"template_id":9}`)
	if resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}

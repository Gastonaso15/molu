package orders

import (
	"context"
	"strconv"
	"strings"
)

const (
	consumableEntity = "consumable"
	woActionEntity   = "wo_actions"
)

// fillTemplateDetail reads the parts of a template document that describe how
// an order from it is run: blocks, consumables, actions, rules and numbering.
func fillTemplateDetail(t *Template, doc map[string]any) error {
	var blocks []struct {
		Name            string              `json:"name"`
		ResponsibleType string              `json:"responsible_type"`
		Predecessor     string              `json:"predecessor"`
		Lines           []TemplateBlockLine `json:"lines"`
	}
	if err := roundTrip(doc["blocks"], &blocks); err != nil {
		return err
	}
	for _, b := range blocks {
		lines := b.Lines
		if lines == nil {
			lines = []TemplateBlockLine{}
		}
		t.Blocks = append(t.Blocks, TemplateBlock{Name: b.Name, ResponsibleType: b.ResponsibleType, Predecessor: b.Predecessor, Lines: lines})
	}

	t.ConsumablesEnabled, _ = doc["consumables_allowed"].(bool)
	t.ExtraConsumablesAllowed, _ = doc["add_any_consumables_allowed"].(bool)
	var cons []map[string]any
	if err := roundTrip(doc["consumables"], &cons); err != nil {
		return err
	}
	for _, c := range cons {
		q, _ := strconv.ParseFloat(idString(c["quantity"]), 64)
		if f, ok := c["quantity"].(float64); ok {
			q = f
		}
		t.Consumables = append(t.Consumables, TemplateConsumable{ConsumableID: idString(c["consumable_id"]), Quantity: q})
	}

	var actions []struct {
		Action         any             `json:"action"`
		TriggerPoint   string          `json:"trigger_point"`
		BlockReference string          `json:"block_reference"`
		MappingValues  []ActionMapping `json:"mapping_values"`
	}
	if err := roundTrip(doc["actions"], &actions); err != nil {
		return err
	}
	for _, a := range actions {
		t.Actions = append(t.Actions, TemplateAction{
			ActionID: idString(a.Action), TriggerPoint: a.TriggerPoint, BlockReference: a.BlockReference, MappingValues: a.MappingValues,
		})
	}

	if err := roundTrip(doc["rule_item"], &t.Rules); err != nil {
		return err
	}

	n := Numbering{SequenceLength: int(intOf(doc["sequence_length"]))}
	n.Prefix, _ = doc["prefix"].(string)
	n.Suffix, _ = doc["suffix"].(string)
	n.ResetPolicy, _ = doc["reset_policy"].(string)
	if n != (Numbering{}) {
		t.Numbering = &n
	}
	return nil
}

// GetTemplate returns one template in full, with the names of its consumables
// and actions. A name that cannot be read (the record is gone, or its entity
// does not exist yet) is left empty: the template is still readable.
func (s *Service) GetTemplate(ctx context.Context, in GetTemplateInput) (Template, error) {
	id := strings.TrimSpace(string(in.TemplateID))
	if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 {
		return Template{}, &ValidationError{Fields: []string{"template_id"}, Reason: "must be a positive id"}
	}
	doc, err := s.sub.GetEntity(ctx, TemplateEntityType, id)
	if err != nil {
		if isMissing(err) {
			return Template{}, &NotFoundError{ID: id, Kind: "work order template"}
		}
		return Template{}, classify(err, "")
	}
	tpl := templateFromRow(withID(doc, id))
	names := map[string]string{}
	nameOf := func(entity, ref string) string {
		key := entity + "/" + ref
		if name, ok := names[key]; ok {
			return name
		}
		var name string
		if ref != "" {
			if d, err := s.sub.GetEntity(ctx, entity, ref); err == nil {
				name, _ = d["name"].(string)
			}
		}
		names[key] = name
		return name
	}
	for i := range tpl.Consumables {
		tpl.Consumables[i].Name = nameOf(consumableEntity, tpl.Consumables[i].ConsumableID)
	}
	for i := range tpl.Actions {
		tpl.Actions[i].Name = nameOf(woActionEntity, tpl.Actions[i].ActionID)
	}
	return tpl, nil
}

func withID(doc map[string]any, id string) map[string]any {
	out := make(map[string]any, len(doc)+1)
	for k, v := range doc {
		out[k] = v
	}
	out["id"] = id
	return out
}

package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const unsupportedTemplateReason = "this template uses consumables (a mobile warehouse), which this publisher does not support yet"

const blocksNotAdvanceableReason = "orders with blocks or a mobile warehouse cannot be advanced from here yet"

// createTemplateOrder opens an order that follows a template's workflow. It
// writes the same work_orders document seam-CURE writes for such an order:
// no xolu FSM (machine_id 0), the workflow position in workflow_index, and the
// answers of the upon_creating header items in header_values.
func (s *Service) createTemplateOrder(ctx context.Context, in CreateInput) (WorkOrder, error) {
	tpl, err := s.loadTemplate(ctx, string(in.TemplateID))
	if err != nil {
		return WorkOrder{}, err
	}
	var problems []fieldProblem
	if in.TemplateVersion != 0 && in.TemplateVersion != tpl.Version {
		return WorkOrder{}, &ValidationError{Fields: []string{"template_version"},
			Reason: fmt.Sprintf("template %s is at version %d, not %d", tpl.ID, tpl.Version, in.TemplateVersion)}
	}
	if tpl.UsesConsumables {
		return WorkOrder{}, &ValidationError{Fields: []string{"template_id"}, Reason: unsupportedTemplateReason}
	}
	if err := tpl.validateWorkflow(); err != nil {
		return WorkOrder{}, &ValidationError{Fields: []string{"template_id"}, Reason: "template workflow is invalid: " + err.Error()}
	}

	// Like the form, the title and description start as the template's own.
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = tpl.Name
	}
	if title == "" {
		problems = append(problems, fieldProblem{"title", "required"})
	}
	description := in.Description
	if description == "" {
		description = tpl.Description
	}

	asg, p, err := s.resolveAssignment(ctx, in)
	if err != nil {
		return WorkOrder{}, err
	}
	problems = append(problems, p...)

	headers, p, err := s.headerValues(ctx, tpl.Headers, phaseCreating, in.HeaderValues, nil, true)
	if err != nil {
		return WorkOrder{}, err
	}
	problems = append(problems, p...)

	assets, p, err := s.assetSelection(ctx, tpl, in.AssetIDs)
	if err != nil {
		return WorkOrder{}, err
	}
	problems = append(problems, p...)

	if err := validationFrom(problems); err != nil {
		return WorkOrder{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	doc := map[string]any{
		"title":            title,
		"description":      description,
		"assigned_to":      asg.Name,
		"state":            tpl.States[0],
		"machine_id":       int64(0),
		"created_at":       now,
		"updated_at":       now,
		"block_values":     map[string]any{},
		"assignment_type":  asg.Type,
		"responsible_type": asg.ResponsibleType,
		"responsible_id":   asg.ResponsibleID,
		"workflow_mode":    workflowModeTmpl,
		"workflow_index":   0,
		"template_id":      intOf(tpl.ID),
		"template_version": tpl.Version,
		"header_values":    headers,
		"asset_ids":        assets,
	}
	var id string
	if len(tpl.Blocks) > 0 {
		id, err = s.createOrderWithBlocks(ctx, tpl, doc)
	} else {
		id, err = s.sub.CreateEntity(ctx, EntityType, doc)
		if err != nil {
			err = classify(err, "")
		}
	}
	if err != nil {
		return WorkOrder{}, err
	}
	return orderFromDoc(id, doc), nil
}

// advanceTemplateOrder moves a template order to the next state of its
// workflow, the way seam-CURE's detail page does: one entity update that
// stores the answers and the new state together, guarded by the entity's
// _version so a concurrent change is rejected instead of overwritten.
func (s *Service) advanceTemplateOrder(ctx context.Context, in AdvanceInput, doc map[string]any) (AdvanceResult, error) {
	state, _ := doc["state"].(string)
	if doc["assignment_type"] == AssignmentBoard && intOf(doc["responsible_id"]) == 0 {
		return AdvanceResult{}, rejection("awaiting_claim", "the order is on the board and nobody has claimed it yet", state)
	}
	if intOf(doc["block_count"]) > 0 || intOf(doc["mobile_warehouse_id"]) > 0 {
		return AdvanceResult{}, &ValidationError{Fields: []string{"order_id"}, Reason: blocksNotAdvanceableReason}
	}

	tpl, err := s.loadTemplate(ctx, idString(doc["template_id"]))
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return AdvanceResult{}, rejection("template_unavailable", "the order's template cannot be used: "+ve.Reason, state)
		}
		return AdvanceResult{}, err
	}
	if v := int(intOf(doc["template_version"])); v > 0 && v != tpl.Version {
		return AdvanceResult{}, rejection("template_version", fmt.Sprintf("the order was opened with template version %d, which is not the stored one (%d)", v, tpl.Version), state)
	}
	if err := tpl.validateWorkflow(); err != nil {
		return AdvanceResult{}, rejection("invalid_workflow", "the template workflow is invalid: "+err.Error(), state)
	}

	idx := int(intOf(doc["workflow_index"]))
	if idx < 0 || idx >= len(tpl.States) || tpl.States[idx] != state {
		return AdvanceResult{}, rejection("invalid_workflow", fmt.Sprintf("the order's state %q does not match its position in the template workflow", state), state)
	}
	if in.WorkflowIndex != nil && *in.WorkflowIndex != idx {
		return AdvanceResult{}, rejection("stale_state", fmt.Sprintf("the order is at step %d, not %d: read it again", idx, *in.WorkflowIndex), state)
	}
	last := len(tpl.States) - 1
	switch {
	case idx == last:
		return AdvanceResult{}, rejection("final_state", fmt.Sprintf("the order is already in its final state %q", state), state)
	case in.Input != InputComplete:
		return AdvanceResult{}, rejection("invalid_input", fmt.Sprintf("no transition for input %q from state %q: template workflows only accept %q", in.Input, state, InputComplete), state)
	}

	var base map[string]any
	if err := roundTrip(doc["header_values"], &base); err != nil {
		base = nil
	}
	headers, problems, err := s.headerValues(ctx, tpl.Headers, phaseExecuting, in.HeaderValues, base, false)
	if err != nil {
		return AdvanceResult{}, err
	}
	// The step that reaches the last state is the sign-off.
	signs := idx == last-1
	signature, _ := in.Payload["technician_signature"].(string)
	if signs && strings.TrimSpace(signature) == "" {
		problems = append(problems, fieldProblem{"payload.technician_signature", "required to complete the last step"})
	}
	if err := validationFrom(problems); err != nil {
		return AdvanceResult{}, err
	}

	next := tpl.States[idx+1]
	now := time.Now().UTC().Format(time.RFC3339)
	patch := map[string]any{
		"header_values":  headers,
		"updated_at":     now,
		"state":          next,
		"workflow_index": idx + 1,
	}
	if signs {
		patch["technician_signature"] = signature
	}
	if idx+1 == last {
		patch["completed_at"] = now
	}
	if v := intOf(doc["_version"]); v > 0 {
		patch["_version"] = v
	}
	if err := s.sub.PatchEntity(ctx, EntityType, in.OrderID, patch); err != nil {
		var xe *XoluError
		if errors.As(err, &xe) && xe.Status == 409 {
			return AdvanceResult{}, rejection("stale_state", "the order changed while it was being updated: read it again and retry", state)
		}
		return AdvanceResult{}, classify(err, in.OrderID)
	}
	for k, v := range patch {
		if k != "_version" {
			doc[k] = v
		}
	}
	return AdvanceResult{Order: orderFromDoc(in.OrderID, doc), Previous: state, Current: next}, nil
}

// templateOrder reports whether a stored order follows a template workflow.
func templateOrder(doc map[string]any) bool {
	mode, _ := doc["workflow_mode"].(string)
	return mode == workflowModeTmpl
}

func idList(v any) []string {
	var raw []any
	if err := roundTrip(v, &raw); err != nil {
		return nil
	}
	var out []string
	for _, x := range raw {
		if id := idString(x); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func formatIndex(doc map[string]any) *int {
	if _, ok := doc["workflow_index"]; !ok {
		return nil
	}
	n := int(intOf(doc["workflow_index"]))
	return &n
}

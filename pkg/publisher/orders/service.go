package orders

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// Service holds the work-order business logic. Lifecycle legality is not
// decided here: every transition is delegated to the xolu FSM, and its
// rejections travel back verbatim. Orders are stored the way seam-CURE stores
// them, so both sides read and write the same work_orders entity.
type Service struct {
	sub Substrate

	defMu sync.Mutex
	defID string
}

func NewService(sub Substrate) *Service { return &Service{sub: sub} }

// CreateOrder opens an order the way Seam does. With a template_id the order
// follows the template's workflow (see createTemplateOrder). Without one it is
// a legacy order: a fresh lifecycle machine first, then the work_orders
// document carrying the machine's initial state and its id, so Seam's own list
// and detail pages see it.
func (s *Service) CreateOrder(ctx context.Context, in CreateInput) (WorkOrder, error) {
	if in.TemplateID != "" {
		return s.createTemplateOrder(ctx, in)
	}
	var stray []fieldProblem
	if in.TemplateVersion != 0 {
		stray = append(stray, fieldProblem{"template_version", "only valid together with template_id"})
	}
	if len(in.HeaderValues) > 0 {
		stray = append(stray, fieldProblem{"header_values", "only valid together with template_id"})
	}
	if len(in.AssetIDs) > 0 {
		stray = append(stray, fieldProblem{"asset_ids", "only valid together with template_id"})
	}
	if err := validationFrom(stray); err != nil {
		return WorkOrder{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return WorkOrder{}, &ValidationError{Fields: []string{"title"}}
	}
	var asg assignment
	assigned := in.AssignedTo
	if in.AssignmentType != "" || in.ResponsibleType != "" || in.ResponsibleID != "" {
		a, problems, err := s.resolveAssignment(ctx, in)
		if err != nil {
			return WorkOrder{}, err
		}
		if err := validationFrom(problems); err != nil {
			return WorkOrder{}, err
		}
		asg, assigned = a, a.Name
	}
	defID, err := s.definitionID(ctx)
	if err != nil {
		return WorkOrder{}, classify(err, "")
	}
	m, err := s.sub.CreateMachine(ctx, defID)
	if err != nil {
		return WorkOrder{}, classify(err, "")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	doc := map[string]any{
		"title":       title,
		"description": in.Description,
		"assigned_to": assigned,
		"state":       m.State,
		"machine_id":  numericOrString(m.ID),
		"created_at":  now,
		"updated_at":  now,
	}
	if asg.Type != "" {
		doc["assignment_type"] = asg.Type
		doc["responsible_type"] = asg.ResponsibleType
		doc["responsible_id"] = asg.ResponsibleID
	}
	id, err := s.sub.CreateEntity(ctx, EntityType, doc)
	if err != nil {
		return WorkOrder{}, classify(err, "")
	}
	return orderFromDoc(id, doc), nil
}

// GetOrder returns the order or NotFoundError.
func (s *Service) GetOrder(ctx context.Context, id string) (WorkOrder, error) {
	if strings.TrimSpace(id) == "" {
		return WorkOrder{}, &ValidationError{Fields: []string{"order_id"}}
	}
	doc, err := s.sub.GetEntity(ctx, EntityType, id)
	if err != nil {
		return WorkOrder{}, classify(err, id)
	}
	return orderFromDoc(id, doc), nil
}

// AdvanceOrder fires an FSM input on the order's machine and then copies the
// new state onto the order document, as Seam does. A rejection by xolu (no
// transition, or a guard that evaluated false) becomes a TransitionError whose
// message is xolu's own. Orders that follow a template workflow have no
// machine: they move through advanceTemplateOrder instead.
func (s *Service) AdvanceOrder(ctx context.Context, in AdvanceInput) (AdvanceResult, error) {
	var bad []string
	if strings.TrimSpace(in.OrderID) == "" {
		bad = append(bad, "order_id")
	}
	if strings.TrimSpace(in.Input) == "" {
		bad = append(bad, "input")
	}
	if len(bad) > 0 {
		return AdvanceResult{}, &ValidationError{Fields: bad}
	}

	doc, err := s.sub.GetEntity(ctx, EntityType, in.OrderID)
	if err != nil {
		return AdvanceResult{}, classify(err, in.OrderID)
	}
	if templateOrder(doc) {
		return s.advanceTemplateOrder(ctx, in, doc)
	}
	if len(in.HeaderValues) > 0 || in.WorkflowIndex != nil {
		return AdvanceResult{}, &ValidationError{Fields: []string{"header_values", "workflow_index"},
			Reason: "only valid for orders that follow a template workflow"}
	}
	machineID := idString(doc["machine_id"])
	if machineID == "" || machineID == "0" {
		return AdvanceResult{}, &ValidationError{Fields: []string{"order_id"}, Reason: "the order has no workflow machine to advance"}
	}

	wr, err := s.sub.Walk(ctx, machineID, in.Input, in.Payload)
	if err != nil {
		return AdvanceResult{}, classify(err, in.OrderID)
	}

	patch := map[string]any{"state": wr.Current, "updated_at": time.Now().UTC().Format(time.RFC3339)}
	if sig, ok := in.Payload["technician_signature"]; ok {
		patch["technician_signature"] = sig
	}
	if err := s.sub.PatchEntity(ctx, EntityType, in.OrderID, patch); err != nil {
		// The machine moved but the document did not: surface it, do not hide the drift.
		return AdvanceResult{}, classify(err, in.OrderID)
	}
	for k, v := range patch {
		doc[k] = v
	}
	return AdvanceResult{Order: orderFromDoc(in.OrderID, doc), Previous: wr.Previous, Current: wr.Current, Outputs: wr.Outputs}, nil
}

// ListTemplates returns the work order templates (read-only). Only the latest
// version of each is listed unless IncludeOldVersions is set. xolu answers
// 400 XOLU-QL004 "does not exist" until the first template is created (the
// same case seam-CURE's entityNotYetCreated handles): that is an empty list.
func (s *Service) ListTemplates(ctx context.Context, in ListTemplatesInput) (TemplateList, error) {
	rows, err := s.sub.ListEntities(ctx, TemplateEntityType)
	if err != nil {
		var xe *XoluError
		if errors.As(err, &xe) && xe.Code == "XOLU-QL004" && strings.Contains(xe.Message, "does not exist") {
			return TemplateList{Templates: []Template{}}, nil
		}
		return TemplateList{}, classify(err, "")
	}
	out := TemplateList{Templates: []Template{}}
	for _, row := range rows {
		t := templateFromRow(row)
		if !in.IncludeOldVersions && !t.Latest {
			continue
		}
		out.Templates = append(out.Templates, t)
	}
	return out, nil
}

func orderFromDoc(id string, doc map[string]any) WorkOrder {
	state, _ := doc["state"].(string)
	o := WorkOrder{ID: id, State: state}
	o.Title, _ = doc["title"].(string)
	o.Description, _ = doc["description"].(string)
	o.AssignedTo, _ = doc["assigned_to"].(string)
	o.AssignmentType, _ = doc["assignment_type"].(string)
	o.ResponsibleType, _ = doc["responsible_type"].(string)
	if rid := idString(doc["responsible_id"]); rid != "" && rid != "0" {
		o.ResponsibleID = rid
	}
	o.CompletedAt, _ = doc["completed_at"].(string)
	if templateOrder(doc) {
		o.TemplateID = idString(doc["template_id"])
		o.TemplateVersion = int(intOf(doc["template_version"]))
		o.WorkflowIndex = formatIndex(doc)
		if hv, ok := doc["header_values"].(map[string]any); ok && len(hv) > 0 {
			o.HeaderValues = hv
		}
		o.AssetIDs = idList(doc["asset_ids"])
		o.Terminal = o.CompletedAt != ""
	} else {
		if tid := idString(doc["template_id"]); tid != "" && tid != "0" {
			o.TemplateID = tid
		}
		o.Terminal = state == StateCompleted
	}
	return o
}

func templateFromRow(row map[string]any) Template {
	id := idString(row["id"])
	t := Template{ID: id, WorkflowStates: []string{}, HeaderItems: []TemplateHeaderItem{}, Supported: true}
	t.Name, _ = row["name"].(string)
	t.Description, _ = row["description"].(string)
	t.Latest, _ = row["latest"].(bool)
	t.Version = int(intOf(row["version"]))
	if a := idString(row["asset_type_id"]); a != "" && a != "0" {
		t.AssetTypeID = a
	}
	t.MinAssets = int(intOf(row["min_assets"]))
	t.MaxAssets = int(intOf(row["max_assets"]))
	if err := fillTemplateDetail(&t, row); err != nil {
		t.Supported, t.Reason = false, "the template is malformed: "+err.Error()
		return t
	}
	if parsed, err := parseTemplateDoc(id, row); err == nil {
		for _, st := range parsed.States {
			if st != "" {
				t.WorkflowStates = append(t.WorkflowStates, st)
			}
		}
		for _, h := range parsed.Headers {
			t.HeaderItems = append(t.HeaderItems, TemplateHeaderItem{Tag: h.Tag, Type: h.Type, FilledAt: h.ToFillOut, Required: h.Required, DefaultValue: h.DefaultValue})
		}
		switch {
		case parsed.HasBlocks || parsed.UsesConsumables:
			t.Supported, t.Reason = false, unsupportedTemplateReason
		case parsed.validateWorkflow() != nil:
			t.Supported, t.Reason = false, "the template workflow is invalid: "+parsed.validateWorkflow().Error()
		}
	} else {
		t.Supported, t.Reason = false, "the template is malformed: "+err.Error()
	}
	return t
}

// definitionID resolves the lifecycle definition once. A failed attempt is not
// cached, so a later call retries.
func (s *Service) definitionID(ctx context.Context) (string, error) {
	s.defMu.Lock()
	defer s.defMu.Unlock()
	if s.defID != "" {
		return s.defID, nil
	}
	id, err := s.sub.EnsureDefinition(ctx, DefinitionName, LifecycleDefinition())
	if err != nil {
		return "", err
	}
	s.defID = id
	return id, nil
}

// classify turns raw substrate failures into the service's typed errors.
func classify(err error, orderID string) error {
	var xe *XoluError
	if !errors.As(err, &xe) {
		return &UnavailableError{Cause: err}
	}
	switch {
	case xe.Status == 404:
		return &NotFoundError{ID: orderID}
	case xe.Code == "XOLU-FSM003" || xe.Code == "XOLU-FSM004":
		details := map[string]any{"xolu_code": xe.Code}
		if xe.Detail != "" {
			details["detail"] = xe.Detail
		}
		if xe.State != "" {
			details["state"] = xe.State
		}
		if len(xe.AvailableInputs) > 0 {
			details["available_inputs"] = xe.AvailableInputs
		}
		return &TransitionError{Code: "XOLU-INVALID_TRANSITION", Message: xe.Message, Details: details}
	case xe.Status >= 500:
		return &UnavailableError{Cause: err}
	}
	return err
}

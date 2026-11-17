package orders

import (
	"context"
	"errors"
	"strconv"
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

// CreateOrder opens a legacy (template-less) order the way Seam does: a fresh
// lifecycle machine first, then the work_orders document carrying the
// machine's initial state and its id, so Seam's own list and detail pages see it.
func (s *Service) CreateOrder(ctx context.Context, in CreateInput) (WorkOrder, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return WorkOrder{}, &ValidationError{Fields: []string{"title"}}
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
		"assigned_to": in.AssignedTo,
		"state":       m.State,
		"machine_id":  numericOrString(m.ID),
		"created_at":  now,
		"updated_at":  now,
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
// machine and are not supported yet.
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
	machineID := idString(doc["machine_id"])
	if machineID == "" || machineID == "0" {
		return AdvanceResult{}, &ValidationError{
			Fields: []string{"order_id"},
			Reason: "order follows a template workflow, which this publisher cannot advance yet",
		}
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
	o := WorkOrder{ID: id, State: state, Terminal: state == StateCompleted}
	o.Title, _ = doc["title"].(string)
	o.Description, _ = doc["description"].(string)
	o.AssignedTo, _ = doc["assigned_to"].(string)
	if tid := idString(doc["template_id"]); tid != "" && tid != "0" {
		o.TemplateID = tid
	}
	return o
}

func templateFromRow(row map[string]any) Template {
	t := Template{ID: idString(row["id"]), WorkflowStates: []string{}}
	t.Name, _ = row["name"].(string)
	t.Description, _ = row["description"].(string)
	t.Latest, _ = row["latest"].(bool)
	if v := idString(row["version"]); v != "" {
		t.Version, _ = strconv.Atoi(v)
	}
	if a := idString(row["asset_type_id"]); a != "" && a != "0" {
		t.AssetTypeID = a
	}
	if states, ok := row["workflow_states"].([]any); ok {
		for _, st := range states {
			if m, ok := st.(map[string]any); ok {
				if n, _ := m["name"].(string); n != "" {
					t.WorkflowStates = append(t.WorkflowStates, n)
				}
			}
		}
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

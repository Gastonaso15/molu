package orders

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// Service holds the work-order business logic. Lifecycle legality is not
// decided here: every transition is delegated to the xolu FSM, and its
// rejections travel back verbatim.
type Service struct {
	sub Substrate

	defMu sync.Mutex
	defID string
}

func NewService(sub Substrate) *Service { return &Service{sub: sub} }

// CreateOrder opens a new order: a xolu entity bound to a fresh lifecycle machine.
func (s *Service) CreateOrder(ctx context.Context, in CreateInput) (WorkOrder, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return WorkOrder{}, &ValidationError{Fields: []string{"title"}}
	}
	defID, err := s.definitionID(ctx)
	if err != nil {
		return WorkOrder{}, classify(err, "")
	}
	entity := map[string]any{"title": title}
	if in.Description != "" {
		entity["description"] = in.Description
	}
	m, err := s.sub.CreateMachine(ctx, defID, EntityType, entity)
	if err != nil {
		return WorkOrder{}, classify(err, "")
	}
	return WorkOrder{ID: m.ID, Title: title, Description: in.Description, State: m.State, Terminal: m.Terminal || terminalStates[m.State]}, nil
}

// GetOrder returns the order or NotFoundError.
func (s *Service) GetOrder(ctx context.Context, id string) (WorkOrder, error) {
	if strings.TrimSpace(id) == "" {
		return WorkOrder{}, &ValidationError{Fields: []string{"order_id"}}
	}
	return s.load(ctx, id)
}

// AdvanceOrder fires an FSM input on the order's machine. A rejection by xolu
// (no transition, or a guard that evaluated false) becomes a TransitionError
// whose message is xolu's own.
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

	wr, err := s.sub.Walk(ctx, in.OrderID, in.Input, in.Payload)
	if err != nil {
		return AdvanceResult{}, classify(err, in.OrderID)
	}
	order, err := s.load(ctx, in.OrderID)
	if err != nil {
		return AdvanceResult{}, err
	}
	return AdvanceResult{Order: order, Previous: wr.Previous, Current: wr.Current, Outputs: wr.Outputs}, nil
}

// load joins the machine (state) with its entity document (title, description).
func (s *Service) load(ctx context.Context, id string) (WorkOrder, error) {
	m, err := s.sub.GetMachine(ctx, id)
	if err != nil {
		return WorkOrder{}, classify(err, id)
	}
	order := WorkOrder{ID: m.ID, State: m.State, Terminal: m.Terminal || terminalStates[m.State]}

	entityType, entityID, ok := strings.Cut(m.Ref, ":")
	if !ok {
		return order, nil
	}
	doc, err := s.sub.GetEntity(ctx, entityType, entityID)
	if err != nil {
		return WorkOrder{}, classify(err, id)
	}
	order.Title, _ = doc["title"].(string)
	order.Description, _ = doc["description"].(string)
	return order, nil
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

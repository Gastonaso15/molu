// Package orders implements the pilot Work Orders domain publisher: a service
// that owns work-order business logic, delegates lifecycle rules to a xolu
// FSM, and registers its functions in the Hub under the "work_orders" namespace.
package orders

import (
	"context"
	"fmt"
	"strings"
)

const (
	// Namespace prefixes every function published by this service.
	Namespace = "work_orders"
	// PublisherID identifies this publisher in the Hub catalog.
	PublisherID = "work-orders-publisher"
	// EntityType is the xolu entity that stores the order document.
	EntityType = "work_order"
	// DefinitionName is the xolu FSM definition that governs the order lifecycle.
	DefinitionName = "WorkOrderLifecycle"
	// TokenRef is the reference the Execution Bridge resolves to the bearer token.
	TokenRef = "orders:api-token"
)

// WorkOrder is the business view of an order: its document plus FSM state.
type WorkOrder struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	State       string `json:"state"`
	Terminal    bool   `json:"terminal"`
}

// CreateInput is the payload of work_orders.CreateOrder.
type CreateInput struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// AdvanceInput is the payload of work_orders.AdvanceOrder. Input is the FSM
// input to fire (for example "start" or "complete").
type AdvanceInput struct {
	OrderID string         `json:"order_id"`
	Input   string         `json:"input"`
	Payload map[string]any `json:"payload,omitempty"`
}

// AdvanceResult reports the transition that xolu committed.
type AdvanceResult struct {
	Order    WorkOrder `json:"order"`
	Previous string    `json:"previous"`
	Current  string    `json:"current"`
	Outputs  []string  `json:"outputs,omitempty"`
}

// Definition is an FSM definition document as xolu stores it.
type Definition map[string]any

// Machine is a running FSM instance bound to an entity.
type Machine struct {
	ID       string
	State    string
	Terminal bool
	Ref      string // "<entity_type>:<id>"
}

// WalkResult is the outcome of a committed transition.
type WalkResult struct {
	Previous string
	Current  string
	Terminal bool
	Outputs  []string
}

// Substrate is the slice of xolu the service needs. It is an interface so the
// business logic stays free of I/O (ADR-002) and is testable with a double.
type Substrate interface {
	// EnsureDefinition returns the id of the named definition, creating it
	// when it does not exist yet.
	EnsureDefinition(ctx context.Context, name string, def Definition) (string, error)
	// CreateMachine mints a machine from a definition bound to a new entity.
	CreateMachine(ctx context.Context, definitionID, entityType string, entity map[string]any) (Machine, error)
	GetMachine(ctx context.Context, id string) (Machine, error)
	GetEntity(ctx context.Context, entityType, id string) (map[string]any, error)
	Walk(ctx context.Context, machineID, input string, payload map[string]any) (WalkResult, error)
}

// XoluError is a non-success answer from xolu, kept raw so the service can
// classify it and preserve the diagnostic verbatim.
type XoluError struct {
	Status          int
	Code            string // e.g. "XOLU-FSM004"
	Message         string
	Detail          string
	State           string
	AvailableInputs []string
}

func (e *XoluError) Error() string {
	return fmt.Sprintf("xolu %d %s: %s", e.Status, e.Code, e.Message)
}

// NotFoundError means the order (machine or entity) does not exist.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("work order %q not found", e.ID) }

// TransitionError is an FSM rejection. Message carries xolu's own text
// untouched; the guard diagnostic lives in Details["detail"].
type TransitionError struct {
	Code    string // always "XOLU-INVALID_TRANSITION"
	Message string
	Details map[string]any
}

func (e *TransitionError) Error() string { return e.Code + ": " + e.Message }

// UnavailableError means xolu could not be reached or failed server-side.
type UnavailableError struct{ Cause error }

func (e *UnavailableError) Error() string {
	if e.Cause == nil {
		return "substrate unavailable"
	}
	return "substrate unavailable: " + e.Cause.Error()
}
func (e *UnavailableError) Unwrap() error { return e.Cause }

// ValidationError lists the request fields that broke the contract.
type ValidationError struct{ Fields []string }

func (e *ValidationError) Error() string {
	return "contract violation: invalid fields " + strings.Join(e.Fields, ", ")
}

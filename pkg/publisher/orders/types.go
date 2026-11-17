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
	// EntityType is the xolu entity Seam stores work orders in (plural, as in seam-CURE).
	EntityType = "work_orders"
	// TemplateEntityType is the xolu entity holding work order templates.
	TemplateEntityType = "work_order_templates"
	// DefinitionName is the xolu FSM definition Seam uses for template-less orders.
	DefinitionName = "seam_work_order_lifecycle"
	// TokenRef is the reference the Execution Bridge resolves to the bearer token.
	TokenRef = "orders:api-token"
)

// Lifecycle vocabulary of the legacy (template-less) work order.
const (
	StateDraft      = "draft"
	StateInProgress = "in_progress"
	StateCompleted  = "completed"

	InputStart    = "start"
	InputComplete = "complete"
)

// WorkOrder is the business view of an order, read from Seam's denormalized
// work_orders entity: the document plus the FSM state copied onto it.
type WorkOrder struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	AssignedTo  string `json:"assigned_to,omitempty"`
	State       string `json:"state"`
	Terminal    bool   `json:"terminal"`
	// TemplateID is set for orders that follow a template workflow. This
	// publisher can read them but not advance them yet.
	TemplateID string `json:"template_id,omitempty"`
}

// CreateInput is the payload of work_orders.CreateOrder.
type CreateInput struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	AssignedTo  string `json:"assigned_to,omitempty"`
}

// AdvanceInput is the payload of work_orders.AdvanceOrder. Input is the FSM
// input to fire ("start" or "complete"); "complete" needs
// payload.technician_signature.
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

// ListTemplatesInput is the payload of work_orders.ListTemplates. By default
// only the latest version of each template is returned.
type ListTemplatesInput struct {
	IncludeOldVersions bool `json:"include_old_versions,omitempty"`
}

// Template is the read-only summary of a work order template.
type Template struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	Version        int      `json:"version"`
	Latest         bool     `json:"latest"`
	AssetTypeID    string   `json:"asset_type_id,omitempty"`
	WorkflowStates []string `json:"workflow_states"`
}

// TemplateList is the result of work_orders.ListTemplates.
type TemplateList struct {
	Templates []Template `json:"templates"`
}

// Definition is an FSM definition document as xolu stores it.
type Definition map[string]any

// Machine is a running FSM instance.
type Machine struct {
	ID       string
	State    string
	Terminal bool
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
	// CreateMachine mints a machine from a definition.
	CreateMachine(ctx context.Context, definitionID string) (Machine, error)
	// CreateEntity stores a document and returns its id.
	CreateEntity(ctx context.Context, entityType string, data map[string]any) (string, error)
	GetEntity(ctx context.Context, entityType, id string) (map[string]any, error)
	// PatchEntity merges fields into an existing document.
	PatchEntity(ctx context.Context, entityType, id string, data map[string]any) error
	Walk(ctx context.Context, machineID, input string, payload map[string]any) (WalkResult, error)
	// ListEntities returns every document of the entity type (newest first),
	// each with its "id".
	ListEntities(ctx context.Context, entityType string) ([]map[string]any, error)
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

// ValidationError lists the request fields that broke the contract. Reason,
// when set, explains why a well-formed request cannot be honoured.
type ValidationError struct {
	Fields []string
	Reason string
}

func (e *ValidationError) Error() string {
	msg := "contract violation: invalid fields " + strings.Join(e.Fields, ", ")
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

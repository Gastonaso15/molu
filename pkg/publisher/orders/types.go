// Package orders implements the pilot Work Orders domain publisher: a service
// that owns work-order business logic, delegates lifecycle rules to a xolu
// FSM, and registers its functions in the Hub under the "work_orders" namespace.
package orders

import (
	"bytes"
	"context"
	"encoding/json"
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

// ID is a xolu entity id. It is sent as a string everywhere, but clients that
// send a JSON number are accepted too.
type ID string

func (i *ID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*i = ID(strings.TrimSpace(s))
		return nil
	}
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&n); err != nil {
		return fmt.Errorf("an id must be a string or a number")
	}
	*i = ID(n.String())
	return nil
}

// WorkOrder is the business view of an order, read from Seam's denormalized
// work_orders entity: the document plus the FSM state copied onto it.
type WorkOrder struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	AssignedTo  string `json:"assigned_to,omitempty"`
	State       string `json:"state"`
	Terminal    bool   `json:"terminal"`

	AssignmentType  string `json:"assignment_type,omitempty"`
	ResponsibleType string `json:"responsible_type,omitempty"`
	ResponsibleID   string `json:"responsible_id,omitempty"`

	// Set for orders that follow a template workflow (no xolu FSM): the
	// position in the template's list of states, and the header answers.
	TemplateID      string         `json:"template_id,omitempty"`
	TemplateVersion int            `json:"template_version,omitempty"`
	WorkflowIndex   *int           `json:"workflow_index,omitempty"`
	HeaderValues    map[string]any `json:"header_values,omitempty"`
	AssetIDs        []string       `json:"asset_ids,omitempty"`
	CompletedAt     string         `json:"completed_at,omitempty"`
}

// CreateInput is the payload of work_orders.CreateOrder. Without TemplateID it
// opens a legacy order driven by a xolu FSM. With TemplateID the order follows
// the template's workflow, and the assignment, header answers and assets of
// the template apply.
type CreateInput struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	AssignedTo  string `json:"assigned_to,omitempty"`

	TemplateID      ID             `json:"template_id,omitempty"`
	TemplateVersion int            `json:"template_version,omitempty"`
	AssignmentType  string         `json:"assignment_type,omitempty"`  // direct_assignment | board_posting
	ResponsibleType string         `json:"responsible_type,omitempty"` // user | crew | third_party
	ResponsibleID   ID             `json:"responsible_id,omitempty"`
	HeaderValues    map[string]any `json:"header_values,omitempty"` // answers by header tag (upon_creating items)
	AssetIDs        []ID           `json:"asset_ids,omitempty"`
}

// AdvanceInput is the payload of work_orders.AdvanceOrder.
//
// Legacy orders: Input is the FSM input to fire ("start" or "complete");
// "complete" needs payload.technician_signature.
//
// Template orders: the only input is "complete", which moves the order to the
// next state of the template. HeaderValues carries the answers of the
// upon_executing header items; the step that reaches the last state also
// needs payload.technician_signature. WorkflowIndex, when set, is the step
// the caller believes the order is at: a mismatch is rejected as stale.
type AdvanceInput struct {
	OrderID       string         `json:"order_id"`
	Input         string         `json:"input"`
	Payload       map[string]any `json:"payload,omitempty"`
	HeaderValues  map[string]any `json:"header_values,omitempty"`
	WorkflowIndex *int           `json:"workflow_index,omitempty"`
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

// TemplateHeaderItem describes one header answer a template asks for.
type TemplateHeaderItem struct {
	Tag          string `json:"tag"`
	Type         string `json:"type"` // text | number | boolean | date | asset | gps
	FilledAt     string `json:"filled_at"`
	Required     bool   `json:"required"`
	DefaultValue string `json:"default_value,omitempty"`
}

// Template is the read-only summary of a work order template: what is needed
// to open an order from it with work_orders.CreateOrder.
type Template struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Description    string               `json:"description,omitempty"`
	Version        int                  `json:"version"`
	Latest         bool                 `json:"latest"`
	AssetTypeID    string               `json:"asset_type_id,omitempty"`
	MinAssets      int                  `json:"min_assets,omitempty"`
	MaxAssets      int                  `json:"max_assets,omitempty"`
	WorkflowStates []string             `json:"workflow_states"`
	HeaderItems    []TemplateHeaderItem `json:"header_items"`
	// Supported is false for templates this publisher cannot open or advance
	// yet (blocks, mobile warehouse); Reason says why.
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
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
// when set, explains why: for per-field problems it reads "field: why; ...".
type ValidationError struct {
	Fields []string
	Reason string
	// perField is set when Reason already names every field.
	perField bool
}

func (e *ValidationError) Error() string {
	if e.perField {
		return "contract violation: " + e.Reason
	}
	msg := "contract violation: invalid fields " + strings.Join(e.Fields, ", ")
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

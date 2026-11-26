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
	// InputAdvance is the only input of a template order's own workflow machine.
	InputAdvance = "advance"
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
	TemplateID        string         `json:"template_id,omitempty"`
	TemplateVersion   int            `json:"template_version,omitempty"`
	WorkflowIndex     *int           `json:"workflow_index,omitempty"`
	HeaderValues      map[string]any `json:"header_values,omitempty"`
	AssetIDs          []string       `json:"asset_ids,omitempty"`
	MobileWarehouseID string         `json:"mobile_warehouse_id,omitempty"`
	CompletedAt       string         `json:"completed_at,omitempty"`
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
	CreatedByUserID ID             `json:"created_by_user_id,omitempty"` // optional: sets responsible_user_id on a mobile warehouse
	// BlockResponsibles names who runs each block, by block name: the id of a
	// user, crew or third party according to the block's responsible_type in
	// the template. Every block of the template needs one.
	BlockResponsibles map[string]ID `json:"block_responsibles,omitempty"`
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
	OrderID      string         `json:"order_id"`
	Input        string         `json:"input"`
	Payload      map[string]any `json:"payload,omitempty"`
	HeaderValues map[string]any `json:"header_values,omitempty"`
	// BlockValues answers the lines of the template's blocks, by block name and
	// then line name: {"Preparacion": {"aislado": true}}. Optional; an answer
	// that is not given (or is blank) keeps the one already stored.
	BlockValues   map[string]any `json:"block_values,omitempty"`
	WorkflowIndex *int           `json:"workflow_index,omitempty"`
	// UserID is the user who advances an order that runs blocks (it has its own
	// workflow machine): it must be the order's responsible. Not used for
	// legacy orders.
	UserID ID `json:"user_id,omitempty"`
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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     int    `json:"version"`
	Latest      bool   `json:"latest"`
	// Enabled is false when the template was disabled with SetTemplateEnabled:
	// CreateOrder refuses it. A document without the field reads as enabled.
	Enabled        bool                 `json:"enabled"`
	AssetTypeID    string               `json:"asset_type_id,omitempty"`
	MinAssets      int                  `json:"min_assets,omitempty"`
	MaxAssets      int                  `json:"max_assets,omitempty"`
	WorkflowStates []string             `json:"workflow_states"`
	HeaderItems    []TemplateHeaderItem `json:"header_items"`

	// Blocks are the template's units of work, run in the order their
	// predecessors allow; each has its own lines (fields) to fill in.
	Blocks []TemplateBlock `json:"blocks,omitempty"`
	// ConsumablesEnabled says orders from this template get a mobile
	// warehouse; Consumables are the supplies it brings by default, and
	// ExtraConsumablesAllowed lets the person add any other consumable.
	ConsumablesEnabled      bool                 `json:"consumables_enabled,omitempty"`
	ExtraConsumablesAllowed bool                 `json:"extra_consumables_allowed,omitempty"`
	Consumables             []TemplateConsumable `json:"consumables,omitempty"`
	// Actions and Rules are configuration: the work order module stores them
	// but does not run them yet.
	Actions   []TemplateAction `json:"actions,omitempty"`
	Rules     []map[string]any `json:"rules,omitempty"`
	Numbering *Numbering       `json:"numbering,omitempty"`

	// Supported is false for templates this publisher cannot open or advance
	// yet (blocks, mobile warehouse); Reason says why.
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// TemplateBlock is one block of a template. Predecessor is the name of the
// block that must finish first ("" for none).
type TemplateBlock struct {
	Name            string              `json:"name"`
	ResponsibleType string              `json:"responsible_type,omitempty"`
	Predecessor     string              `json:"predecessor,omitempty"`
	Lines           []TemplateBlockLine `json:"lines"`
}

// TemplateBlockLine is a field of a block, filled in when the order runs.
type TemplateBlockLine struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// TemplateConsumable is a supply an order brings by default. Name is filled
// by GetTemplate only.
type TemplateConsumable struct {
	ConsumableID string  `json:"consumable_id"`
	Name         string  `json:"name,omitempty"`
	Quantity     float64 `json:"quantity"`
}

// TemplateAction links a catalog action to a point of the order. TriggerPoint
// says when (for example before_work_order or after_block) and MappingValues
// where each parameter of the action takes its value from. Name is filled by
// GetTemplate only.
type TemplateAction struct {
	ActionID       string          `json:"action_id"`
	Name           string          `json:"name,omitempty"`
	TriggerPoint   string          `json:"trigger_point,omitempty"`
	BlockReference string          `json:"block_reference,omitempty"`
	MappingValues  []ActionMapping `json:"mapping_values,omitempty"`
}

// ActionMapping fills one action parameter: Source is "context" (read
// ContextPath, such as header:tag or block:name:line:line, optionally changed
// by Transform) or "fixed" (FixedValue).
type ActionMapping struct {
	Parameter   string `json:"parameter"`
	Source      string `json:"source"`
	ContextPath string `json:"context_path,omitempty"`
	FixedValue  any    `json:"fixed_value,omitempty"`
	Transform   string `json:"transform,omitempty"`
}

// Numbering is how orders from the template are numbered.
type Numbering struct {
	Prefix         string `json:"prefix,omitempty"`
	Suffix         string `json:"suffix,omitempty"`
	SequenceLength int    `json:"sequence_length,omitempty"`
	ResetPolicy    string `json:"reset_policy,omitempty"`
}

// GetTemplateInput is the payload of work_orders.GetTemplate.
type GetTemplateInput struct {
	TemplateID ID `json:"template_id"`
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
	// CreateMachineWithRef mints a machine bound to an external reference, so
	// an operator can find it later even if the answer is lost.
	CreateMachineWithRef(ctx context.Context, definitionID, ref string) (Machine, error)
	// DeleteMachine removes a machine and its history.
	DeleteMachine(ctx context.Context, machineID string) error
	// Commit saves one entity (insert or update by id) together with up to 25
	// appended records, all or nothing.
	Commit(ctx context.Context, req CommitRequest) error
	// CommitWalk is Commit with req.FSMWalk set. It returns the state the
	// machine reached, so the caller can check it is the one it expected.
	CommitWalk(ctx context.Context, req CommitRequest) (string, error)
	// CreateEntity stores a document and returns its id.
	CreateEntity(ctx context.Context, entityType string, data map[string]any) (string, error)
	GetEntity(ctx context.Context, entityType, id string) (map[string]any, error)
	// PatchEntity merges fields into an existing document.
	PatchEntity(ctx context.Context, entityType, id string, data map[string]any) error
	Walk(ctx context.Context, machineID, input string, payload map[string]any) (WalkResult, error)
	// ListEntities returns every document of the entity type (newest first),
	// each with its "id".
	ListEntities(ctx context.Context, entityType string) ([]map[string]any, error)
	// MachineHistory returns the FSM history of a machine, oldest first.
	MachineHistory(ctx context.Context, machineID string) ([]HistoryEntry, error)
	// FindEntities returns the documents matching every condition, newest
	// first, at most q.Limit of them (each with its "id").
	FindEntities(ctx context.Context, entityType string, q EntityQuery) ([]map[string]any, error)
}

// CommitUpdate is the primary write of a Commit: an upsert of entity ID. A nil
// Version writes unconditionally.
type CommitUpdate struct {
	Entity  string         `json:"entity"`
	ID      int64          `json:"id"`
	Version *int           `json:"version,omitempty"`
	Data    map[string]any `json:"data"`
}

// CommitAppend is one more record saved by the same Commit. A nil ID lets xolu
// assign it.
type CommitAppend struct {
	Entity string         `json:"entity"`
	ID     *int64         `json:"id,omitempty"`
	Data   map[string]any `json:"data"`
}

// CommitRequest is the payload of xolu's atomic multi-entity write.
type CommitRequest struct {
	Update CommitUpdate   `json:"update"`
	Append []CommitAppend `json:"append"`
	// FSMWalk, when set, fires one input on a machine in the same atomic
	// write: if the transition is refused nothing is saved.
	FSMWalk *CommitFSMWalk `json:"fsm_walk,omitempty"`
}

// CommitFSMWalk is the machine transition a Commit performs with its writes.
type CommitFSMWalk struct {
	Machine int64          `json:"machine"`
	Input   string         `json:"input"`
	Payload map[string]any `json:"payload,omitempty"`
}

// maxCommitAppends is how many records one xolu Commit can append.
const maxCommitAppends = 25

// Condition is one comparison of a document field. Op is "=", ">=" or "<=".
// Integer values (int, int64) are compared as numbers; strings as text.
type Condition struct {
	Field string
	Op    string
	Value any
}

// EntityQuery selects documents of one entity type: all conditions must hold.
type EntityQuery struct {
	Where []Condition
	Limit int
	// OrderBy lists fields to sort by, each newest/highest first. Empty means
	// created_at, then id.
	OrderBy []string
}

// HistoryEntry is one step of an FSM machine: the creation (empty From and
// Input) or a transition fired with Input from From to To.
type HistoryEntry struct {
	At    string `json:"at"`
	From  string `json:"from,omitempty"`
	Input string `json:"input,omitempty"`
	To    string `json:"to"`
	Note  string `json:"note,omitempty"`
}

// OrderBlock is the execution of one template block inside an order. Its State
// (pending, in_progress, completed) is a copy of the block's own FSM.
type OrderBlock struct {
	ID               string         `json:"id"`
	Index            int            `json:"index"`
	Name             string         `json:"name"`
	State            string         `json:"state"`
	PredecessorIndex *int           `json:"predecessor_index,omitempty"` // nil: no predecessor
	ResponsibleType  string         `json:"responsible_type,omitempty"`
	ResponsibleID    string         `json:"responsible_id,omitempty"`
	Values           map[string]any `json:"values,omitempty"`
	StartedAt        string         `json:"started_at,omitempty"`
	CompletedAt      string         `json:"completed_at,omitempty"`
}

// StockLine is the quantity of one consumable held by a warehouse.
type StockLine struct {
	ConsumableID string  `json:"consumable_id"`
	Name         string  `json:"name"`
	Unit         string  `json:"unit,omitempty"`
	Quantity     float64 `json:"quantity"`
}

// MobileWarehouse is the temporary warehouse an order opens for its supplies.
type MobileWarehouse struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Enabled           bool        `json:"enabled"`
	ResponsibleUserID string      `json:"responsible_user_id,omitempty"`
	Stock             []StockLine `json:"stock"`
}

// OrderDetail is the full record of an order: what GetOrder returns. Signed
// says a technician signature is stored; the signature image itself is not
// returned.
type OrderDetail struct {
	WorkOrder
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	ClaimedByUserID string `json:"claimed_by_user_id,omitempty"`
	Signed          bool   `json:"signed"`
	// HeaderConfirmedAt is when the header filled at execution time was
	// confirmed (orders that run blocks); blocks cannot start before it.
	HeaderConfirmedAt string         `json:"header_confirmed_at,omitempty"`
	BlockValues       map[string]any `json:"block_values,omitempty"` // as stored: keys template_block_<block>_line_<line>
	// BlockAnswers is BlockValues by block and line name (template orders whose
	// template can still be read).
	BlockAnswers    map[string]map[string]any `json:"block_answers,omitempty"`
	Blocks          []OrderBlock              `json:"blocks,omitempty"`
	MobileWarehouse *MobileWarehouse          `json:"mobile_warehouse,omitempty"`
	// History is the FSM history of orders driven by a xolu machine (those
	// opened without a template).
	History []HistoryEntry `json:"history,omitempty"`
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
type NotFoundError struct {
	ID string
	// Kind names what was not found; empty means "work order".
	Kind string
}

func (e *NotFoundError) Error() string {
	kind := e.Kind
	if kind == "" {
		kind = "work order"
	}
	return fmt.Sprintf("%s %q not found", kind, e.ID)
}

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

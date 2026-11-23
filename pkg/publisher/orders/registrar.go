package orders

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Gastonaso15/molu/pkg/hub"
)

// Backoff controls registration retries while the Hub is not ready yet.
type Backoff struct {
	Initial     time.Duration
	Max         time.Duration
	MaxAttempts int // attempts per tool, including the first
}

// DefaultBackoff waits 200ms, doubling up to 5s, for about a minute in total.
func DefaultBackoff() Backoff {
	return Backoff{Initial: 200 * time.Millisecond, Max: 5 * time.Second, MaxAttempts: 15}
}

// Registrar publishes the service's tools in a remote Hub and removes them on
// shutdown.
type Registrar struct {
	hubURL       string
	publisherURL string
	backoff      Backoff
	client       *http.Client
}

// NewRegistrar targets the Hub at hubURL; publisherURL is the base URL at
// which the Execution Bridge can reach this service.
func NewRegistrar(hubURL, publisherURL string, b Backoff) *Registrar {
	if b.Initial <= 0 || b.Max <= 0 || b.MaxAttempts <= 0 {
		b = DefaultBackoff()
	}
	return &Registrar{
		hubURL:       strings.TrimRight(hubURL, "/"),
		publisherURL: strings.TrimRight(publisherURL, "/"),
		backoff:      b,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// Tools returns the Hub registrations for the four work-order functions.
func (r *Registrar) Tools() []hub.ToolRegistration {
	auth := map[string]any{"mode": "bearer", "header": "Authorization", "token_ref": TokenRef}
	mk := func(fn, desc, route string, schema map[string]any) hub.ToolRegistration {
		return hub.ToolRegistration{
			PublisherID: PublisherID,
			Name:        Namespace + "." + fn,
			Description: desc,
			InputSchema: schema,
			Namespace:   Namespace,
			Metadata:    map[string]any{"endpoint": r.publisherURL + route, "auth": auth},
		}
	}
	return []hub.ToolRegistration{
		mk("CreateOrder",
			"Open a new work order. Without template_id it is a simple order (draft, then in_progress, then completed). "+
				"With template_id (see ListTemplates) it follows that template's workflow: assignment_type and responsible_type are required, "+
				"header_values answers the template's header items filled at creation, and asset_ids picks its assets. "+
				"If the template has blocks or enables consumables, each block gets its own record/lifecycle (pending) and consumables open an empty mobile warehouse "+
				"(mobile_warehouse_id == order id) in one atomic Commit (at most 25 appended records). Ids of such orders are large numbers, treat them as opaque strings. "+
				"A direct assignment needs a crew or third_party responsible_id; use board_posting or AssignOrder for a user.",
			"/api/v1/orders/create",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":              map[string]any{"type": "string", "description": "Short title of the work to do. Required, except with a template (defaults to the template's name)."},
					"description":        map[string]any{"type": "string", "description": "Optional details (defaults to the template's description)."},
					"assigned_to":        map[string]any{"type": "string", "description": "Simple orders only: technician name or email. Ignored when an assignment resolves the responsible."},
					"template_id":        idSchema("Id of the template the order follows."),
					"template_version":   map[string]any{"type": "integer", "description": "Optional: fail if the template is not at this version."},
					"assignment_type":    map[string]any{"type": "string", "enum": []any{AssignmentDirect, AssignmentBoard}, "description": "direct_assignment: assign now. board_posting: publish for someone to claim."},
					"responsible_type":   map[string]any{"type": "string", "enum": []any{"user", "crew", "third_party"}, "description": "Who is responsible, or who may claim it."},
					"responsible_id":     idSchema("Direct assignment: id of the crew or outsourced party. Omit for a board posting."),
					"header_values":      map[string]any{"type": "object", "description": "Answers by header tag, for the header items filled at creation (see ListTemplates header_items)."},
					"asset_ids":          map[string]any{"type": "array", "items": idSchema("Asset id."), "description": "Assets the order is about, within the template's min/max and asset type."},
					"created_by_user_id": idSchema("Optional user id recorded as responsible_user_id on a newly opened mobile warehouse."),
				},
				"additionalProperties": false,
			}),
		mk("GetOrder",
			"Fetch one work order in full by id: state, assignment and who claimed it, header and block answers, its blocks with their own state and values (in order, with predecessors), "+
				"the mobile warehouse with the stock it holds, whether it is signed (the signature image is not returned), and for orders without a template the FSM history of its transitions. "+
				"Use ListOrders to find orders.",
			"/api/v1/orders/get",
			map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"order_id": map[string]any{"type": "string", "minLength": 1}},
				"required":             []any{"order_id"},
				"additionalProperties": false,
			}),
		mk("AdvanceOrder",
			"Move a work order forward. Simple orders: fire an input, start (draft to in_progress) or complete (in_progress to completed, "+
				"requires payload.technician_signature); the state machine may reject it and the rejection is returned as-is. "+
				"Template orders: the only input is complete, which moves to the next state of the template; header_values answers "+
				"the header items filled while executing, block_values answers the lines of the template's blocks (GetOrder shows them as block_answers), "+
				"and the step that reaches the last state needs payload.technician_signature. "+
				"Rejected steps (stale step, final state, board posting not claimed) come back as INVALID_TRANSITION with details.rule.",
			"/api/v1/orders/advance",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"order_id":       map[string]any{"type": "string", "minLength": 1},
					"input":          map[string]any{"type": "string", "minLength": 1, "description": "start or complete (template orders: complete)."},
					"payload":        map[string]any{"type": "object", "description": "complete needs {\"technician_signature\": \"...\"} (simple orders, and the last step of a template order)."},
					"header_values":  map[string]any{"type": "object", "description": "Template orders: answers by header tag for the items filled while executing."},
					"block_values":   map[string]any{"type": "object", "description": "Template orders with blocks, optional: answers to the blocks' lines by block name then line name, e.g. {\"Preparacion\": {\"aislado\": true}}. Checked against each line's type; a missing or blank answer keeps the stored one."},
					"workflow_index": map[string]any{"type": "integer", "description": "Template orders, optional: the step you believe the order is at; a mismatch is rejected as stale."},
				},
				"required":             []any{"order_id", "input"},
				"additionalProperties": false,
			}),
		mk("ListOrders",
			"List work orders, newest first, with optional filters (all combined with AND): state, template_id, assignment_type, "+
				"responsible_type plus responsible_id (a crew, a third_party or a user), asset_id, created_from/created_to, or backlog=true "+
				"for the job board (board_posting orders nobody has claimed yet; awaiting_claim marks them). Returns a summary of each order; "+
				"use GetOrder for one order in full. total is the number of orders returned and truncated says more matched than limit. "+
				"Priority and location are not order fields in the work order module, so they cannot be filtered.",
			"/api/v1/orders/list",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"state":            map[string]any{"type": "string", "description": "Current state name, e.g. draft, in_progress, completed, or a template's state."},
					"template_id":      idSchema("Only orders opened from this template."),
					"assignment_type":  map[string]any{"type": "string", "enum": []string{AssignmentDirect, AssignmentBoard}},
					"responsible_type": map[string]any{"type": "string", "enum": []string{"user", "crew", "third_party"}, "description": "Kind of responsible. Needed when responsible_id is given."},
					"responsible_id":   idSchema("Id of the responsible user, crew or third party."),
					"backlog":          map[string]any{"type": "boolean", "description": "true: only unclaimed job-board orders."},
					"asset_id":         idSchema("Only orders that include this asset (template orders)."),
					"created_from":     map[string]any{"type": "string", "description": "RFC 3339 timestamp or YYYY-MM-DD (start of that day, UTC)."},
					"created_to":       map[string]any{"type": "string", "description": "RFC 3339 timestamp or YYYY-MM-DD (end of that day, UTC)."},
					"limit":            map[string]any{"type": "integer", "minimum": 1, "maximum": maxListLimit, "description": "Default 50."},
				},
				"additionalProperties": false,
			}),
		mk("GetTemplate",
			"Read one work order template in full (read-only): workflow states, header items (filled at creation or at execution), blocks with their lines and predecessors, "+
				"default consumables (with names) and whether extra ones are allowed, actions with their parameter mappings (with names), rules, numbering, "+
				"and whether CreateOrder can use it (supported). Action and rule configuration is stored by the work order module but not executed yet.",
			"/api/v1/orders/template",
			map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"template_id": idSchema("Id of the template (see ListTemplates).")},
				"required":             []string{"template_id"},
				"additionalProperties": false,
			}),
		mk("ListCrews",
			"List the crews (internal teams, e.g. electricians or plumbers) sorted by name, each with its responsible user id and member user ids. "+
				"Filters: name (contains), responsible_user_id, member_user_id. User names are not resolved here; use GetCrew for that. "+
				"A crew's responsible user is the one who may claim job-board orders for it. Availability of crews is not tracked by the work order module.",
			"/api/v1/orders/crews/list",
			listSchema(map[string]any{
				"name":                map[string]any{"type": "string", "description": "Part of the crew name, ignoring case."},
				"responsible_user_id": idSchema("Crews whose responsible user is this user."),
				"member_user_id":      idSchema("Crews this user belongs to."),
			})),
		mk("GetCrew",
			"Read one crew, with the names of its responsible user and members (a user that cannot be read has no name).",
			"/api/v1/orders/crews/get",
			getSchema("crew_id", "Id of the crew (see ListCrews).")),
		mk("CreateCrew",
			"Create an internal crew with a name, responsible_user_id and at least two distinct active member_user_ids. "+
				"If responsible_user_id is not listed in member_user_ids it is added automatically.",
			"/api/v1/orders/crews/create",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":                map[string]any{"type": "string", "minLength": 1},
					"description":         map[string]any{"type": "string"},
					"responsible_user_id": idSchema("Active user id responsible for the crew."),
					"member_user_ids":     map[string]any{"type": "array", "minItems": 2, "items": idSchema("Active member user id.")},
				},
				"required":             []string{"name", "responsible_user_id", "member_user_ids"},
				"additionalProperties": false,
			}),
		mk("UpdateCrew",
			"Update an existing crew (partial update guarded by _version). Only provided fields change; member_user_ids must have at least two distinct active users when sent, "+
				"and the responsible user is automatically ensured to be a member.",
			"/api/v1/orders/crews/update",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"crew_id":             idSchema("Id of the crew to update."),
					"name":                map[string]any{"type": "string", "minLength": 1},
					"description":         map[string]any{"type": "string"},
					"responsible_user_id": idSchema("New responsible active user id."),
					"member_user_ids":     map[string]any{"type": "array", "minItems": 2, "items": idSchema("Active member user id.")},
				},
				"required":             []string{"crew_id"},
				"additionalProperties": false,
			}),
		mk("ListOutsourced",
			"List the third-party providers (outsourced contractors) sorted by name. Each has a description, a representative account (responsible user id), "+
				"the signatory's clarification and whether a signature is stored (the image is not returned). Filters: name (contains), responsible_user_id. "+
				"The module has no provider code field.",
			"/api/v1/orders/outsourced/list",
			listSchema(map[string]any{
				"name":                map[string]any{"type": "string", "description": "Part of the provider name, ignoring case."},
				"responsible_user_id": idSchema("Providers whose representative account is this user."),
			})),
		mk("GetOutsourced",
			"Read one third-party provider, with the name of its representative account.",
			"/api/v1/orders/outsourced/get",
			getSchema("outsourced_id", "Id of the provider (see ListOutsourced).")),
		mk("CreateOutsourced",
			"Create a third-party provider (outsourced contractor) with a name, optional description, optional representative account (active responsible_user_id), "+
				"and optional signature / signature_clarification (the signature image is stored but not returned in responses).",
			"/api/v1/orders/outsourced/create",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":                    map[string]any{"type": "string", "minLength": 1},
					"description":             map[string]any{"type": "string"},
					"responsible_user_id":     idSchema("Optional active user id representing the provider."),
					"signature":               map[string]any{"type": "string"},
					"signature_clarification": map[string]any{"type": "string"},
				},
				"required":             []string{"name"},
				"additionalProperties": false,
			}),
		mk("UpdateOutsourced",
			"Update an existing third-party provider (partial update guarded by _version). Pass responsible_user_id=\"\" (or \"0\") to unlink the representative account.",
			"/api/v1/orders/outsourced/update",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"outsourced_id":           idSchema("Id of the outsourced provider to update."),
					"name":                    map[string]any{"type": "string", "minLength": 1},
					"description":             map[string]any{"type": "string"},
					"responsible_user_id":     idSchema("Active user id, or empty/0 to unlink."),
					"signature":               map[string]any{"type": "string"},
					"signature_clarification": map[string]any{"type": "string"},
				},
				"required":             []string{"outsourced_id"},
				"additionalProperties": false,
			}),
		mk("ListWarehouses",
			"List the warehouses sorted by name: location (lat,lng), enabled, responsible user id, and mobile=true for the temporary warehouse an order opens for its supplies (work_order_id says which order). "+
				"Filters: name (contains), enabled, mobile (omit it to get both kinds). Use GetWarehouse to see what one holds.",
			"/api/v1/orders/warehouses/list",
			listSchema(map[string]any{
				"name":    map[string]any{"type": "string", "description": "Part of the warehouse name, ignoring case."},
				"enabled": map[string]any{"type": "boolean"},
				"mobile":  map[string]any{"type": "boolean", "description": "true: only order warehouses; false: only the fixed ones."},
			})),
		mk("GetWarehouse",
			"Read one warehouse with the name of its responsible user and the consumables it holds (name, unit, quantity).",
			"/api/v1/orders/warehouses/get",
			getSchema("warehouse_id", "Id of the warehouse (see ListWarehouses).")),
		mk("CreateWarehouse",
			"Create a fixed warehouse with a name (max 200 chars), GPS location (\"lat,lng\"), active responsible_user_id and optional enabled flag (defaults to true).",
			"/api/v1/orders/warehouses/create",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":                map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
					"description":         map[string]any{"type": "string"},
					"location":            map[string]any{"type": "string", "description": "GPS coordinates as \"latitude,longitude\"."},
					"responsible_user_id": idSchema("Active user id responsible for the warehouse."),
					"enabled":             map[string]any{"type": "boolean", "description": "Defaults to true."},
				},
				"required":             []string{"name", "location", "responsible_user_id"},
				"additionalProperties": false,
			}),
		mk("UpdateWarehouse",
			"Update an existing warehouse (partial update guarded by _version). Disabling (enabled=false) is rejected with INVALID_TRANSITION (details.rule=warehouse_has_stock) if the warehouse still holds stock.",
			"/api/v1/orders/warehouses/update",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"warehouse_id":        idSchema("Id of the warehouse to update."),
					"name":                map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
					"description":         map[string]any{"type": "string"},
					"location":            map[string]any{"type": "string", "description": "GPS coordinates as \"latitude,longitude\"."},
					"responsible_user_id": idSchema("New responsible active user id."),
					"enabled":             map[string]any{"type": "boolean"},
				},
				"required":             []string{"warehouse_id"},
				"additionalProperties": false,
			}),
		mk("ListConsumables",
			"List the consumables (supplies) sorted by name, with type, unit, stock_min (alert threshold), total_stock across all warehouses and low_stock (some warehouse holds stock_min or less). "+
				"Filters: name (contains), enabled, consumable_type_id, low_stock. Use GetConsumable to see where one is stored.",
			"/api/v1/orders/consumables/list",
			listSchema(map[string]any{
				"name":               map[string]any{"type": "string", "description": "Part of the consumable name, ignoring case."},
				"enabled":            map[string]any{"type": "boolean"},
				"consumable_type_id": idSchema("Only consumables of this type."),
				"low_stock":          map[string]any{"type": "boolean", "description": "true: only those at or below their minimum in some warehouse."},
			})),
		mk("GetConsumable",
			"Read one consumable with the quantity held in each warehouse (mobile order warehouses included and marked).",
			"/api/v1/orders/consumables/get",
			getSchema("consumable_id", "Id of the consumable (see ListConsumables).")),
		mk("CreateConsumable",
			"Create a consumable (supply item) with a name (max 200 chars), unit_id, optional consumable_type_id, non-negative stock_min (defaults to 0) and enabled (defaults to true). "+
				"Initial stock must be registered through RegisterMovement.",
			"/api/v1/orders/consumables/create",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":               map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
					"description":        map[string]any{"type": "string"},
					"consumable_type_id": idSchema("Optional consumable type id."),
					"unit_id":            idSchema("Unit of measure id (entity unit)."),
					"stock_min":          map[string]any{"type": "number", "minimum": 0, "description": "Minimum stock alert threshold (default 0)."},
					"enabled":            map[string]any{"type": "boolean", "description": "Defaults to true."},
				},
				"required":             []string{"name", "unit_id"},
				"additionalProperties": false,
			}),
		mk("UpdateConsumable",
			"Update a consumable's metadata (partial update guarded by _version). Stock quantities cannot be edited here; use RegisterMovement. Pass consumable_type_id=\"\" (or \"0\") to clear its type.",
			"/api/v1/orders/consumables/update",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"consumable_id":      idSchema("Id of the consumable to update."),
					"name":               map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
					"description":        map[string]any{"type": "string"},
					"consumable_type_id": idSchema("Consumable type id, or empty/0 to clear."),
					"unit_id":            idSchema("Unit of measure id."),
					"stock_min":          map[string]any{"type": "number", "minimum": 0},
					"enabled":            map[string]any{"type": "boolean"},
				},
				"required":             []string{"consumable_id"},
				"additionalProperties": false,
			}),
		mk("ListLowStock",
			"Minimum-stock alerts: every consumable/warehouse pair whose quantity is at or below the consumable's stock_min (the rule the work order module uses to flag a consumable), "+
				"largest shortfall first. Mobile order warehouses are included and marked warehouse_mobile. Filters: warehouse_id, consumable_id.",
			"/api/v1/orders/stock/low",
			listSchema(map[string]any{
				"warehouse_id":  idSchema("Only alerts of this warehouse."),
				"consumable_id": idSchema("Only alerts of this consumable."),
			})),
		mk("ClaimOrder",
			"Claim a job-board order (board_posting, nobody has claimed it; see ListOrders backlog=true). user_id is the person claiming. "+
				"The claim is on behalf of the order's responsible type: a user order can only be claimed by that user; a crew or third_party order by a user who is "+
				"that crew's or third party's responsible user (see ListCrews / ListOutsourced). Give responsible_id to choose among several; it can be left out when only one is eligible. "+
				"Of two simultaneous claims only one wins. Claiming again with the same user and responsible changes nothing (already_claimed=true). "+
				"Rejections come back as INVALID_TRANSITION with details.rule: not_claimable (assigned directly), already_claimed, ineligible.",
			"/api/v1/orders/claim",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"order_id":       map[string]any{"type": "string", "minLength": 1},
					"user_id":        idSchema("Id of the user who claims."),
					"responsible_id": idSchema("Crew or third party to claim for; for a user order, the user."),
				},
				"required":             []string{"order_id", "user_id"},
				"additionalProperties": false,
			}),
		mk("AssignOrder",
			"Assign or reassign an open (non-completed) work order to a user, crew or third_party (direct_assignment, the default), or place it on the job board (board_posting) "+
				"so an eligible responsible can claim it. Direct assignment checks that the target record exists (and is active for users) and sets assigned_to from its name; "+
				"any prior claim is cleared. Repeating the exact same assignment changes nothing (already_assigned=true). Guarded by optimistic locking (_version): "+
				"completed orders return INVALID_TRANSITION with details.rule=final_state and concurrent modifications return details.rule=stale_state.",
			"/api/v1/orders/assign",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"order_id":         map[string]any{"type": "string", "minLength": 1},
					"assignment_type":  map[string]any{"type": "string", "enum": []any{AssignmentDirect, AssignmentBoard}, "description": "direct_assignment (default): assign now. board_posting: place on the job board."},
					"responsible_type": map[string]any{"type": "string", "enum": []any{"user", "crew", "third_party"}, "description": "Kind of responsible, or who may claim a board posting."},
					"responsible_id":   idSchema("Direct assignment: id of the user, crew or third party. Omit for a board posting."),
				},
				"required":             []string{"order_id", "responsible_type"},
				"additionalProperties": false,
			}),
		mk("RegisterMovement",
			"Register an atomic inventory movement (entry, exit, transfer or adjust) for a consumable. Updates consumable.stock_by_warehouse (stored in hundredths, "+
				"guarded by _version) and appends both the movement record and an idempotent movement_request receipt in one Commit. "+
				"entry/adjust take target_warehouse_id only (adjust sets physical stock and allows 0); exit takes origin_warehouse_id only; transfer takes distinct origin and target. "+
				"Provide a 32-char lowercase hex request_id (or let the service generate one) for safe idempotent retries: repeating the same request returns already_processed=true, "+
				"reusing request_id with different content fails with INVALID_TRANSITION (details.rule=request_reused). Other domain rejections: insufficient_stock, consumable_disabled, warehouse_disabled, stock_overflow, stale_state.",
			"/api/v1/orders/movements/register",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type":                map[string]any{"type": "string", "enum": []any{string(MovementEntry), string(MovementExit), string(MovementTransfer), string(MovementAdjust)}},
					"consumable_id":       idSchema("Id of the consumable."),
					"user_id":             idSchema("Id of the user performing the movement."),
					"origin_warehouse_id": idSchema("Origin warehouse id (exit and transfer)."),
					"target_warehouse_id": idSchema("Destination warehouse id (entry, transfer and adjust)."),
					"quantity":            map[string]any{"type": "number", "minimum": 0, "description": "Quantity with at most 2 decimals (>0 for entry/exit/transfer; >=0 physical stock for adjust)."},
					"reason":              map[string]any{"type": "string", "minLength": 1, "description": "Mandatory reason for the movement."},
					"reference":           map[string]any{"type": "string", "description": "Optional external reference (e.g. receipt or work order number)."},
					"request_id":          map[string]any{"type": "string", "pattern": "^[0-9a-f]{32}$", "description": "Optional 32 lowercase hex idempotency key; generated if omitted."},
				},
				"required":             []string{"type", "consumable_id", "user_id", "quantity", "reason"},
				"additionalProperties": false,
			}),
		mk("ConsumeFromOrder",
			"Consume stock of a consumable from an open work order's mobile warehouse (atomic exit movement via RegisterMovement). "+
				"Load stock into the mobile warehouse first with RegisterMovement (transfer). Rejects completed orders (final_state), orders without a mobile warehouse "+
				"(no_mobile_warehouse), consumables excluded by the template when extra consumables are disabled (consumable_not_allowed), and insufficient stock (insufficient_stock). "+
				"Supports idempotent retries via request_id.",
			"/api/v1/orders/consume",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"order_id":      map[string]any{"type": "string", "minLength": 1},
					"consumable_id": idSchema("Id of the consumable being consumed."),
					"user_id":       idSchema("Id of the user recording the consumption."),
					"quantity":      map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "Positive quantity with at most 2 decimals."},
					"reason":        map[string]any{"type": "string", "description": "Optional reason (defaults to order consumption note)."},
					"reference":     map[string]any{"type": "string", "description": "Optional reference (defaults to WO-<order_id>)."},
					"request_id":    map[string]any{"type": "string", "pattern": "^[0-9a-f]{32}$", "description": "Optional 32 lowercase hex idempotency key."},
				},
				"required":             []string{"order_id", "consumable_id", "user_id", "quantity"},
				"additionalProperties": false,
			}),
		mk("ReturnLeftovers",
			"Return leftover stock of a consumable from a work order's mobile warehouse back to a target warehouse (atomic transfer via RegisterMovement). "+
				"Works on both open and completed orders. Omit quantity to return all remaining stock of that consumable in the mobile warehouse (fails with no_leftovers if empty). "+
				"Supports idempotent retries via request_id.",
			"/api/v1/orders/return-leftovers",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"order_id":            map[string]any{"type": "string", "minLength": 1},
					"consumable_id":       idSchema("Id of the consumable being returned."),
					"target_warehouse_id": idSchema("Destination warehouse id receiving the leftovers."),
					"user_id":             idSchema("Id of the user recording the return."),
					"quantity":            map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "Optional positive quantity (max 2 decimals). Omit to return all remaining stock."},
					"reason":              map[string]any{"type": "string", "description": "Optional reason (defaults to order leftover return note)."},
					"reference":           map[string]any{"type": "string", "description": "Optional reference (defaults to WO-<order_id>)."},
					"request_id":          map[string]any{"type": "string", "pattern": "^[0-9a-f]{32}$", "description": "Optional 32 lowercase hex idempotency key."},
				},
				"required":             []string{"order_id", "consumable_id", "target_warehouse_id", "user_id"},
				"additionalProperties": false,
			}),
		mk("ListTemplates",
			"List work order templates (read-only): workflow states, header items to answer, blocks, default consumables, actions and rules, asset limits, and whether CreateOrder can use them (supported). "+
				"Names of consumables and actions are not resolved here; use GetTemplate for one template with names. Only the latest version of each template unless include_old_versions is true.",
			"/api/v1/orders/templates",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"include_old_versions": map[string]any{"type": "boolean", "description": "Also list superseded template versions."},
				},
				"additionalProperties": false,
			}),
		mk("CreateTemplate",
			"Create version 1 (latest=true) of a work order template after validating workflow states (at least 2 distinct), header items, blocks (acyclic predecessors, max 25 commit records including mobile warehouse), "+
				"consumables, actions, rules and numbering. Templates are immutable once created; use PublishTemplateVersion for a new version.",
			"/api/v1/orders/templates/create",
			templateWriteSchema(false)),
		mk("PublishTemplateVersion",
			"Publish a new version (version+1, latest=true) of an existing latest work order template, atomically setting latest=false on previous_template_id in one Commit guarded by _version. "+
				"Rejects already-superseded versions with INVALID_TRANSITION (details.rule=not_latest) and concurrent changes with stale_state.",
			"/api/v1/orders/templates/publish-version",
			templateWriteSchema(true)),
	}
}

// Register publishes every tool, retrying with exponential backoff while the
// Hub is unreachable or answers 5xx. A 4xx is a contract problem and is not retried.
func (r *Registrar) Register(ctx context.Context) error {
	for _, tool := range r.Tools() {
		if err := r.withRetry(ctx, func() error { return r.post(ctx, "/api/v1/registry/register", tool) }); err != nil {
			return fmt.Errorf("register %s: %w", tool.Name, err)
		}
	}
	return nil
}

// Unregister removes every tool, attempting all of them and joining failures.
// Call it with a fresh context on shutdown: the service context is already done.
func (r *Registrar) Unregister(ctx context.Context) error {
	var errs []error
	for _, tool := range r.Tools() {
		body := map[string]string{"publisher_id": tool.PublisherID, "name": tool.Name}
		if err := r.post(ctx, "/api/v1/registry/unregister", body); err != nil {
			errs = append(errs, fmt.Errorf("unregister %s: %w", tool.Name, err))
		}
	}
	return errors.Join(errs...)
}

type permanentError struct{ error }

func (r *Registrar) withRetry(ctx context.Context, op func() error) error {
	delay := r.backoff.Initial
	var last error
	for attempt := 1; attempt <= r.backoff.MaxAttempts; attempt++ {
		last = op()
		if last == nil {
			return nil
		}
		var perm permanentError
		if errors.As(last, &perm) {
			return perm.error
		}
		if attempt == r.backoff.MaxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last error: %v)", ctx.Err(), last)
		case <-time.After(delay):
		}
		if delay *= 2; delay > r.backoff.Max {
			delay = r.backoff.Max
		}
	}
	return fmt.Errorf("giving up after %d attempts: %w", r.backoff.MaxAttempts, last)
}

func (r *Registrar) post(ctx context.Context, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return permanentError{err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.hubURL+path, bytes.NewReader(b))
	if err != nil {
		return permanentError{err}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err // transport failure: the Hub may simply not be up yet
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 500:
		return fmt.Errorf("hub answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	default:
		return permanentError{fmt.Errorf("hub answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))}
	}
}

// idSchema describes an entity id: a string, or a number, of digits.
func idSchema(desc string) map[string]any {
	return map[string]any{"type": []any{"string", "integer"}, "description": desc}
}

// listSchema is the input schema of a list function: its filters plus limit.
func listSchema(filters map[string]any) map[string]any {
	props := map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxListLimit, "description": "Default 50."}}
	for k, v := range filters {
		props[k] = v
	}
	return map[string]any{"type": "object", "properties": props, "additionalProperties": false}
}

// getSchema is the input schema of a function that reads one record by id.
func getSchema(field, desc string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{field: idSchema(desc)},
		"required":             []string{field},
		"additionalProperties": false,
	}
}

func templateWriteSchema(withPrevious bool) map[string]any {
	props := map[string]any{
		"name":                      map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
		"description":               map[string]any{"type": "string"},
		"color":                     map[string]any{"type": "string"},
		"icon":                      map[string]any{"type": "string"},
		"asset_type_id":             idSchema("Optional asset type id required for selected assets."),
		"min_assets":                map[string]any{"type": "integer", "minimum": 0},
		"max_assets":                map[string]any{"type": "integer", "minimum": 0},
		"workflow_states":           map[string]any{"type": "array", "minItems": 2, "items": map[string]any{"type": "string", "minLength": 1}},
		"header_items":              map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"blocks":                    map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"consumables_enabled":       map[string]any{"type": "boolean"},
		"extra_consumables_allowed": map[string]any{"type": "boolean"},
		"consumables":               map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"actions":                   map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"rules":                     map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"numbering":                 map[string]any{"type": "object"},
	}
	req := []string{"name", "workflow_states"}
	if withPrevious {
		props["previous_template_id"] = idSchema("Id of the current latest template version to supersede.")
		req = append([]string{"previous_template_id"}, req...)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             req,
		"additionalProperties": false,
	}
}

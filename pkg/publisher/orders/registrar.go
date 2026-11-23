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
				"Templates marked supported=false (blocks, mobile warehouse) are rejected. A direct assignment needs a crew or third_party "+
				"responsible_id; a user cannot be assigned directly from here, use board_posting instead.",
			"/api/v1/orders/create",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":            map[string]any{"type": "string", "description": "Short title of the work to do. Required, except with a template (defaults to the template's name)."},
					"description":      map[string]any{"type": "string", "description": "Optional details (defaults to the template's description)."},
					"assigned_to":      map[string]any{"type": "string", "description": "Simple orders only: technician name or email. Ignored when an assignment resolves the responsible."},
					"template_id":      idSchema("Id of the template the order follows."),
					"template_version": map[string]any{"type": "integer", "description": "Optional: fail if the template is not at this version."},
					"assignment_type":  map[string]any{"type": "string", "enum": []any{AssignmentDirect, AssignmentBoard}, "description": "direct_assignment: assign now. board_posting: publish for someone to claim."},
					"responsible_type": map[string]any{"type": "string", "enum": []any{"user", "crew", "third_party"}, "description": "Who is responsible, or who may claim it."},
					"responsible_id":   idSchema("Direct assignment: id of the crew or outsourced party. Omit for a board posting."),
					"header_values":    map[string]any{"type": "object", "description": "Answers by header tag, for the header items filled at creation (see ListTemplates header_items)."},
					"asset_ids":        map[string]any{"type": "array", "items": idSchema("Asset id."), "description": "Assets the order is about, within the template's min/max and asset type."},
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
				"the header items filled while executing, and the step that reaches the last state needs payload.technician_signature. "+
				"Rejected steps (stale step, final state, board posting not claimed) come back as INVALID_TRANSITION with details.rule.",
			"/api/v1/orders/advance",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"order_id":       map[string]any{"type": "string", "minLength": 1},
					"input":          map[string]any{"type": "string", "minLength": 1, "description": "start or complete (template orders: complete)."},
					"payload":        map[string]any{"type": "object", "description": "complete needs {\"technician_signature\": \"...\"} (simple orders, and the last step of a template order)."},
					"header_values":  map[string]any{"type": "object", "description": "Template orders: answers by header tag for the items filled while executing."},
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

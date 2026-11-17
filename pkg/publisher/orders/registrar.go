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
			"Fetch a work order and its current lifecycle state by id.",
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
		mk("ListTemplates",
			"List work order templates (read-only): workflow states, header items to answer, asset limits, and whether CreateOrder can use them (supported). Only the latest version of each template unless include_old_versions is true.",
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

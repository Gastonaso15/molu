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

// Tools returns the Hub registrations for the three work-order functions.
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
			"Open a new work order. Returns the order with its initial lifecycle state.",
			"/api/v1/orders/create",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":       map[string]any{"type": "string", "minLength": 1, "description": "Short title of the work to do."},
					"description": map[string]any{"type": "string", "description": "Optional details."},
				},
				"required":             []any{"title"},
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
			"Move a work order through its lifecycle by firing an input (start, complete, cancel). "+
				"The state machine may reject the transition; the rejection is returned as-is.",
			"/api/v1/orders/advance",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"order_id": map[string]any{"type": "string", "minLength": 1},
					"input":    map[string]any{"type": "string", "minLength": 1, "description": "Lifecycle input, e.g. start, complete, cancel."},
					"payload":  map[string]any{"type": "object", "description": "Data the transition guard may inspect, e.g. {\"resolution\": \"...\"} for complete."},
				},
				"required":             []any{"order_id", "input"},
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

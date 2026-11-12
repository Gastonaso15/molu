package issues

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
	MaxAttempts int
}

// DefaultBackoff waits 200ms, doubling up to 5s, for about a minute in total.
func DefaultBackoff() Backoff {
	return Backoff{Initial: 200 * time.Millisecond, Max: 5 * time.Second, MaxAttempts: 15}
}

// Registrar publishes the service's tools in a remote Hub and removes them on shutdown.
type Registrar struct {
	hubURL       string
	publisherURL string
	backoff      Backoff
	client       *http.Client
}

// NewRegistrar creates a new Registrar targeting the Hub at hubURL.
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

// Tools returns the Hub registrations for the Issue Tracker functions.
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
		mk("CreateIssue",
			"Create a new operational issue associated optionally with an asset. Computes SLA deadlines and initializes lifecycle FSM.",
			"/api/v1/issues/create",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tenant":      map[string]any{"type": "string", "description": "Tenant identifier."},
					"title":       map[string]any{"type": "string", "minLength": 1, "description": "Short summary of the issue."},
					"description": map[string]any{"type": "string", "description": "Detailed explanation of the issue."},
					"priority":    map[string]any{"type": "string", "enum": []any{"critical", "high", "medium", "low"}, "description": "Issue priority level."},
					"severity":    map[string]any{"type": "string", "enum": []any{"critical", "major", "minor"}, "description": "Issue severity level."},
					"asset_id":    map[string]any{"type": "integer", "description": "Related asset identifier in xolu."},
					"reporter_id": map[string]any{"type": "integer", "description": "User id of the reporter."},
				},
				"required":             []any{"title"},
				"additionalProperties": false,
			}),
		mk("GetIssue",
			"Fetch an issue document and its current lifecycle state and SLA by id.",
			"/api/v1/issues/get",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tenant":   map[string]any{"type": "string", "description": "Tenant identifier."},
					"issue_id": map[string]any{"type": "integer", "description": "Issue ID to look up."},
				},
				"required":             []any{"issue_id"},
				"additionalProperties": false,
			}),
		mk("AdvanceIssue",
			"Advance an issue through its lifecycle by firing a transition input (triage, start, hold, resume, resolve, close, reject). "+
				"Invalid transitions are rejected by the FSM and returned verbatim.",
			"/api/v1/issues/advance",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tenant":     map[string]any{"type": "string", "description": "Tenant identifier."},
					"issue_id":   map[string]any{"type": "integer", "description": "Issue ID to transition."},
					"transition": map[string]any{"type": "string", "enum": []any{"triage", "start", "hold", "resume", "resolve", "close", "reject"}, "description": "Lifecycle transition input."},
					"actor_id":   map[string]any{"type": "integer", "description": "User ID performing the transition."},
				},
				"required":             []any{"issue_id", "transition"},
				"additionalProperties": false,
			}),
	}
}

// Register publishes every tool in the Hub, retrying with exponential backoff while unreachable or answering 5xx.
func (r *Registrar) Register(ctx context.Context) error {
	for _, tool := range r.Tools() {
		if err := r.withRetry(ctx, func() error { return r.post(ctx, "/api/v1/registry/register", tool) }); err != nil {
			return fmt.Errorf("register %s: %w", tool.Name, err)
		}
	}
	return nil
}

// Unregister removes every tool from the Hub on shutdown.
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
		return err
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


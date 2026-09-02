package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ha1tch/molu/pkg/health"
	"github.com/ha1tch/molu/pkg/obs"
	"github.com/ha1tch/molu/pkg/semantic"
)

// Standard Molu Front Error Codes
const (
	ErrCodeUnavailable = "XOLU-MOLU-FRONT-UNAVAILABLE"
	ErrCodeStartup     = "XOLU-MOLU-FRONT-STARTUP"
	ErrCodeTimeout     = "XOLU-MOLU-FRONT-TIMEOUT"
	ErrCodeContract    = "XOLU-MOLU-FRONT-CONTRACT"
	ErrCodeHubUnavail  = "XOLU-MOLU-FRONT-HUB-UNAVAILABLE"
)

// ToolError represents a structured error returned to the MCP client.
type ToolError struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Detail  map[string]interface{} `json:"detail,omitempty"`
}

func (e *ToolError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Executor handles tool execution against xolu with safety checks and schema validation.
type Executor struct {
	xoluURL        string
	authMode       string
	token          string
	tenant         string
	timeout        time.Duration
	redactPayloads bool
	httpClient     *http.Client
	semanticStore  *semantic.SemanticStore
	healthProbe    *health.HealthProbe
}

// NewExecutor creates a new Executor.
func NewExecutor(
	xoluURL, authMode, token, tenant string,
	timeout time.Duration,
	redact bool,
	semStore *semantic.SemanticStore,
	probe *health.HealthProbe,
) *Executor {
	return &Executor{
		xoluURL:        strings.TrimRight(xoluURL, "/"),
		authMode:       authMode,
		token:          token,
		tenant:         tenant,
		timeout:        timeout,
		redactPayloads: redact,
		httpClient:     &http.Client{Timeout: timeout},
		semanticStore:  semStore,
		healthProbe:    probe,
	}
}

// CheckGated validates that xolu is healthy before sending requests.
func (e *Executor) CheckGated() *ToolError {
	if err := e.healthProbe.GatedCheck(); err != nil {
		status := e.healthProbe.Status()

		// Distinguish "never came up" from "was up, now down": if no successful
		// pong has ever been observed, molu is still in its startup wait rather
		// than recovering from a mid-run outage (spec Part 2 §8.5).
		code := ErrCodeUnavailable
		message := "xolu substrate is currently unreachable; retrying"
		if status.LastPongAt.IsZero() {
			code = ErrCodeStartup
			message = "molu is still waiting for xolu's first successful pong at startup"
		}

		return &ToolError{
			Code:    code,
			Message: message,
			Detail: map[string]interface{}{
				"last_pong_at":      status.LastPongAt.Format(time.RFC3339),
				"last_fail_at":      status.LastFailAt.Format(time.RFC3339),
				"consecutive_fails": status.ConsecutiveFails,
				"next_retry_at":     status.NextRetryAt.Format(time.RFC3339),
			},
		}
	}
	return nil
}

// Describe implements the describe MCP tool.
func (e *Executor) Describe(ctx context.Context, scope, name string) (interface{}, error) {
	semMap := e.semanticStore.Get()
	res, err := semMap.Describe(scope, name)
	if err != nil {
		return nil, &ToolError{
			Code:    ErrCodeContract,
			Message: err.Error(),
		}
	}
	return res, nil
}

// Get retrieves an entity by type and id.
func (e *Executor) Get(ctx context.Context, entityType string, id interface{}) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	semMap := e.semanticStore.Get()
	if def := semMap.FindEntity(entityType); def == nil {
		return nil, &ToolError{
			Code:    ErrCodeContract,
			Message: fmt.Sprintf("unknown entity type %q", entityType),
		}
	}

	path := fmt.Sprintf("/api/v1/%s/%v", url.PathEscape(entityType), id)
	return e.doRequest(ctx, http.MethodGet, path, nil)
}

// List retrieves entities of a given type with optional filters.
func (e *Executor) List(ctx context.Context, entityType string, filter map[string]interface{}, limit, offset int) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	semMap := e.semanticStore.Get()
	if def := semMap.FindEntity(entityType); def == nil {
		return nil, &ToolError{
			Code:    ErrCodeContract,
			Message: fmt.Sprintf("unknown entity type %q", entityType),
		}
	}

	params := url.Values{}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	for k, v := range filter {
		params.Set(k, fmt.Sprintf("%v", v))
	}

	path := fmt.Sprintf("/api/v1/%s?%s", url.PathEscape(entityType), params.Encode())
	return e.doRequest(ctx, http.MethodGet, path, nil)
}

// Create creates a new entity.
func (e *Executor) Create(ctx context.Context, entityType string, data map[string]interface{}) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	semMap := e.semanticStore.Get()
	def := semMap.FindEntity(entityType)
	if def == nil {
		return nil, &ToolError{
			Code:    ErrCodeContract,
			Message: fmt.Sprintf("unknown entity type %q", entityType),
		}
	}

	// Validate required fields if defined in Fields
	for _, f := range def.Fields {
		if f.Required {
			if _, ok := data[f.Name]; !ok {
				return nil, &ToolError{
					Code:    ErrCodeContract,
					Message: fmt.Sprintf("missing required field %q for entity %q", f.Name, entityType),
				}
			}
		}
	}

	path := fmt.Sprintf("/api/v1/%s", url.PathEscape(entityType))
	return e.doRequest(ctx, http.MethodPost, path, data)
}

// Update updates fields on an existing entity.
func (e *Executor) Update(ctx context.Context, entityType string, id interface{}, changes map[string]interface{}, version *int) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	semMap := e.semanticStore.Get()
	if def := semMap.FindEntity(entityType); def == nil {
		return nil, &ToolError{
			Code:    ErrCodeContract,
			Message: fmt.Sprintf("unknown entity type %q", entityType),
		}
	}

	body := map[string]interface{}{
		"changes": changes,
	}
	if version != nil {
		body["version"] = *version
	}

	path := fmt.Sprintf("/api/v1/%s/%v", url.PathEscape(entityType), id)
	return e.doRequest(ctx, http.MethodPatch, path, body)
}

// Query executes an OQL or Sulpher query against xolu.
func (e *Executor) Query(ctx context.Context, language, query string, params map[string]interface{}) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	body := map[string]interface{}{
		"language": language,
		"query":    query,
		"params":   params,
	}

	path := "/api/v1/query"
	return e.doRequest(ctx, http.MethodPost, path, body)
}

// Walk advances an FSM machine by input.
func (e *Executor) Walk(ctx context.Context, machineID interface{}, input string, payload map[string]interface{}) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	body := map[string]interface{}{
		"input":   input,
		"payload": payload,
	}

	path := fmt.Sprintf("/api/v2/fsm/machine/%v/walk", machineID)
	return e.doRequest(ctx, http.MethodPost, path, body)
}

// MachineState retrieves current state and variables for a machine.
func (e *Executor) MachineState(ctx context.Context, machineID interface{}) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	statePath := fmt.Sprintf("/api/v2/fsm/machine/%v/state", machineID)
	stateData, err := e.doRequest(ctx, http.MethodGet, statePath, nil)
	if err != nil {
		return nil, err
	}

	varsPath := fmt.Sprintf("/api/v2/fsm/machine/%v/vars", machineID)
	varsData, _ := e.doRequest(ctx, http.MethodGet, varsPath, nil)

	return map[string]interface{}{
		"machine_id": machineID,
		"state":      stateData,
		"vars":       varsData,
	}, nil
}

// MachineHistory retrieves transition history for an FSM machine.
func (e *Executor) MachineHistory(ctx context.Context, machineID interface{}, limit int) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	path := fmt.Sprintf("/api/v2/fsm/machine/%v/history?limit=%d", machineID, limit)
	return e.doRequest(ctx, http.MethodGet, path, nil)
}

// CalCheck performs dry-run availability check on calendars.
func (e *Executor) CalCheck(ctx context.Context, calendars []string, start, end string) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	body := map[string]interface{}{
		"calendars": calendars,
		"start":     start,
		"end":       end,
	}
	return e.doRequest(ctx, http.MethodPost, "/api/v1/cal/check", body)
}

// CalOpenings finds open slots across calendars.
func (e *Executor) CalOpenings(ctx context.Context, calendars []string, duration, from, to, objective string, limit int) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	body := map[string]interface{}{
		"calendars": calendars,
		"duration":  duration,
		"from":      from,
		"to":        to,
		"objective": objective,
		"limit":     limit,
	}
	return e.doRequest(ctx, http.MethodPost, "/api/v1/cal/openings", body)
}

// CalPropose creates a proposed booking.
func (e *Executor) CalPropose(ctx context.Context, calendars []string, start, end string, payload map[string]interface{}) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	body := map[string]interface{}{
		"calendars": calendars,
		"start":     start,
		"end":       end,
		"payload":   payload,
	}
	return e.doRequest(ctx, http.MethodPost, "/api/v1/cal/propose", body)
}

// CalConfirm confirms a proposed booking.
func (e *Executor) CalConfirm(ctx context.Context, bookingID interface{}) (interface{}, error) {
	if gErr := e.CheckGated(); gErr != nil {
		return nil, gErr
	}

	body := map[string]interface{}{
		"booking_id": bookingID,
	}
	return e.doRequest(ctx, http.MethodPost, "/api/v1/cal/confirm", body)
}

// Helper to execute authenticated HTTP calls to xolu
func (e *Executor) doRequest(ctx context.Context, method, path string, payload interface{}) (interface{}, error) {
	reqURL := e.xoluURL + path
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, &ToolError{
				Code:    ErrCodeContract,
				Message: fmt.Sprintf("failed to encode request payload: %v", err),
			}
		}
		bodyReader = bytes.NewReader(data)
	}

	reqCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, reqURL, bodyReader)
	if err != nil {
		return nil, &ToolError{
			Code:    ErrCodeContract,
			Message: fmt.Sprintf("failed to construct request: %v", err),
		}
	}

	req.Header.Set("Content-Type", "application/json")
	if e.tenant != "" {
		req.Header.Set("X-Tenant-ID", e.tenant)
	}

	if e.token != "" {
		if e.authMode == "apikey" {
			req.Header.Set("X-API-Key", e.token)
		} else {
			req.Header.Set("Authorization", "Bearer "+e.token)
		}
	}

	start := time.Now()
	resp, err := e.httpClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		slog.Error("Substrate request failed",
			"method", method,
			"path", path,
			"duration_ms", duration.Milliseconds(),
			"error", err)

		if strings.Contains(err.Error(), "context deadline exceeded") {
			return nil, &ToolError{
				Code:    ErrCodeTimeout,
				Message: fmt.Sprintf("request to xolu timed out after %v", e.timeout),
			}
		}
		return nil, &ToolError{
			Code:    ErrCodeUnavailable,
			Message: fmt.Sprintf("failed to communicate with xolu: %v", err),
		}
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ToolError{
			Code:    ErrCodeUnavailable,
			Message: fmt.Sprintf("failed to read response body: %v", err),
		}
	}

	var parsedResp interface{}
	if len(respBytes) > 0 {
		_ = json.Unmarshal(respBytes, &parsedResp)
	}

	slog.Info("Executed tool operation against substrate",
		"method", method,
		"path", path,
		"status", resp.StatusCode,
		"duration_ms", duration.Milliseconds(),
		"payload", obs.RedactIfEnabled(payload, e.redactPayloads),
	)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Pass-through substrate error code if present
		errCode := fmt.Sprintf("XOLU-HTTP-%d", resp.StatusCode)
		errMsg := string(respBytes)

		if m, ok := parsedResp.(map[string]interface{}); ok {
			if code, ok := m["code"].(string); ok && code != "" {
				errCode = code
			}
			if msg, ok := m["message"].(string); ok && msg != "" {
				errMsg = msg
			}
		}

		return nil, &ToolError{
			Code:    errCode,
			Message: errMsg,
			Detail: map[string]interface{}{
				"status": resp.StatusCode,
				"body":   parsedResp,
			},
		}
	}

	return parsedResp, nil
}

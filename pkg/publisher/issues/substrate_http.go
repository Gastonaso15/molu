package issues

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// HTTPSubstrate implements XoluClient talking to xolu REST API.
type HTTPSubstrate struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewHTTPSubstrate constructs an HTTPSubstrate.
func NewHTTPSubstrate(baseURL, token string, timeout time.Duration) *HTTPSubstrate {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &HTTPSubstrate{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: timeout},
	}
}

func (h *HTTPSubstrate) GetEntity(ctx context.Context, tenant, entityType string, id int64) (map[string]any, error) {
	path := fmt.Sprintf("/api/v1/%s/%d", url.PathEscape(entityType), id)
	var out map[string]any
	if err := h.do(ctx, tenant, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if inner, ok := out["data"].(map[string]any); ok {
		return inner, nil
	}
	return out, nil
}

func (h *HTTPSubstrate) CreateEntity(ctx context.Context, tenant, entityType string, data map[string]any) (int64, error) {
	path := fmt.Sprintf("/api/v1/%s", url.PathEscape(entityType))
	var created map[string]any
	if err := h.do(ctx, tenant, http.MethodPost, path, data, &created); err != nil {
		return 0, err
	}
	id := toInt64(created["id"])
	if id == 0 {
		if inner, ok := created["data"].(map[string]any); ok {
			id = toInt64(inner["id"])
		}
	}
	if id == 0 {
		return 0, fmt.Errorf("xolu created %s without an id", entityType)
	}
	return id, nil
}

func (h *HTTPSubstrate) PatchEntity(ctx context.Context, tenant, entityType string, id int64, patch map[string]any) error {
	path := fmt.Sprintf("/api/v1/%s/%d", url.PathEscape(entityType), id)
	return h.do(ctx, tenant, http.MethodPatch, path, patch, nil)
}

func (h *HTTPSubstrate) EnsureMachineDefinition(ctx context.Context, tenant string) (int64, error) {
	var listed any
	if err := h.do(ctx, tenant, http.MethodGet, "/api/v2/fsm/def", nil, &listed); err == nil {
		for _, item := range extractItems(listed) {
			if n, _ := item["name"].(string); n == DefinitionName {
				if id := toInt64(item["id"]); id > 0 {
					return id, nil
				}
			}
		}
	}

	// Definition of the 8-state lifecycle
	def := map[string]any{
		"name":        DefinitionName,
		"description": "Issue Tracker lifecycle (8 states)",
		"initial":     "reported",
		"determinism": "firstmatch",
		"states": map[string]any{
			"reported":    map[string]any{"terminal": false},
			"triaged":     map[string]any{"terminal": false},
			"in_progress": map[string]any{"terminal": false},
			"on_hold":     map[string]any{"terminal": false},
			"resolved":    map[string]any{"terminal": false},
			"closed":      map[string]any{"terminal": true},
			"rejected":    map[string]any{"terminal": true},
			"duplicate":   map[string]any{"terminal": true},
		},
		"transitions": []map[string]any{
			{"from": "reported", "input": "triage", "to": "triaged"},
			{"from": []any{"reported", "triaged"}, "input": "start", "to": "in_progress"},
			{"from": []any{"triaged", "in_progress"}, "input": "hold", "to": "on_hold"},
			{"from": "on_hold", "input": "resume", "to": "in_progress"},
			{"from": "in_progress", "input": "resolve", "to": "resolved"},
			{"from": "resolved", "input": "close", "to": "closed"},
			{"from": []any{"reported", "triaged", "in_progress", "on_hold", "resolved"}, "input": "reject", "to": "rejected"},
		},
	}

	var created map[string]any
	if err := h.do(ctx, tenant, http.MethodPost, "/api/v2/fsm/def", def, &created); err != nil {
		return 0, err
	}
	id := toInt64(created["id"])
	if id == 0 {
		return 0, fmt.Errorf("xolu created definition %q without an id", DefinitionName)
	}
	return id, nil
}

func (h *HTTPSubstrate) CreateMachine(ctx context.Context, tenant string, defID int64) (int64, string, error) {
	body := map[string]any{"definition": defID}
	var out map[string]any
	if err := h.do(ctx, tenant, http.MethodPost, "/api/v2/fsm/machine", body, &out); err != nil {
		return 0, "", err
	}
	id := toInt64(out["id"])
	state, _ := out["state"].(string)
	if state == "" {
		state, _ = out["current"].(string)
	}
	if state == "" {
		state = "reported"
	}
	return id, state, nil
}

func (h *HTTPSubstrate) WalkMachine(ctx context.Context, tenant string, machineID int64, input string, payload map[string]any) (string, error) {
	path := fmt.Sprintf("/api/v2/fsm/machine/%d/walk", machineID)
	body := map[string]any{"input": input, "payload": payload}
	var out map[string]any
	if err := h.do(ctx, tenant, http.MethodPost, path, body, &out); err != nil {
		return "", err
	}
	nextState, _ := out["current"].(string)
	if nextState == "" {
		nextState, _ = out["state"].(string)
	}
	return nextState, nil
}

func (h *HTTPSubstrate) do(ctx context.Context, tenant, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, h.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	if tenant != "" {
		req.Header.Set("X-Tenant-ID", tenant)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return taxonomy.NewSubstrateUnavailable(taxonomy.NowRFC3339(), taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 5*time.Second), 1, 3)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return taxonomy.NewNotFound(tenant, path)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		var xErr struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
			Error   *struct {
				Code    string         `json:"code"`
				Message string         `json:"message"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &xErr) == nil {
			code := xErr.Code
			msg := xErr.Message
			details := xErr.Details
			if xErr.Error != nil {
				if code == "" {
					code = xErr.Error.Code
				}
				if msg == "" {
					msg = xErr.Error.Message
				}
				if details == nil {
					details = xErr.Error.Details
				}
			}
			if strings.Contains(code, "INVALID_TRANSITION") || resp.StatusCode == http.StatusConflict {
				if !strings.HasPrefix(code, "XOLU-") {
					code = "XOLU-INVALID_TRANSITION"
				}
				return &taxonomy.TypedError{
					Code:    code,
					Message: msg,
					Details: details,
				}
			}
		}
		if resp.StatusCode >= 500 {
			return taxonomy.NewSubstrateUnavailable(taxonomy.NowRFC3339(), taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 5*time.Second), 1, 3)
		}
		return fmt.Errorf("xolu status %d: %s", resp.StatusCode, string(raw))
	}

	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func extractItems(v any) []map[string]any {
	switch val := v.(type) {
	case []any:
		var res []map[string]any
		for _, it := range val {
			if m, ok := it.(map[string]any); ok {
				res = append(res, m)
			}
		}
		return res
	case map[string]any:
		if items, ok := val["items"].([]any); ok {
			var res []map[string]any
			for _, it := range items {
				if m, ok := it.(map[string]any); ok {
					res = append(res, m)
				}
			}
			return res
		}
	}
	return nil
}

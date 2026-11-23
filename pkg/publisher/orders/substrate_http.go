package orders

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTPSubstrate talks to a real xolu over its REST API. Transport failures are
// returned as-is; non-2xx answers become *XoluError so the service can
// classify them.
type HTTPSubstrate struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPSubstrate(baseURL, token string, timeout time.Duration) *HTTPSubstrate {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &HTTPSubstrate{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Timeout: timeout}}
}

func (h *HTTPSubstrate) EnsureDefinition(ctx context.Context, name string, def Definition) (string, error) {
	var listed any
	if err := h.do(ctx, http.MethodGet, "/api/v2/fsm/def", nil, &listed); err != nil {
		return "", err
	}
	for _, item := range definitionItems(listed) {
		if n, _ := item["name"].(string); n == name {
			if id := idString(item["id"]); id != "" {
				return id, nil
			}
		}
	}
	var created map[string]any
	if err := h.do(ctx, http.MethodPost, "/api/v2/fsm/def", def, &created); err != nil {
		return "", err
	}
	id := idString(created["id"])
	if id == "" {
		return "", fmt.Errorf("xolu created definition %q without an id", name)
	}
	return id, nil
}

// CreateMachine mints a machine from a definition. It is not bound to an
// entity: like seam-CURE, the order document points at it through machine_id.
func (h *HTTPSubstrate) CreateMachine(ctx context.Context, definitionID string) (Machine, error) {
	body := map[string]any{"definition": numericOrString(definitionID)}
	var out map[string]any
	if err := h.do(ctx, http.MethodPost, "/api/v2/fsm/machine", body, &out); err != nil {
		return Machine{}, err
	}
	m := machineFrom(out)
	if m.ID == "" {
		return Machine{}, fmt.Errorf("xolu created a machine without an id")
	}
	return m, nil
}

func (h *HTTPSubstrate) CreateEntity(ctx context.Context, entityType string, data map[string]any) (string, error) {
	var created map[string]any
	if err := h.do(ctx, http.MethodPost, "/api/v1/"+url.PathEscape(entityType), data, &created); err != nil {
		return "", err
	}
	id := idString(created["id"])
	if id == "" {
		return "", fmt.Errorf("xolu created %s without an id", entityType)
	}
	return id, nil
}

func (h *HTTPSubstrate) GetEntity(ctx context.Context, entityType, id string) (map[string]any, error) {
	var out map[string]any
	if err := h.do(ctx, http.MethodGet, "/api/v1/"+url.PathEscape(entityType)+"/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	if inner, ok := out["data"].(map[string]any); ok {
		return inner, nil
	}
	return out, nil
}

func (h *HTTPSubstrate) PatchEntity(ctx context.Context, entityType, id string, data map[string]any) error {
	return h.do(ctx, http.MethodPatch, "/api/v1/"+url.PathEscape(entityType)+"/"+url.PathEscape(id), data, nil)
}

// ListEntities reads every document through OQL, the route seam-CURE uses for
// its own lists.
func (h *HTTPSubstrate) ListEntities(ctx context.Context, entityType string) ([]map[string]any, error) {
	var out struct {
		Data []map[string]any `json:"data"`
	}
	q := map[string]any{"query": "SELECT TOP 100 * FROM " + entityType + " ORDER BY id DESC"}
	if err := h.do(ctx, http.MethodPost, "/api/v1/oql/query", q, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// FindEntities builds an OQL SELECT with the conditions in q. Field names and
// operators are checked before anything is sent, and string values are quoted
// with doubled single quotes, so a value can never alter the query.
func (h *HTTPSubstrate) FindEntities(ctx context.Context, entityType string, q EntityQuery) ([]map[string]any, error) {
	if !safeIdent(entityType) {
		return nil, fmt.Errorf("invalid entity name %q", entityType)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "SELECT TOP %d * FROM %s", limit, entityType)
	for i, c := range q.Where {
		if !safeIdent(c.Field) {
			return nil, fmt.Errorf("invalid field name %q", c.Field)
		}
		if c.Op != "=" && c.Op != ">=" && c.Op != "<=" {
			return nil, fmt.Errorf("unsupported operator %q", c.Op)
		}
		lit, err := oqlLiteral(c.Value)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", c.Field, err)
		}
		if i == 0 {
			sb.WriteString(" WHERE ")
		} else {
			sb.WriteString(" AND ")
		}
		fmt.Fprintf(&sb, "%s %s %s", c.Field, c.Op, lit)
	}
	sb.WriteString(" ORDER BY created_at DESC, id DESC")
	var out struct {
		Data []map[string]any `json:"data"`
	}
	if err := h.do(ctx, http.MethodPost, "/api/v1/oql/query", map[string]any{"query": sb.String()}, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func safeIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func oqlLiteral(v any) (string, error) {
	switch t := v.(type) {
	case int:
		return strconv.Itoa(t), nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	case string:
		return "'" + strings.ReplaceAll(t, "'", "''") + "'", nil
	}
	return "", fmt.Errorf("unsupported value type %T", v)
}

func (h *HTTPSubstrate) Walk(ctx context.Context, machineID, input string, payload map[string]any) (WalkResult, error) {
	body := map[string]any{"input": input}
	if payload != nil {
		body["payload"] = payload
	}
	var out map[string]any
	if err := h.do(ctx, http.MethodPost, "/api/v2/fsm/machine/"+url.PathEscape(machineID)+"/walk", body, &out); err != nil {
		return WalkResult{}, err
	}
	res := WalkResult{}
	res.Previous, _ = out["previous"].(string)
	res.Current, _ = out["current"].(string)
	res.Terminal, _ = out["terminal"].(bool)
	if outs, ok := out["outputs"].([]any); ok {
		for _, o := range outs {
			if s, ok := o.(string); ok {
				res.Outputs = append(res.Outputs, s)
			}
		}
	}
	return res, nil
}

func (h *HTTPSubstrate) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.baseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return parseXoluError(resp.StatusCode, raw)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep ids exact; never render 1001 as 1001.0
	return dec.Decode(out)
}

// parseXoluError reads every error shape xolu emits: {"error":"XOLU-FSM004",...}
// (FSM routes), {"error":{"code","message"}} (nested) and flat {"code","message"}.
func parseXoluError(status int, raw []byte) *XoluError {
	xe := &XoluError{Status: status}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		xe.Message = strings.TrimSpace(string(raw))
		return xe
	}
	switch e := m["error"].(type) {
	case string:
		xe.Code = e
	case map[string]any:
		xe.Code, _ = e["code"].(string)
		xe.Message, _ = e["message"].(string)
		xe.Detail, _ = e["detail"].(string)
	}
	if xe.Code == "" {
		xe.Code, _ = m["code"].(string)
	}
	if xe.Message == "" {
		xe.Message, _ = m["message"].(string)
	}
	if xe.Detail == "" {
		xe.Detail, _ = m["detail"].(string)
	}
	xe.State, _ = m["state"].(string)
	if ai, ok := m["available_inputs"].([]any); ok {
		for _, v := range ai {
			if s, ok := v.(string); ok {
				xe.AvailableInputs = append(xe.AvailableInputs, s)
			}
		}
	}
	return xe
}

func machineFrom(m map[string]any) Machine {
	out := Machine{ID: idString(m["id"])}
	out.State, _ = m["state"].(string)
	out.Terminal, _ = m["terminal"].(bool)
	return out
}

func definitionItems(v any) []map[string]any {
	var arr []any
	switch t := v.(type) {
	case []any:
		arr = t
	case map[string]any:
		for _, k := range []string{"items", "data", "definitions", "defs", "results"} {
			if a, ok := t[k].([]any); ok {
				arr = a
				break
			}
		}
	}
	var out []map[string]any
	for _, it := range arr {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func idString(v any) string {
	switch t := v.(type) {
	case json.Number:
		return t.String()
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	}
	return ""
}

func numericOrString(id string) any {
	if n, err := strconv.ParseInt(id, 10, 64); err == nil {
		return n
	}
	return id
}

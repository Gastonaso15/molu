package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

// HTTPDomainCaller executes HTTP calls to domain function endpoints.
type HTTPDomainCaller struct {
	httpClient *http.Client
	resolver   SecretResolver
	timeout    time.Duration
	logger     telemetry.Logger
	metrics    telemetry.Metrics
}

// NewHTTPDomainCaller creates a new HTTPDomainCaller with sensible defaults.
func NewHTTPDomainCaller(client *http.Client, resolver SecretResolver, timeout time.Duration) *HTTPDomainCaller {
	if client == nil {
		client = &http.Client{}
	}
	if resolver == nil {
		resolver = NewEnvSecretResolver()
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &HTTPDomainCaller{
		httpClient: client,
		resolver:   resolver,
		timeout:    timeout,
	}
}

// SetLogger attaches telemetry logger to the caller.
func (c *HTTPDomainCaller) SetLogger(logger telemetry.Logger) {
	c.logger = logger
}

// SetMetrics attaches telemetry metrics to the caller.
func (c *HTTPDomainCaller) SetMetrics(metrics telemetry.Metrics) {
	c.metrics = metrics
}

// Call executes an HTTP POST to the given domain function endpoint.
func (c *HTTPDomainCaller) Call(ctx context.Context, endpoint string, auth AuthMetadata, args map[string]any) (any, error) {
	rc := telemetry.RequestContextFrom(ctx)

	// Resolve auth secret if required before performing HTTP call
	var secretToken string
	if auth.Mode == AuthModeBearer || auth.Mode == AuthModeAPIKey {
		if auth.TokenRef != "" {
			tok, err := c.resolver.Resolve(auth.TokenRef)
			if err != nil {
				return nil, taxonomy.NewContractViolation(rc.TenantID, rc.Tool, []string{"auth.token_ref: " + auth.TokenRef + " not found"})
			}
			secretToken = tok
		}
	}

	// Prepare request payload
	var bodyBytes []byte
	var err error
	if args != nil {
		bodyBytes, err = json.Marshal(args)
		if err != nil {
			return nil, taxonomy.NewContractViolation(rc.TenantID, rc.Tool, []string{"invalid argument payload"})
		}
	} else {
		bodyBytes = []byte("{}")
	}

	reqCtx := ctx
	if c.timeout > 0 {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Set auth header
	switch auth.Mode {
	case AuthModeBearer:
		header := auth.Header
		if header == "" {
			header = "Authorization"
		}
		req.Header.Set(header, "Bearer "+secretToken)
	case AuthModeAPIKey:
		header := auth.Header
		if header == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, secretToken)
	}

	// Set correlation headers
	if rc.RequestID != "" {
		req.Header.Set("X-Request-ID", rc.RequestID)
	}
	if rc.TraceID != "" {
		spanID := rc.SpanID
		if spanID == "" {
			spanID = "0000000000000001"
		}
		req.Header.Set("traceparent", fmt.Sprintf("00-%s-%s-01", rc.TraceID, spanID))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, taxonomy.NewSubstrateUnavailable(
			taxonomy.NowRFC3339(),
			taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 5*time.Second),
			1, 3,
		)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, taxonomy.NewSubstrateUnavailable(
			taxonomy.NowRFC3339(),
			taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 5*time.Second),
			1, 3,
		)
	}

	// Check for xolu verbatim error envelope first
	if xoluErr := parseXoluError(respBytes); xoluErr != nil {
		return nil, xoluErr
	}

	// Check status codes
	if resp.StatusCode >= 500 {
		return nil, taxonomy.NewSubstrateUnavailable(
			taxonomy.NowRFC3339(),
			taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 5*time.Second),
			1, 3,
		)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, taxonomy.NewNotFound(rc.TenantID, endpoint)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("domain publisher error (%d): %s", resp.StatusCode, string(respBytes))
	}

	// Decode JSON response
	var decoded any
	if err := json.Unmarshal(respBytes, &decoded); err != nil {
		// Non-JSON response (e.g. HTML 502 page from a reverse proxy)
		return nil, taxonomy.NewSubstrateUnavailable(
			taxonomy.NowRFC3339(),
			taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 5*time.Second),
			1, 3,
		)
	}

	// Check for empty collection
	if isEmptyResponse(decoded) {
		return nil, taxonomy.NewEmpty(rc.TenantID, rc.Tool, args)
	}

	return decoded, nil
}

func parseXoluError(body []byte) *taxonomy.TypedError {
	var envelope struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}

	code := envelope.Code
	msg := envelope.Message
	details := envelope.Details
	if envelope.Error.Code != "" {
		code = envelope.Error.Code
		msg = envelope.Error.Message
		details = envelope.Error.Details
	}

	if strings.HasPrefix(code, "XOLU-") {
		return &taxonomy.TypedError{
			Code:    code,
			Message: msg,
			Details: details,
		}
	}
	return nil
}

func isEmptyResponse(data any) bool {
	switch v := data.(type) {
	case []any:
		return len(v) == 0
	case map[string]any:
		for _, key := range []string{"data", "items", "rows", "results"} {
			if val, ok := v[key]; ok {
				if slice, ok := val.([]any); ok && len(slice) == 0 {
					return true
				}
			}
		}
		if count, ok := v["count"].(float64); ok && count == 0 {
			return true
		}
		if count, ok := v["count"].(int); ok && count == 0 {
			return true
		}
		if total, ok := v["total"].(float64); ok && total == 0 {
			return true
		}
		if total, ok := v["total"].(int); ok && total == 0 {
			return true
		}
	}
	return false
}


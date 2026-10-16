package xolu

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

type WalkRequest struct {
	Tool     string
	Input    map[string]any
	TenantID string
	Context  map[string]any
}

type WalkResponse struct {
	Output    map[string]any
	NextState string
}

type FindRequest struct {
	Query    map[string]any
	TenantID string
	Limit    int
	Offset   int
}

type FindResponse struct {
	Results []map[string]any
	Total   int
}

type GetRequest struct {
	ID       string
	TenantID string
}

type GetResponse struct {
	Object map[string]any
}

// TypedError is used in integration tests to simulate xolu errors
type TypedError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *TypedError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.Code + ": " + e.Message
}

type XoluClient interface {
	Walk(ctx context.Context, req WalkRequest) (WalkResponse, error)
	Find(ctx context.Context, req FindRequest) (FindResponse, error)
	Get(ctx context.Context, req GetRequest) (GetResponse, error)
	Ping(ctx context.Context) error
	Ready(ctx context.Context) error
	SetLogger(logger telemetry.Logger)
	SetMetrics(metrics telemetry.Metrics)
}

type HTTPXoluClient struct {
	baseURL    string
	httpClient *http.Client
	logger     telemetry.Logger
	metrics    telemetry.Metrics
}

func NewHTTPXoluClient(baseURL string, timeout time.Duration) *HTTPXoluClient {
	return &HTTPXoluClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *HTTPXoluClient) SetLogger(logger telemetry.Logger) {
	c.logger = logger
}

func (c *HTTPXoluClient) SetMetrics(metrics telemetry.Metrics) {
	c.metrics = metrics
}

func (c *HTTPXoluClient) Walk(ctx context.Context, req WalkRequest) (WalkResponse, error) {
	start := time.Now()
	ctxLog := c.logger
	if ctxLog != nil {
		ctxLog = ctxLog.WithContext(ctx).WithFields(map[string]any{
			"tool":      req.Tool,
			"tenant_id": req.TenantID,
		})
		ctxLog.Application().Info().Msg("xolu walk start")
	}

	resp, err := c.doRequest(ctx, "POST", "/walk", req, &WalkResponse{})

	latencyMs := float64(time.Since(start).Milliseconds())
	code := taxonomy.CodeFromError(err)

	if c.metrics != nil {
		c.metrics.ObserveLatency(telemetry.ComponentXoluClient, "walk", latencyMs)
		c.metrics.IncRequests(telemetry.ComponentXoluClient, "walk", req.Tool, code)
		if err != nil {
			c.metrics.IncErrors(telemetry.ComponentXoluClient, code)
		}
	}

	if ctxLog != nil {
		if err != nil {
			ctxLog.Application().Error().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu walk failed")
		} else {
			ctxLog.Application().Info().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu walk completed")
		}
	}

	if err != nil {
		return WalkResponse{}, err
	}

	return *resp.(*WalkResponse), nil
}

func (c *HTTPXoluClient) Find(ctx context.Context, req FindRequest) (FindResponse, error) {
	start := time.Now()
	ctxLog := c.logger
	if ctxLog != nil {
		ctxLog = ctxLog.WithContext(ctx).WithFields(map[string]any{
			"tenant_id": req.TenantID,
			"limit":     req.Limit,
			"offset":    req.Offset,
		})
		ctxLog.Application().Info().Msg("xolu find start")
	}

	resp, err := c.doRequest(ctx, "POST", "/find", req, &FindResponse{})

	latencyMs := float64(time.Since(start).Milliseconds())
	code := taxonomy.CodeFromError(err)

	if c.metrics != nil {
		c.metrics.ObserveLatency(telemetry.ComponentXoluClient, "find", latencyMs)
		c.metrics.IncRequests(telemetry.ComponentXoluClient, "find", "find", code)
		if err != nil {
			c.metrics.IncErrors(telemetry.ComponentXoluClient, code)
		}
	}

	if ctxLog != nil {
		if err != nil {
			ctxLog.Application().Error().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu find failed")
		} else {
			ctxLog.Application().Info().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu find completed")
		}
	}

	if err != nil {
		return FindResponse{}, err
	}

	return *resp.(*FindResponse), nil
}

func (c *HTTPXoluClient) Get(ctx context.Context, req GetRequest) (GetResponse, error) {
	start := time.Now()
	ctxLog := c.logger
	if ctxLog != nil {
		ctxLog = ctxLog.WithContext(ctx).WithFields(map[string]any{
			"id":        req.ID,
			"tenant_id": req.TenantID,
		})
		ctxLog.Application().Info().Msg("xolu get start")
	}

	resp, err := c.doRequest(ctx, "POST", "/get", req, &GetResponse{})

	latencyMs := float64(time.Since(start).Milliseconds())
	code := taxonomy.CodeFromError(err)

	if c.metrics != nil {
		c.metrics.ObserveLatency(telemetry.ComponentXoluClient, "get", latencyMs)
		c.metrics.IncRequests(telemetry.ComponentXoluClient, "get", "get", code)
		if err != nil {
			c.metrics.IncErrors(telemetry.ComponentXoluClient, code)
		}
	}

	if ctxLog != nil {
		if err != nil {
			ctxLog.Application().Error().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu get failed")
		} else {
			ctxLog.Application().Info().
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("xolu get completed")
		}
	}

	if err != nil {
		return GetResponse{}, err
	}

	return *resp.(*GetResponse), nil
}

func (c *HTTPXoluClient) Ping(ctx context.Context) error {
	start := time.Now()
	ctxLog := c.logger
	if ctxLog != nil {
		ctxLog = ctxLog.WithContext(ctx)
		ctxLog.Network().Info().Msg("xolu ping start")
	}

	_, err := c.doRequest(ctx, "GET", "/ready", nil, nil)

	latencyMs := float64(time.Since(start).Milliseconds())

	if c.metrics != nil {
		c.metrics.ObserveLatency(telemetry.ComponentXoluClient, "ping", latencyMs)
	}

	if ctxLog != nil {
		if err != nil {
			ctxLog.Network().Error().
				Str("code", taxonomy.CodeFromError(err)).
				Float64("latency_ms", latencyMs).
				Msg("xolu ping failed")
		} else {
			ctxLog.Network().Info().
				Float64("latency_ms", latencyMs).
				Msg("xolu ping completed")
		}
	}

	return err
}

func (c *HTTPXoluClient) Ready(ctx context.Context) error {
	return c.Ping(ctx)
}

func (c *HTTPXoluClient) doRequest(ctx context.Context, method, path string, body any, dest any) (any, error) {
	url := c.baseURL + path

	var bodyReader *bytes.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(bodyBytes)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Propagate correlation headers
	if c.logger != nil {
		// We can't easily get request_id/trace_id from context here without
		// changing the signature, but the middleware adds them to the request
		// headers before calling the handler. The HTTP client would read them.
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return nil, taxonomy.NewSubstrateUnavailable(
			time.Now().UTC().Format(time.RFC3339),
			time.Now().UTC().Add(30*time.Second).Format(time.RFC3339),
			1, 3,
		)
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
			return nil, &taxonomy.TypedError{
				Code:    errResp.Code,
				Message: errResp.Message,
				Details: errResp.Details,
			}
		}
		return nil, taxonomy.NewContractViolation("", "", []string{"http_error"})
	}

	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return nil, err
		}
		return dest, nil
	}

	return nil, nil
}

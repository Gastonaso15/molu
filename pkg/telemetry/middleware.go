package telemetry

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/oklog/ulid/v2"
)

// Middleware handles MCP request correlation.
type Middleware struct {
	logger Logger
	entropy *ulid.MonotonicEntropy
}

// NewMiddleware creates a new MCP middleware.
func NewMiddleware(logger Logger) *Middleware {
	return &Middleware{
		logger:  logger,
		entropy: ulid.Monotonic(ulid.DefaultEntropy(), 0),
	}
}

// Wrap wraps an HTTP handler with correlation ID extraction and propagation.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract or generate request_id (ULID - time-ordered like UUID v7)
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = ulid.MustNew(ulid.Now(), m.entropy).String()
		}

		// Extract or generate trace_id from W3C traceparent
		traceID, spanID := extractTraceParent(r.Header.Get("traceparent"))

		// Extract method and tool from MCP request
		method, tool := extractMCPInfo(r)

		// Create request context
		rc := &RequestContext{
			RequestID: requestID,
			TraceID:   traceID,
			SpanID:    spanID,
			Method:    method,
			Tool:      tool,
		}

		// Add to response headers for client correlation
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Trace-ID", traceID)

		// Continue with enriched context
		ctx := WithRequestContext(r.Context(), rc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractTraceParent parses W3C traceparent header.
// Format: version-trace-id-parent-id-flags
// Example: 00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
func extractTraceParent(header string) (traceID, spanID string) {
	if header == "" {
		// Generate new trace ID (32 hex chars) and span ID (16 hex chars)
		traceID = ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()).String()
		spanID = ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()).String()[:16]
		return traceID, spanID
	}

	parts := strings.Split(header, "-")
	if len(parts) < 3 {
		traceID = ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()).String()
		spanID = ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()).String()[:16]
		return traceID, spanID
	}

	return parts[1], parts[2]
}

// extractMCPInfo extracts method and tool name from MCP request body.
func extractMCPInfo(r *http.Request) (method, tool string) {
	// Default values
	method = "tools/call"
	tool = ""

	// Try to parse JSON body for MCP requests
	if r.Body != nil && strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		body, err := io.ReadAll(r.Body)
		if err == nil && len(body) > 0 {
			// Restore body for downstream handlers
			r.Body = io.NopCloser(strings.NewReader(string(body)))

			var mcpReq struct {
				Method string `json:"method"`
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			if json.Unmarshal(body, &mcpReq) == nil {
				if mcpReq.Method != "" {
					method = mcpReq.Method
				}
				if mcpReq.Params.Name != "" {
					tool = mcpReq.Params.Name
				}
			}
		}
	}

	return method, tool
}

// MiddlewareFunc is a function that wraps handlers with middleware.
type MiddlewareFunc func(http.Handler) http.Handler

// Chain applies multiple middleware functions in order.
func Chain(middlewares ...MiddlewareFunc) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}
package telemetry

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Middleware handles MCP request correlation and structured JSON logging.
type Middleware struct {
	logger Logger
}

// NewMiddleware creates a new MCP middleware.
func NewMiddleware(logger Logger) *Middleware {
	return &Middleware{
		logger: logger,
	}
}

// wrappingResponseWriter captures the HTTP status code written by a handler.
type wrappingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (wr *wrappingResponseWriter) WriteHeader(status int) {
	wr.status = status
	wr.ResponseWriter.WriteHeader(status)
}

// Wrap wraps an HTTP handler with correlation ID extraction, propagation, and structured JSON logging.
// It logs incoming MCP requests to the network domain and outgoing responses with latency.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract or generate request_id (ULID - time-ordered like UUID v7)
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = ulid.MustNew(ulid.Now(), rand.Reader).String()
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

	// Log incoming MCP request (Network domain, RF-2)
		ctx := WithRequestContext(r.Context(), rc)
		ctxLogger := m.logger.WithContext(ctx)
		ctxLogger.Network().Info().
			Str("method", method).
			Str("tool", tool).
			Str("tenant_id", rc.TenantID).
			Msg("incoming mcp request")

		// Wrap response writer to capture status code
		wr := &wrappingResponseWriter{ResponseWriter: w}

		// Add to response headers for client correlation
		wr.Header().Set("X-Request-ID", requestID)
		wr.Header().Set("X-Trace-ID", traceID)

		// Continue with enriched context, measuring latency
		ctx = WithRequestContext(r.Context(), rc)
		start := time.Now()
		next.ServeHTTP(wr, r.WithContext(ctx))
		latency := time.Since(start).Milliseconds()

		// Log outgoing MCP response (Network domain, RF-4)
		ctxLogger2 := m.logger.WithContext(ctx)
		ctxLogger2.Network().Info().
			Int("latency_ms", int(latency)).
			Int("status", wr.status).
			Str("method", method).
			Str("tool", tool).
			Msg("outgoing mcp response")
	})
}

// extractTraceParent parses W3C traceparent header.
// Format: version-trace-id-parent-id-flags
// Example: 00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
func extractTraceParent(header string) (traceID, spanID string) {
	if header == "" {
		// Generate new trace ID (32 hex chars) and span ID (16 hex chars)
		traceID = ulid.MustNew(ulid.Now(), rand.Reader).String()
		spanID = ulid.MustNew(ulid.Now(), rand.Reader).String()[:16]
		return traceID, spanID
	}

	parts := strings.Split(header, "-")
	if len(parts) < 3 {
		traceID = ulid.MustNew(ulid.Now(), rand.Reader).String()
		spanID = ulid.MustNew(ulid.Now(), rand.Reader).String()[:16]
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

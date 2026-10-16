package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddleware_RequestID(t *testing.T) {
	t.Parallel()

	logger := NewLogger(Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	})

	middleware := NewMiddleware(logger)

	var buf strings.Builder
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"tools/call","params":{"name":"walk"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Wrap a simple handler
	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := RequestContextFrom(r.Context())
		if rc.RequestID == "" {
			buf.WriteString("missing request_id")
		} else if !isValidULID(rc.RequestID) {
			buf.WriteString("invalid request_id format (not ULID)")
		}
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	if buf.Len() > 0 {
		t.Errorf("request_id validation failed: %s", buf.String())
	}
}

func TestMiddleware_TraceParent(t *testing.T) {
	t.Parallel()

	logger := NewLogger(Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	})

	middleware := NewMiddleware(logger)

	// Test with existing traceparent
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")

	var traceID, spanID string
	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := RequestContextFrom(r.Context())
		traceID = rc.TraceID
		spanID = rc.SpanID
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if traceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("trace_id = %q, want 0af7651916cd43dd8448eb211c80319c", traceID)
	}
	if spanID != "b7ad6b7169203331" {
		t.Errorf("span_id = %q, want b7ad6b7169203331", spanID)
	}
}

func TestMiddleware_TraceParentGenerated(t *testing.T) {
	t.Parallel()

	logger := NewLogger(Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	})

	middleware := NewMiddleware(logger)

	// Test without traceparent - should generate new
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	var traceID, spanID string
	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := RequestContextFrom(r.Context())
		traceID = rc.TraceID
		spanID = rc.SpanID
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if traceID == "" {
		t.Error("trace_id should be generated when traceparent missing")
	}
	if spanID == "" {
		t.Error("span_id should be generated when traceparent missing")
	}
	// Verify ULID format (26 chars Crockford's Base32)
	if len(traceID) != 26 {
		t.Errorf("trace_id length = %d, want 26 (ULID)", len(traceID))
	}
	if len(spanID) != 16 {
		t.Errorf("span_id length = %d, want 16", len(spanID))
	}
}

func TestMiddleware_Propagation(t *testing.T) {
	t.Parallel()

	logger := NewLogger(Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	})

	middleware := NewMiddleware(logger)

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")

	var forwardedReq *http.Request
	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwardedReq = r
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Check that headers are added to context for outgoing requests
	// (The middleware adds them to context; actual HTTP client would read from context)
	rc := RequestContextFrom(forwardedReq.Context())
	if rc.RequestID == "" {
		t.Error("request_id should be in context")
	}
	if rc.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("trace_id not propagated: %s", rc.TraceID)
	}
}

func TestMiddleware_MethodToolExtraction(t *testing.T) {
	t.Parallel()

	logger := NewLogger(Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	})

	middleware := NewMiddleware(logger)

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"tools/call","params":{"name":"walk","arguments":{}}}`))
	req.Header.Set("Content-Type", "application/json")

	var method, tool string
	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := RequestContextFrom(r.Context())
		method = rc.Method
		tool = rc.Tool
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if method != "tools/call" {
		t.Errorf("method = %q, want tools/call", method)
	}
	if tool != "walk" {
		t.Errorf("tool = %q, want walk", tool)
	}
}

func TestMiddleware_ResponseHeaders(t *testing.T) {
	t.Parallel()

	logger := NewLogger(Config{
		Format:     "json",
		Level:      "info",
		OutputPath: "",
		Component:  ComponentMoluFront,
	})

	middleware := NewMiddleware(logger)

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Check response headers
	requestID := rec.Header().Get("X-Request-ID")
	traceID := rec.Header().Get("X-Trace-ID")

	if requestID == "" {
		t.Error("X-Request-ID header should be set in response")
	}
	if traceID == "" {
		t.Error("X-Trace-ID header should be set in response")
	}
}

// isValidULID checks if a string is a valid ULID (26 chars Crockford's Base32)
func isValidULID(s string) bool {
	if len(s) != 26 {
		return false
	}
	// Crockford's Base32 alphabet: 0-9, A-H, J-K, M-N, P-T, V-Z (no I, L, O, U)
	for _, c := range s {
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", c) {
			return false
		}
	}
	return true
}
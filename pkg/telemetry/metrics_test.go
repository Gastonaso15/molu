package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMetrics_Counters(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetricsWithRegistry(reg)
	handler := m.Handler()

	// Increment request counter
	m.IncRequests(ComponentMoluFront, "tools/call", "walk", "OK")
	m.IncRequests(ComponentMoluFront, "tools/call", "walk", "OK")
	m.IncRequests(ComponentMoluFront, "tools/call", "find", "NOT_FOUND")

	// Increment error counter
	m.IncErrors(ComponentMoluFront, "NOT_FOUND")
	m.IncErrors(ComponentMoluFront, "NOT_FOUND")
	m.IncErrors(ComponentMoluFront, "SUBSTRATE_UNAVAILABLE")

	// Fetch metrics
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	t.Logf("metrics output:\n%s", body)

	// Verify counters
	if !strings.Contains(body, `molu_requests_total{`) || !strings.Contains(body, `component="molu-front"`) || !strings.Contains(body, `method="tools/call"`) || !strings.Contains(body, `tool="walk"`) || !strings.Contains(body, `code="OK"`) || !strings.Contains(body, ` 2`) {
		t.Errorf("request counter not found or incorrect: %s", body)
	}
	if !strings.Contains(body, `molu_requests_total{`) || !strings.Contains(body, `component="molu-front"`) || !strings.Contains(body, `method="tools/call"`) || !strings.Contains(body, `tool="find"`) || !strings.Contains(body, `code="NOT_FOUND"`) || !strings.Contains(body, ` 1`) {
		t.Errorf("request counter for find not found: %s", body)
	}
	if !strings.Contains(body, `molu_errors_total{`) || !strings.Contains(body, `component="molu-front"`) || !strings.Contains(body, `code="NOT_FOUND"`) || !strings.Contains(body, ` 2`) {
		t.Errorf("error counter for NOT_FOUND not found: %s", body)
	}
	if !strings.Contains(body, `molu_errors_total{`) || !strings.Contains(body, `component="molu-front"`) || !strings.Contains(body, `code="SUBSTRATE_UNAVAILABLE"`) || !strings.Contains(body, ` 1`) {
		t.Errorf("error counter for SUBSTRATE_UNAVAILABLE not found: %s", body)
	}
}

func TestMetrics_LatencyHistogram(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetricsWithRegistry(reg)
	handler := m.Handler()

	// Observe latencies
	m.ObserveLatency(ComponentMoluFront, "tools/call", 10.5)
	m.ObserveLatency(ComponentMoluFront, "tools/call", 20.3)
	m.ObserveLatency(ComponentMoluFront, "xolu/walk", 150.0)

	// Fetch metrics
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	t.Logf("metrics output:\n%s", body)

	// Verify histogram buckets exist
	if !strings.Contains(body, `molu_latency_seconds_bucket{component="molu-front",method="tools/call"`) {
		t.Errorf("latency histogram for tools/call not found: %s", body)
	}
	if !strings.Contains(body, `molu_latency_seconds_bucket{component="molu-front",method="xolu/walk"`) {
		t.Errorf("latency histogram for xolu/walk not found: %s", body)
	}

	// Verify count and sum
	if !strings.Contains(body, `molu_latency_seconds_count{component="molu-front",method="tools/call"} 2`) {
		t.Errorf("latency count for tools/call incorrect: %s", body)
	}
}

func TestMetrics_Handler(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetricsWithRegistry(reg)
	handler := m.Handler()

	// Test metrics endpoint
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}

	// Should have content-type
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected text/plain content-type, got %s", ct)
	}
}

func TestMetrics_MultipleComponents(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetricsWithRegistry(reg)
	handler := m.Handler()

	// Different components
	m.IncRequests(ComponentMoluFront, "tools/call", "walk", "OK")
	m.IncRequests(ComponentHubRegistry, "admit", "", "OK")
	m.IncRequests(ComponentXoluClient, "walk", "", "OK")

	m.IncErrors(ComponentMoluFront, "CONTRACT_VIOLATION")
	m.IncErrors(ComponentXoluClient, "SUBSTRATE_UNAVAILABLE")

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()

	// Verify all components present
	if !strings.Contains(body, `component="molu-front"`) {
		t.Errorf("molu-front component missing")
	}
	if !strings.Contains(body, `component="hub-registry"`) {
		t.Errorf("hub-registry component missing")
	}
	if !strings.Contains(body, `component="xolu-client"`) {
		t.Errorf("xolu-client component missing")
	}
}
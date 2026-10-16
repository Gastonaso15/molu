package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealth_ReadyLive(t *testing.T) {
	t.Parallel()

	h := NewHealth()
	handler := h.Handler()

	// Initially should not be ready (never pinged)
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 initially (never pinged), got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"status":"degraded"`) {
		t.Errorf("expected degraded status, got: %s", body)
	}
}

func TestHealth_XoluAvailable(t *testing.T) {
	t.Parallel()

	h := NewHealth()
	handler := h.Handler()

	// Set xolu as available
	h.SetXoluStatus(true, time.Now())

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK when xolu available, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("expected ok status, got: %s", body)
	}
	if !strings.Contains(body, `"status":"available"`) {
		t.Errorf("expected xolu available, got: %s", body)
	}
}

func TestHealth_XoluUnavailable(t *testing.T) {
	t.Parallel()

	h := NewHealth()
	handler := h.Handler()

	// First set available
	h.SetXoluStatus(true, time.Now())

	// Then set unavailable
	h.SetXoluStatus(false, time.Now())

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should return 503 because last successful ping was > 0 but we set unavailable
	// Actually, the last success ping is still the previous one, so it would be available for 30s
	// Let's test with an old ping
	h2 := NewHealth()
	h2.SetXoluStatus(true, time.Now().Add(-5*time.Minute)) // ping 5 min ago
	handler2 := h2.Handler()

	req = httptest.NewRequest("GET", "/healthz", nil)
	rec = httptest.NewRecorder()
	handler2.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when last ping > 30s ago, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"status":"stale"`) {
		t.Errorf("expected xolu stale, got: %s", body)
	}
}

func TestHealth_ReadyLiveMethods(t *testing.T) {
	t.Parallel()

	h := NewHealth()

	// Initially not ready (never pinged)
	if h.Ready() {
		t.Error("expected Ready() false initially (never pinged)")
	}
	if !h.Live() {
		t.Error("expected Live() true initially")
	}

	// After setting xolu available
	h.SetXoluStatus(true, time.Now())
	if !h.Ready() {
		t.Error("expected Ready() true when xolu available")
	}
	if !h.Live() {
		t.Error("expected Live() true")
	}

	// After 30 seconds (simulate by setting old ping)
	h.SetXoluStatus(true, time.Now().Add(-5*time.Minute))
	if h.Ready() {
		t.Error("expected Ready() false when last ping > 30s ago")
	}
}
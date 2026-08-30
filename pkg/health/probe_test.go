package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthProbe_SuccessAndGatedCheck(t *testing.T) {
	var statusCode int32 = http.StatusOK
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := int(atomic.LoadInt32(&statusCode))
		w.WriteHeader(code)
	}))
	defer ts.Close()

	probe := NewHealthProbe(
		ts.URL,
		100*time.Millisecond,
		50*time.Millisecond,
		200*time.Millisecond,
		10*time.Millisecond,
		50*time.Millisecond,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Initial state should not be healthy
	if probe.IsHealthy() {
		t.Errorf("expected initial state to be unhealthy")
	}
	if err := probe.GatedCheck(); err == nil {
		t.Errorf("expected GatedCheck to fail before ping")
	}

	// 2. Ping once with 200 OK
	if err := probe.PingOnce(ctx); err != nil {
		t.Fatalf("PingOnce failed: %v", err)
	}
	if !probe.IsHealthy() {
		t.Errorf("expected probe to be healthy after 200 OK")
	}
	if err := probe.GatedCheck(); err != nil {
		t.Errorf("expected GatedCheck to succeed when healthy: %v", err)
	}

	// 3. Set server to 503 Service Unavailable
	atomic.StoreInt32(&statusCode, http.StatusServiceUnavailable)
	_ = probe.PingOnce(ctx)

	if probe.IsHealthy() {
		t.Errorf("expected probe to be unhealthy after 503 failure")
	}
	if err := probe.GatedCheck(); err == nil {
		t.Errorf("expected GatedCheck to fail when substrate is down")
	}
}

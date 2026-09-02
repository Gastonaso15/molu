package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newTestProbe(url string) *HealthProbe {
	return NewHealthProbe(
		url,
		20*time.Millisecond,  // pingInterval / startup retry cadence
		50*time.Millisecond,  // pingTimeout
		200*time.Millisecond, // pongFreshness
		10*time.Millisecond,  // failFloor
		80*time.Millisecond,  // failCeiling
	)
}

func TestHealthProbe_BackoffForIsMonotonicAndCapped(t *testing.T) {
	p := newTestProbe("http://unused")

	// fails <= 0 yields the normal ping interval.
	if got := p.backoffFor(0); got != p.pingInterval {
		t.Errorf("backoffFor(0) = %v, want pingInterval %v", got, p.pingInterval)
	}

	want := []time.Duration{
		10 * time.Millisecond, // fails=1 -> floor
		20 * time.Millisecond, // fails=2 -> 2x
		40 * time.Millisecond, // fails=3 -> 4x
		80 * time.Millisecond, // fails=4 -> 8x == ceiling
		80 * time.Millisecond, // fails=5 -> capped
	}
	for i, w := range want {
		fails := i + 1
		if got := p.backoffFor(fails); got != w {
			t.Errorf("backoffFor(%d) = %v, want %v", fails, got, w)
		}
	}

	// A long outage must not overflow into a negative or tiny duration.
	if got := p.backoffFor(1000); got != p.failCeiling {
		t.Errorf("backoffFor(1000) = %v, want ceiling %v", got, p.failCeiling)
	}
}

func TestHealthProbe_RecordFailUsesSameBackoff(t *testing.T) {
	p := newTestProbe("http://unused")

	for fails := 1; fails <= 5; fails++ {
		before := time.Now()
		p.recordFail(context.DeadlineExceeded)
		st := p.Status()
		if st.ConsecutiveFails != fails {
			t.Fatalf("ConsecutiveFails = %d, want %d", st.ConsecutiveFails, fails)
		}
		gap := st.NextRetryAt.Sub(before)
		want := p.backoffFor(fails)
		// Allow a small scheduling slack on the timestamp arithmetic.
		if gap < want-2*time.Millisecond || gap > want+20*time.Millisecond {
			t.Errorf("fails=%d: NextRetryAt gap = %v, want ~%v", fails, gap, want)
		}
	}
}

func TestHealthProbe_WaitForStartupSucceedsAfterRetries(t *testing.T) {
	var attempts int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	p := newTestProbe(ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	if err := p.WaitForStartup(ctx, 10); err != nil {
		t.Fatalf("WaitForStartup returned error: %v", err)
	}
	if !p.IsHealthy() {
		t.Errorf("probe should be healthy after WaitForStartup succeeds")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("server saw %d attempts, want 3", got)
	}
	// Two failed attempts => at least two pingInterval waits between them.
	if elapsed := time.Since(start); elapsed < 2*p.pingInterval {
		t.Errorf("elapsed %v, expected at least 2 ping intervals (%v)", elapsed, 2*p.pingInterval)
	}
}

func TestHealthProbe_WaitForStartupGivesUpAtMaxAttempts(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	p := newTestProbe(ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := p.WaitForStartup(ctx, 3); err == nil {
		t.Fatal("WaitForStartup should return an error once maxAttempts are exhausted")
	}
	if p.IsHealthy() {
		t.Errorf("probe should not be healthy after startup gave up")
	}
}

func TestHealthProbe_NoPongEverBeforeStartup(t *testing.T) {
	p := newTestProbe("http://unused")
	if !p.Status().LastPongAt.IsZero() {
		t.Errorf("LastPongAt should be zero before any successful pong (executor keys the STARTUP error on this)")
	}
}

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

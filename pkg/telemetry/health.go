package telemetry

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Health interface for health checks.
type Health interface {
	Handler() http.Handler
	Ready() bool
	Live() bool
	SetXoluStatus(available bool, lastPing time.Time)
}

// healthImpl implements Health.
type healthImpl struct {
	mu             sync.RWMutex
	lastSuccessPing time.Time // last time xolu ping succeeded
}

// NewHealth creates a new health checker.
func NewHealth() Health {
	return &healthImpl{
		lastSuccessPing: time.Time{}, // zero = never pinged
	}
}

func (h *healthImpl) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ready := h.Ready()
		live := h.Live()

		status := "ok"
		code := http.StatusOK
		if !ready {
			status = "degraded"
			code = http.StatusServiceUnavailable
		}
		if !live {
			status = "down"
			code = http.StatusServiceUnavailable
		}

		h.mu.RLock()
		lastPing := h.lastSuccessPing
		h.mu.RUnlock()

		var xoluStatus string
		if lastPing.IsZero() {
			xoluStatus = "unknown"
		} else if time.Since(lastPing) <= 30*time.Second {
			xoluStatus = "available"
		} else {
			xoluStatus = "stale"
		}

		response := map[string]any{
			"status":    status,
			"timestamp": time.Now().Format(time.RFC3339Nano),
			"checks": map[string]any{
				"xolu": map[string]any{
					"status":      xoluStatus,
					"last_ping":   lastPing.Format(time.RFC3339Nano),
					"seconds_ago": time.Since(lastPing).Seconds(),
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(response)
	})
}

func (h *healthImpl) Ready() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Ready if xolu ping succeeded within last 30 seconds
	if h.lastSuccessPing.IsZero() {
		return false // never pinged successfully
	}
	return time.Since(h.lastSuccessPing) <= 30*time.Second
}

func (h *healthImpl) Live() bool {
	// Liveness is always true unless process is shutting down
	return true
}

func (h *healthImpl) SetXoluStatus(available bool, lastPing time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if available {
		h.lastSuccessPing = lastPing
	}
	// If not available, don't update lastSuccessPing - it will naturally expire
}
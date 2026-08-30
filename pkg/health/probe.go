package health

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// ProbeStatus represents the current state of the xolu health probe.
type ProbeStatus struct {
	Healthy          bool      `json:"healthy"`
	LastPongAt       time.Time `json:"last_pong_at"`
	LastFailAt       time.Time `json:"last_fail_at,omitempty"`
	ConsecutiveFails int       `json:"consecutive_fails"`
	NextRetryAt      time.Time `json:"next_retry_at,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
}

// HealthProbe manages background polling of xolu's /ready endpoint.
type HealthProbe struct {
	xoluURL       string
	client        *http.Client
	pingInterval  time.Duration
	pingTimeout   time.Duration
	pongFreshness time.Duration
	failFloor     time.Duration
	failCeiling   time.Duration

	mu     sync.RWMutex
	status ProbeStatus
}

// NewHealthProbe creates a new HealthProbe instance.
func NewHealthProbe(xoluURL string, interval, timeout, freshness, floor, ceiling time.Duration) *HealthProbe {
	return &HealthProbe{
		xoluURL:       xoluURL,
		client:        &http.Client{Timeout: timeout},
		pingInterval:  interval,
		pingTimeout:   timeout,
		pongFreshness: freshness,
		failFloor:     floor,
		failCeiling:   ceiling,
		status: ProbeStatus{
			Healthy: false,
		},
	}
}

// Status returns a copy of the current probe status.
func (p *HealthProbe) Status() ProbeStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
}

// IsHealthy returns whether xolu is currently healthy according to freshness and failure state.
func (p *HealthProbe) IsHealthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.status.Healthy {
		return false
	}
	// Verify freshness window
	if time.Since(p.status.LastPongAt) > p.pongFreshness {
		return false
	}
	return true
}

// GatedCheck verifies health before dispatching calls. Returns an error if xolu is unreachable.
func (p *HealthProbe) GatedCheck() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.status.Healthy && time.Since(p.status.LastPongAt) <= p.pongFreshness {
		return nil
	}

	return fmt.Errorf("xolu substrate is currently unreachable (last pong: %v, fails: %d)",
		p.status.LastPongAt.Format(time.RFC3339), p.status.ConsecutiveFails)
}

// PingOnce executes a single /ready check against xolu.
func (p *HealthProbe) PingOnce(ctx context.Context) error {
	reqURL := fmt.Sprintf("%s/ready", p.xoluURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		p.recordFail(err)
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		p.recordFail(err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("xolu /ready returned status %d", resp.StatusCode)
		p.recordFail(err)
		return err
	}

	p.recordSuccess()
	return nil
}

func (p *HealthProbe) recordSuccess() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.status.Healthy = true
	p.status.LastPongAt = time.Now()
	p.status.ConsecutiveFails = 0
	p.status.LastError = ""
}

func (p *HealthProbe) recordFail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.status.Healthy = false
	p.status.LastFailAt = time.Now()
	p.status.ConsecutiveFails++
	p.status.LastError = err.Error()

	// Calculate exponential backoff
	backoff := p.failFloor * (1 << (p.status.ConsecutiveFails - 1))
	if backoff > p.failCeiling || backoff < 0 {
		backoff = p.failCeiling
	}
	p.status.NextRetryAt = time.Now().Add(backoff)
}

// WaitForStartup blocks until xolu produces its first successful pong, or maxAttempts is reached.
func (p *HealthProbe) WaitForStartup(ctx context.Context, maxAttempts int) error {
	attempts := 0
	for {
		attempts++
		err := p.PingOnce(ctx)
		if err == nil {
			slog.Info("xolu substrate reached and verified ready", "attempt", attempts)
			return nil
		}

		slog.Info("Waiting for xolu...", "attempt", attempts, "max_attempts", maxAttempts, "error", err)
		if maxAttempts > 0 && attempts >= maxAttempts {
			return fmt.Errorf("xolu failed to become ready after %d attempts: %w", maxAttempts, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.failFloor):
		}
	}
}

// Start runs the background probe loop until context cancellation.
func (p *HealthProbe) Start(ctx context.Context) {
	go func() {
		for {
			var sleepDuration time.Duration

			p.mu.RLock()
			isHealthy := p.status.Healthy
			fails := p.status.ConsecutiveFails
			p.mu.RUnlock()

			if isHealthy {
				sleepDuration = p.pingInterval
			} else {
				backoff := p.failFloor * (1 << fails)
				if backoff > p.failCeiling || backoff < 0 {
					backoff = p.failCeiling
				}
				sleepDuration = backoff
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(sleepDuration):
				_ = p.PingOnce(ctx)
			}
		}
	}()
}

package exec

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// Pinger es lo único que la sonda necesita de xolu: preguntar si está listo.
// El cliente oficial de xolu (pkg/client) ya lo cumple con su método Ready,
// que hace GET /ready. Usar una interfaz permite probar la sonda sin xolu.
type Pinger interface {
	Ready(ctx context.Context) error
}

// ProbeConfig son los tiempos de la sonda (Parte 2 §8.3, §8.4 y §8.6).
type ProbeConfig struct {
	Interval           time.Duration // cadencia normal entre pings
	Timeout            time.Duration // tiempo máximo de cada ping
	Freshness          time.Duration // cuánto tiempo se confía en el último pong
	FailFloor          time.Duration // primera espera después de un fallo
	FailCeiling        time.Duration // espera máxima del backoff
	StartupMaxAttempts int           // intentos al arrancar (0 = ilimitado)
}

// ProbeState es lo que la sonda recuerda (§8.1).
type ProbeState struct {
	LastPongAt       time.Time
	LastFailAt       time.Time
	LastError        string
	ConsecutiveFails int
	NextRetryAt      time.Time
	AttemptNumber    int
	MaxAttempts      int
}

// Probe vigila si xolu está disponible. Corre en su propia goroutine
// y las llamadas a herramientas consultan su estado con Check.
type Probe struct {
	pinger Pinger
	cfg    ProbeConfig
	now    func() time.Time // se reemplaza en los tests para controlar el reloj

	mu    sync.RWMutex
	state ProbeState
}

func NewProbe(pinger Pinger, cfg ProbeConfig) *Probe {
	return &Probe{
		pinger: pinger,
		cfg:    cfg,
		now:    time.Now,
		state: ProbeState{
			MaxAttempts: cfg.StartupMaxAttempts,
		},
	}
}

// pingOnce hace un solo ping a xolu y actualiza el estado.
func (p *Probe) pingOnce(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	err := p.pinger.Ready(ctx)

	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.state.LastFailAt = p.now()
		p.state.LastError = err.Error()
		p.state.ConsecutiveFails++
		p.state.AttemptNumber++
	} else {
		p.state.LastPongAt = p.now()
		p.state.LastError = ""
		p.state.ConsecutiveFails = 0
	}
	return err
}

// Healthy es true cuando hay un pong dentro de la ventana de frescura
// y no hubo un fallo después de ese pong (§8.1). El fallo se detecta con
// ConsecutiveFails y no comparando horas, porque en Windows el reloj puede
// darle la misma hora a dos pings seguidos.
func (p *Probe) Healthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.healthyLocked()
}

func (p *Probe) healthyLocked() bool {
	s := p.state
	if s.LastPongAt.IsZero() || s.ConsecutiveFails > 0 {
		return false
	}
	return p.now().Sub(s.LastPongAt) <= p.cfg.Freshness
}

// State devuelve una copia del estado actual.
func (p *Probe) State() ProbeState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// Check es la compuerta (§8.2): devuelve nil si se puede llamar a xolu,
// o un error XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE con RetryMetadata completo.
func (p *Probe) Check() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.healthyLocked() {
		return nil
	}
	s := p.state
	return taxonomy.NewSubstrateUnavailable(
		formatTimeString(s.LastPongAt),
		formatTimeString(s.NextRetryAt),
		s.AttemptNumber,
		s.MaxAttempts,
	)
}

// nextDelay calcula cuánto esperar hasta el próximo ping (§8.3):
// sin fallos, la cadencia normal; con fallos, FailFloor duplicándose
// en cada fallo seguido hasta llegar a FailCeiling.
func (p *Probe) nextDelay(consecutiveFails int) time.Duration {
	if consecutiveFails == 0 {
		return p.cfg.Interval
	}
	d := p.cfg.FailFloor
	for i := 1; i < consecutiveFails && d < p.cfg.FailCeiling; i++ {
		d *= 2
	}
	return min(d, p.cfg.FailCeiling)
}

// WaitReady espera el primer pong al arrancar (§8.4). Reintenta a la
// cadencia normal hasta StartupMaxAttempts (0 = sin límite).
func (p *Probe) WaitReady(ctx context.Context) error {
	maxText := "unlimited"
	if p.cfg.StartupMaxAttempts > 0 {
		maxText = fmt.Sprint(p.cfg.StartupMaxAttempts)
	}

	for attempt := 1; ; attempt++ {
		err := p.pingOnce(ctx)
		if err == nil {
			slog.Info("xolu is ready", "attempt", attempt)
			return nil
		}
		slog.Info(fmt.Sprintf("Waiting for xolu... attempt %d/%s (last error: %v)", attempt, maxText, err))

		if p.cfg.StartupMaxAttempts > 0 && attempt >= p.cfg.StartupMaxAttempts {
			return &Error{
				Code:    CodeStartup,
				Message: fmt.Sprintf("xolu did not become ready after %d attempts", attempt),
				Detail:  map[string]any{"last_error": err.Error()},
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.cfg.Interval):
		}
	}
}

// Run hace pings en bucle hasta que se cancele ctx (§8.3).
func (p *Probe) Run(ctx context.Context) {
	for {
		p.mu.Lock()
		delay := p.nextDelay(p.state.ConsecutiveFails)
		p.state.NextRetryAt = p.now().Add(delay)
		p.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		wasHealthy := p.Healthy()
		if err := p.pingOnce(ctx); err != nil {
			slog.Warn("xolu health probe failed", "error", err, "consecutive_fails", p.State().ConsecutiveFails)
		} else if !wasHealthy {
			slog.Info("xolu health probe recovered")
		}
	}
}

func formatTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func formatTimeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
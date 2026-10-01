package exec

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakePinger simula a xolu: devuelve los errores de la lista en orden
// y, cuando se acaba la lista, responde siempre bien.
type fakePinger struct {
	results []error
	calls   int
}

func (f *fakePinger) Ready(ctx context.Context) error {
	f.calls++
	if len(f.results) == 0 {
		return nil
	}
	err := f.results[0]
	f.results = f.results[1:]
	return err
}

var errDown = errors.New("connection refused")

func testConfig() ProbeConfig {
	return ProbeConfig{
		Interval:           time.Millisecond,
		Timeout:            time.Second,
		Freshness:          45 * time.Second,
		FailFloor:          1 * time.Second,
		FailCeiling:        30 * time.Second,
		StartupMaxAttempts: 3,
	}
}

func TestHealthyAfterPong(t *testing.T) {
	p := NewProbe(&fakePinger{}, testConfig())
	if p.Healthy() {
		t.Fatal("sin ningún pong no debería estar sano")
	}
	if err := p.pingOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !p.Healthy() {
		t.Fatal("después de un pong debería estar sano")
	}
}

func TestPongExpires(t *testing.T) {
	p := NewProbe(&fakePinger{}, testConfig())
	start := time.Now()
	p.now = func() time.Time { return start }
	_ = p.pingOnce(context.Background())

	// Avanzamos el reloj más allá de la ventana de frescura (45s).
	p.now = func() time.Time { return start.Add(46 * time.Second) }
	if p.Healthy() {
		t.Fatal("con el pong vencido no debería estar sano")
	}
}

func TestFailureAfterPong(t *testing.T) {
	p := NewProbe(&fakePinger{results: []error{nil, errDown}}, testConfig())
	// Reloj fijo: el pong y el fallo quedan con la misma hora, como pasa
	// en Windows cuando dos llamadas ocurren dentro del mismo tick del reloj.
	start := time.Now()
	p.now = func() time.Time { return start }
	_ = p.pingOnce(context.Background())
	_ = p.pingOnce(context.Background())
	if p.Healthy() {
		t.Fatal("un fallo después del pong debería dejarlo no sano")
	}
}

func TestCheckReturnsUnavailable(t *testing.T) {
	p := NewProbe(&fakePinger{results: []error{errDown}}, testConfig())
	_ = p.pingOnce(context.Background())

	err := p.Check()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("se esperaba *Error, llegó %v", err)
	}
	if e.Code != CodeUnavailable {
		t.Errorf("Code = %q, se esperaba %q", e.Code, CodeUnavailable)
	}
	if e.Detail["consecutive_fails"] != 1 {
		t.Errorf("consecutive_fails = %v, se esperaba 1", e.Detail["consecutive_fails"])
	}
}

func TestBackoff(t *testing.T) {
	p := NewProbe(&fakePinger{}, testConfig())
	want := map[int]time.Duration{
		0: time.Millisecond, // sin fallos: cadencia normal
		1: 1 * time.Second,  // primer fallo: el piso
		2: 2 * time.Second,
		3: 4 * time.Second,
		5: 16 * time.Second,
		6: 30 * time.Second, // 32s supera el techo
		9: 30 * time.Second,
	}
	for fails, expected := range want {
		if got := p.nextDelay(fails); got != expected {
			t.Errorf("nextDelay(%d) = %v, se esperaba %v", fails, got, expected)
		}
	}
}

func TestWaitReadySucceeds(t *testing.T) {
	f := &fakePinger{results: []error{errDown, errDown}}
	p := NewProbe(f, testConfig())
	if err := p.WaitReady(context.Background()); err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if f.calls != 3 {
		t.Errorf("se hicieron %d intentos, se esperaban 3", f.calls)
	}
}

func TestWaitReadyGivesUp(t *testing.T) {
	f := &fakePinger{results: []error{errDown, errDown, errDown, errDown}}
	p := NewProbe(f, testConfig())

	err := p.WaitReady(context.Background())
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeStartup {
		t.Fatalf("se esperaba %s, llegó %v", CodeStartup, err)
	}
	if f.calls != 3 {
		t.Errorf("se hicieron %d intentos, se esperaban 3", f.calls)
	}
}
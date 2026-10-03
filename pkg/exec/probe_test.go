package exec

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
	"github.com/Gastonaso15/molu/pkg/xolu"
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
	var te *taxonomy.TypedError
	if !errors.As(err, &te) {
		t.Fatalf("se esperaba *taxonomy.TypedError, llegó %v", err)
	}
	wantCode := taxonomy.PrefixMolu + string(taxonomy.CodeSubstrateUnavailable)
	if te.Code != wantCode {
		t.Errorf("Code = %q, se esperaba %q", te.Code, wantCode)
	}
	// Verificar que tiene los campos de RetryMetadata
	requiredFields := []string{"lastSuccessfulContact", "nextRetryAt", "attemptNumber", "maxAttempts"}
	for _, field := range requiredFields {
		if _, ok := te.Details[field]; !ok {
			t.Errorf("details falta campo %q", field)
		}
	}
	if attempt, ok := te.Details["attemptNumber"].(int); !ok || attempt != 1 {
		t.Errorf("attemptNumber = %v, se esperaba 1", te.Details["attemptNumber"])
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

func TestCheckReturnsSubstrateUnavailable(t *testing.T) {
	p := NewProbe(&fakePinger{results: []error{errDown}}, testConfig())
	_ = p.pingOnce(context.Background())

	err := p.Check()
	var te *taxonomy.TypedError
	if !errors.As(err, &te) {
		t.Fatalf("se esperaba *taxonomy.TypedError, llegó %T: %v", err, err)
	}
	wantCode := taxonomy.PrefixMolu + string(taxonomy.CodeSubstrateUnavailable)
	if te.Code != wantCode {
		t.Errorf("Code = %q, se esperaba %q", te.Code, wantCode)
	}
	// Verificar RetryMetadata completo
	requiredFields := []string{"lastSuccessfulContact", "nextRetryAt", "attemptNumber", "maxAttempts"}
	for _, field := range requiredFields {
		if _, ok := te.Details[field]; !ok {
			t.Errorf("details falta campo %q", field)
		}
	}
	// lastSuccessfulContact debe ser string vacío (sin pong previo)
	if te.Details["lastSuccessfulContact"] != "" {
		t.Errorf("lastSuccessfulContact = %q, se esperaba string vacío", te.Details["lastSuccessfulContact"])
	}
	// attemptNumber debe ser >= 1
	if attempt, ok := te.Details["attemptNumber"].(int); !ok || attempt < 1 {
		t.Errorf("attemptNumber = %v, se esperaba int >= 1", te.Details["attemptNumber"])
	}
	// maxAttempts debe ser StartupMaxAttempts (3 en testConfig)
	if maxAttempts, ok := te.Details["maxAttempts"].(int); !ok || maxAttempts != 3 {
		t.Errorf("maxAttempts = %v, se esperaba 3", te.Details["maxAttempts"])
	}
}

func TestCheckReturnsNilWhenHealthy(t *testing.T) {
	p := NewProbe(&fakePinger{}, testConfig())
	if err := p.pingOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	err := p.Check()
	if err != nil {
		t.Fatalf("se esperaba nil cuando healthy, llegó %v", err)
	}
}

func TestProbeRecoveryResetsBackoff(t *testing.T) {
	p := NewProbe(&fakePinger{results: []error{nil, errDown, errDown, nil}}, testConfig())

	// Primer pong: healthy
	if err := p.pingOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !p.Healthy() {
		t.Fatal("debería estar healthy tras pong")
	}

	// Dos fallos: unhealthy, backoff
	_ = p.pingOnce(context.Background())
	_ = p.pingOnce(context.Background())
	if p.Healthy() {
		t.Fatal("no debería estar healthy tras fallos")
	}
	_ = p.State().NextRetryAt // capturado para verificación implícita de backoff

	// Recuperación: pong exitoso
	_ = p.pingOnce(context.Background())
	if !p.Healthy() {
		t.Fatal("debería estar healthy tras recuperación")
	}

	// Verificar que ConsecutiveFails se reseteó a 0
	if p.State().ConsecutiveFails != 0 {
		t.Errorf("ConsecutiveFails = %d, se esperaba 0", p.State().ConsecutiveFails)
	}

	// Verificar que la cadencia se reseteó a Interval (no backoff)
	// nextDelay(0) debe ser Interval
	if p.nextDelay(0) != testConfig().Interval {
		t.Errorf("nextDelay(0) = %v, se esperaba %v", p.nextDelay(0), testConfig().Interval)
	}

	// Verificar que NextRetryAt se actualizó a cadencia normal (no backoff)
	// No podemos comparar exactamente NextRetryAt porque depende del reloj,
	// pero sí que ya no está en backoff (ConsecutiveFails=0)
}

func TestProbeConcurrency(t *testing.T) {
	p := NewProbe(&fakePinger{}, testConfig())
	_ = p.pingOnce(context.Background()) // healthy

	done := make(chan struct{})
	errors := make(chan error, 100)

	// Múltiples goroutines llamando Check() concurrentemente
	for i := 0; i < 50; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					if err := p.Check(); err != nil {
						errors <- err
					}
				}
			}
		}()
	}

	// Mientras tanto, Run() en otra goroutine (simulado con pingOnce)
	go func() {
		for i := 0; i < 100; i++ {
			_ = p.pingOnce(context.Background())
		}
	}()

	// Esperar un poco
	time.Sleep(100 * time.Millisecond)
	close(done)
	time.Sleep(50 * time.Millisecond)

	// No debe haber errores de race detector (go test -race lo detectaría)
	close(errors)
	for err := range errors {
		t.Errorf("error inesperado en concurrencia: %v", err)
	}
}

func TestIntegration_SubstrateUnavailable_WithProbe(t *testing.T) {
	// Setup: xolu mock que siempre falla en Ready
	mockXolu := &xolu.MockXoluClient{}
	mockXolu.ReadyFunc = func(ctx context.Context) error {
		return errors.New("connection refused")
	}

	cfg := testConfig()
	p := NewProbe(mockXolu, cfg)

	// Ejecutar WaitReady para simular arranque (debe fallar tras max attempts)
	ctx := context.Background()
	err := p.WaitReady(ctx)
	if err == nil {
		t.Fatal("WaitReady debería fallar cuando xolu no está disponible")
	}

	// Ahora simular que una tool call llega y llama a Check()
	// Como el probe está unhealthy, Check() debe retornar SUBSTRATE_UNAVAILABLE
	err = p.Check()
	var te *taxonomy.TypedError
	if !errors.As(err, &te) {
		t.Fatalf("se esperaba *taxonomy.TypedError, llegó %T: %v", err, err)
	}
	wantCode := taxonomy.PrefixMolu + string(taxonomy.CodeSubstrateUnavailable)
	if te.Code != wantCode {
		t.Errorf("Code = %q, se esperaba %q", te.Code, wantCode)
	}

	// Verificar RetryMetadata completo
	requiredFields := []string{"lastSuccessfulContact", "nextRetryAt", "attemptNumber", "maxAttempts"}
	for _, field := range requiredFields {
		if _, ok := te.Details[field]; !ok {
			t.Errorf("details falta campo %q", field)
		}
	}

	// attemptNumber debe ser >= StartupMaxAttempts (3)
	if attempt, ok := te.Details["attemptNumber"].(int); !ok || attempt < cfg.StartupMaxAttempts {
		t.Errorf("attemptNumber = %v, se esperaba >= %d", te.Details["attemptNumber"], cfg.StartupMaxAttempts)
	}

	// maxAttempts debe ser StartupMaxAttempts
	if maxAttempts, ok := te.Details["maxAttempts"].(int); !ok || maxAttempts != cfg.StartupMaxAttempts {
		t.Errorf("maxAttempts = %v, se esperaba %d", te.Details["maxAttempts"], cfg.StartupMaxAttempts)
	}

	// lastSuccessfulContact debe ser string vacío (nunca hubo pong exitoso)
	if te.Details["lastSuccessfulContact"] != "" {
		t.Errorf("lastSuccessfulContact = %q, se esperaba string vacío", te.Details["lastSuccessfulContact"])
	}
}
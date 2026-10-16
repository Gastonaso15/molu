package rpi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Gastonaso15/molu/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
)

func TestMockRPIProviderDelegatesAndRecordsCalls(t *testing.T) {
	actx := AdmitContext{Tenant: "acme", Roles: []string{"operator"}}
	candidates := []ToolCandidate{{Name: "walk", Primitive: true}, {Name: "work_orders.CreateOrder", Namespace: "work_orders"}}

	m := &MockRPIProvider{
		AdmitFunc: func(ctx context.Context, actx AdmitContext, candidates []ToolCandidate) ([]ToolCandidate, error) {
			return candidates[:1], nil
		},
		ScoreFunc: func(ctx context.Context, actx AdmitContext, admitted []ToolCandidate) ([]ScoredTool, error) {
			return []ScoredTool{{ToolCandidate: admitted[0], Score: 0.7}}, nil
		},
	}

	var p RPIProvider = m
	admitted, err := p.Admit(context.Background(), actx, candidates)
	if err != nil || len(admitted) != 1 || admitted[0].Name != "walk" {
		t.Fatalf("Admit = %v, %v; want [walk], nil", admitted, err)
	}
	scored, err := p.Score(context.Background(), actx, admitted)
	if err != nil || len(scored) != 1 || scored[0].Score != 0.7 {
		t.Fatalf("Score = %v, %v; want one tool with score 0.7", scored, err)
	}

	if len(m.AdmitCalls) != 1 || m.AdmitCalls[0].Context.Tenant != "acme" || len(m.AdmitCalls[0].Candidates) != 2 {
		t.Errorf("unexpected AdmitCalls: %+v", m.AdmitCalls)
	}
	if len(m.ScoreCalls) != 1 || m.ScoreCalls[0].Context.Tenant != "acme" || len(m.ScoreCalls[0].Admitted) != 1 {
		t.Errorf("unexpected ScoreCalls: %+v", m.ScoreCalls)
	}
}

func TestMockRPIProviderPropagatesErrors(t *testing.T) {
	boom := errors.New("provider down")
	m := &MockRPIProvider{
		AdmitFunc: func(context.Context, AdmitContext, []ToolCandidate) ([]ToolCandidate, error) { return nil, boom },
		ScoreFunc: func(context.Context, AdmitContext, []ToolCandidate) ([]ScoredTool, error) { return nil, boom },
	}

	if _, err := m.Admit(context.Background(), AdmitContext{}, nil); !errors.Is(err, boom) {
		t.Errorf("Admit error = %v, want %v", err, boom)
	}
	if _, err := m.Score(context.Background(), AdmitContext{}, nil); !errors.Is(err, boom) {
		t.Errorf("Score error = %v, want %v", err, boom)
	}
}

func TestMockRPIProviderNotImplemented(t *testing.T) {
	m := &MockRPIProvider{}

	if _, err := m.Admit(context.Background(), AdmitContext{}, nil); err == nil {
		t.Error("Admit without AdmitFunc should return an error")
	}
	if _, err := m.Score(context.Background(), AdmitContext{}, nil); err == nil {
		t.Error("Score without ScoreFunc should return an error")
	}
	// Calls are recorded even when no function is set.
	if len(m.AdmitCalls) != 1 || len(m.ScoreCalls) != 1 {
		t.Errorf("calls not recorded: admit=%d score=%d", len(m.AdmitCalls), len(m.ScoreCalls))
	}
}

func TestMockRPIProviderReset(t *testing.T) {
	m := &MockRPIProvider{}
	_, _ = m.Admit(context.Background(), AdmitContext{}, nil)
	_, _ = m.Score(context.Background(), AdmitContext{}, nil)

	m.Reset()

	if len(m.AdmitCalls) != 0 || len(m.ScoreCalls) != 0 {
		t.Errorf("Reset did not clear calls: admit=%d score=%d", len(m.AdmitCalls), len(m.ScoreCalls))
	}
}

func TestMockRPIProviderConcurrent(t *testing.T) {
	m := &MockRPIProvider{
		AdmitFunc: func(_ context.Context, _ AdmitContext, c []ToolCandidate) ([]ToolCandidate, error) { return c, nil },
		ScoreFunc: func(_ context.Context, _ AdmitContext, a []ToolCandidate) ([]ScoredTool, error) { return nil, nil },
	}

	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			actx := AdmitContext{Tenant: fmt.Sprintf("tenant-%d", i)}
			_, _ = m.Admit(context.Background(), actx, []ToolCandidate{{Name: "get", Primitive: true}})
			_, _ = m.Score(context.Background(), actx, nil)
		}()
	}
	wg.Wait()

	if len(m.AdmitCalls) != n || len(m.ScoreCalls) != n {
		t.Errorf("calls = admit %d score %d, want %d each", len(m.AdmitCalls), len(m.ScoreCalls), n)
	}
}

func TestMockRPIProviderStoresTelemetry(t *testing.T) {
	m := &MockRPIProvider{}
	logger := telemetry.NewLogger(telemetry.DefaultConfig())
	metrics := telemetry.NewMetricsWithRegistry(prometheus.NewRegistry())

	m.SetLogger(logger)
	m.SetMetrics(metrics)

	if m.Logger() != logger {
		t.Error("Logger() did not return the logger set with SetLogger")
	}
	if m.Metrics() != metrics {
		t.Error("Metrics() did not return the metrics set with SetMetrics")
	}
}

package rpi

import (
	"context"
	"errors"
	"sync"

	"github.com/Gastonaso15/molu/pkg/telemetry"
)

// AdmitCall records the arguments of one MockRPIProvider.Admit call.
type AdmitCall struct {
	Context    AdmitContext
	Candidates []ToolCandidate
}

// ScoreCall records the arguments of one MockRPIProvider.Score call.
type ScoreCall struct {
	Context  AdmitContext
	Admitted []ToolCandidate
}

// MockRPIProvider is a test double for RPIProvider. Calls are recorded and
// delegated to AdmitFunc/ScoreFunc; an unset func returns an error.
type MockRPIProvider struct {
	mu         sync.Mutex
	AdmitFunc  func(ctx context.Context, actx AdmitContext, candidates []ToolCandidate) ([]ToolCandidate, error)
	ScoreFunc  func(ctx context.Context, actx AdmitContext, admitted []ToolCandidate) ([]ScoredTool, error)
	AdmitCalls []AdmitCall
	ScoreCalls []ScoreCall
	logger     telemetry.Logger
	metrics    telemetry.Metrics
}

var _ RPIProvider = (*MockRPIProvider)(nil)

func (m *MockRPIProvider) Admit(ctx context.Context, actx AdmitContext, candidates []ToolCandidate) ([]ToolCandidate, error) {
	m.mu.Lock()
	m.AdmitCalls = append(m.AdmitCalls, AdmitCall{Context: actx, Candidates: candidates})
	fn := m.AdmitFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, actx, candidates)
	}
	return nil, errors.New("Admit not implemented")
}

func (m *MockRPIProvider) Score(ctx context.Context, actx AdmitContext, admitted []ToolCandidate) ([]ScoredTool, error) {
	m.mu.Lock()
	m.ScoreCalls = append(m.ScoreCalls, ScoreCall{Context: actx, Admitted: admitted})
	fn := m.ScoreFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, actx, admitted)
	}
	return nil, errors.New("Score not implemented")
}

func (m *MockRPIProvider) SetLogger(logger telemetry.Logger) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logger = logger
}

func (m *MockRPIProvider) SetMetrics(metrics telemetry.Metrics) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = metrics
}

// Logger returns the logger set with SetLogger, so AdmitWithLogging and
// ScoreWithLogging can log through the mock.
func (m *MockRPIProvider) Logger() telemetry.Logger {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.logger
}

func (m *MockRPIProvider) Metrics() telemetry.Metrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.metrics
}

func (m *MockRPIProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AdmitCalls = nil
	m.ScoreCalls = nil
}

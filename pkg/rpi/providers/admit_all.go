package providers

import (
	"context"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

// AdmitAllProvider is the V1 development provider: it admits every candidate
// unconditionally and scores each one 1.0. Admit and Score keep no state, so
// they are safe for concurrent use; SetLogger and SetMetrics are meant to be
// called once at startup, before serving.
type AdmitAllProvider struct {
	logger  telemetry.Logger
	metrics telemetry.Metrics
}

var _ rpi.RPIProvider = (*AdmitAllProvider)(nil)

func NewAdmitAllProvider() *AdmitAllProvider {
	return &AdmitAllProvider{}
}

// Admit returns a copy of all candidates in their original order, ignoring
// tenant, roles and query. An empty catalog yields an empty, non-nil slice.
func (p *AdmitAllProvider) Admit(ctx context.Context, actx rpi.AdmitContext, candidates []rpi.ToolCandidate) ([]rpi.ToolCandidate, error) {
	admitted := make([]rpi.ToolCandidate, len(candidates))
	copy(admitted, candidates)
	return admitted, nil
}

// Score assigns 1.0 to every admitted tool, keeping the input order.
func (p *AdmitAllProvider) Score(ctx context.Context, actx rpi.AdmitContext, admitted []rpi.ToolCandidate) ([]rpi.ScoredTool, error) {
	scored := make([]rpi.ScoredTool, len(admitted))
	for i, t := range admitted {
		scored[i] = rpi.ScoredTool{ToolCandidate: t, Score: 1.0}
	}
	return scored, nil
}

func (p *AdmitAllProvider) SetLogger(logger telemetry.Logger) {
	p.logger = logger
}

func (p *AdmitAllProvider) SetMetrics(metrics telemetry.Metrics) {
	p.metrics = metrics
}

// Logger returns the logger set with SetLogger, used by rpi.AdmitWithLogging
// and rpi.ScoreWithLogging.
func (p *AdmitAllProvider) Logger() telemetry.Logger {
	return p.logger
}

func (p *AdmitAllProvider) Metrics() telemetry.Metrics {
	return p.metrics
}

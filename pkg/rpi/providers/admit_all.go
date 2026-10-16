package providers

import (
	"context"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

// AdmitAllProvider is the V1 development provider: it admits every candidate
// unconditionally and scores each one 1.0. It holds no state, so it is safe
// for concurrent use.
type AdmitAllProvider struct{}

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

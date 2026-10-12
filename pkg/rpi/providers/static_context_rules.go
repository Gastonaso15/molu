package providers

import (
	"context"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

type StaticContextRulesProvider struct {
	rules map[string][]string // tenant:role -> allowed tool names
}

func NewStaticContextRulesProvider(rules map[string][]string) *StaticContextRulesProvider {
	return &StaticContextRulesProvider{rules: rules}
}

func (p *StaticContextRulesProvider) Admit(ctx context.Context, query string, candidates []rpi.ToolCandidate) ([]rpi.ToolCandidate, error) {
	return candidates, nil
}

func (p *StaticContextRulesProvider) Score(ctx context.Context, query string, admitted []rpi.ToolCandidate) ([]rpi.ScoredTool, error) {
	scored := make([]rpi.ScoredTool, len(admitted))
	for i, t := range admitted {
		scored[i] = rpi.ScoredTool{ToolCandidate: t, Score: 1.0}
	}
	return scored, nil
}
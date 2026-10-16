// Package rpi defines the Relevance Provider Interface that decides which
// tools are offered to an agent (Axis 1).
package rpi

import (
	"context"

	"github.com/Gastonaso15/molu/pkg/telemetry"
)

// ToolCandidate is a catalog entry the RPI may offer to an agent. Primitives
// carry no namespace; domain functions are named "namespace.Name".
type ToolCandidate struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Primitive   bool           `json:"primitive"`
	Namespace   string         `json:"namespace"`
}

// AdmitContext identifies who is asking for tools.
type AdmitContext struct {
	Tenant string   `json:"tenant"`
	Roles  []string `json:"roles"`
	Query  string   `json:"query,omitempty"`
}

// ScoredTool is an admitted candidate with its relevance, from 0.0 to 1.0
// (1.0 = most relevant).
type ScoredTool struct {
	ToolCandidate
	Score     float64 `json:"score"`
	Rationale string  `json:"rationale,omitempty"`
}

// RPIProvider decides which tools are offered to an agent. Admit returns the
// admitted subset of candidates; Score returns the admitted tools ordered by
// descending relevance.
type RPIProvider interface {
	Admit(ctx context.Context, actx AdmitContext, candidates []ToolCandidate) ([]ToolCandidate, error)
	Score(ctx context.Context, actx AdmitContext, admitted []ToolCandidate) ([]ScoredTool, error)
	SetLogger(logger telemetry.Logger)
	SetMetrics(metrics telemetry.Metrics)
}

func AdmitWithLogging(provider RPIProvider, ctx context.Context, actx AdmitContext, candidates []ToolCandidate) ([]ToolCandidate, error) {
	logger := telemetry.Logger(nil)
	if lp, ok := provider.(interface{ Logger() telemetry.Logger }); ok {
		logger = lp.Logger()
	}

	if logger != nil {
		logger = logger.WithContext(ctx).WithFields(map[string]any{
			"tenant":        actx.Tenant,
			"roles":         actx.Roles,
			"query":         actx.Query,
			"candidate_cnt": len(candidates),
		})
		logger.Application().Debug().Msg("rpi admit start")
	}

	start := telemetry.NowFunc()
	admitted, err := provider.Admit(ctx, actx, candidates)
	latencyMs := float64(telemetry.NowFunc().Sub(start).Milliseconds())

	if logger != nil {
		if err != nil {
			logger.Application().Error().
				Str("error", err.Error()).
				Float64("latency_ms", latencyMs).
				Msg("rpi admit failed")
		} else if len(admitted) == 0 {
			logger.Security().Warn().
				Str("decision", telemetry.DecisionDeny).
				Str("reason", "admit_empty").
				Float64("latency_ms", latencyMs).
				Msg("rpi admit empty -> NOT_OFFERED")
		} else {
			logger.Application().Info().
				Int("admitted_cnt", len(admitted)).
				Float64("latency_ms", latencyMs).
				Msg("rpi admit completed")
		}
	}

	return admitted, err
}

func ScoreWithLogging(provider RPIProvider, ctx context.Context, actx AdmitContext, admitted []ToolCandidate) ([]ScoredTool, error) {
	logger := telemetry.Logger(nil)
	if lp, ok := provider.(interface{ Logger() telemetry.Logger }); ok {
		logger = lp.Logger()
	}

	if logger != nil {
		logger = logger.WithContext(ctx).WithFields(map[string]any{
			"tenant":       actx.Tenant,
			"roles":        actx.Roles,
			"query":        actx.Query,
			"admitted_cnt": len(admitted),
		})
		logger.Application().Debug().Msg("rpi score start")
	}

	start := telemetry.NowFunc()
	scored, err := provider.Score(ctx, actx, admitted)
	latencyMs := float64(telemetry.NowFunc().Sub(start).Milliseconds())

	if logger != nil {
		if err != nil {
			logger.Application().Error().
				Str("error", err.Error()).
				Float64("latency_ms", latencyMs).
				Msg("rpi score failed")
		} else {
			logger.Application().Info().
				Int("scored_cnt", len(scored)).
				Float64("latency_ms", latencyMs).
				Msg("rpi score completed")
		}
	}

	return scored, err
}

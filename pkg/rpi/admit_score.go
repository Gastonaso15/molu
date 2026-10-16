package rpi

import (
	"context"

	"github.com/Gastonaso15/molu/pkg/telemetry"
)

type ToolCandidate struct {
	Name        string
	Namespace   string
	Description string
	InputSchema map[string]any
}

type ScoredTool struct {
	ToolCandidate
	Score float64
}

type RPIProvider interface {
	Admit(ctx context.Context, query string, candidates []ToolCandidate) ([]ToolCandidate, error)
	Score(ctx context.Context, query string, admitted []ToolCandidate) ([]ScoredTool, error)
	SetLogger(logger telemetry.Logger)
	SetMetrics(metrics telemetry.Metrics)
}

func AdmitWithLogging(provider RPIProvider, ctx context.Context, query string, candidates []ToolCandidate) ([]ToolCandidate, error) {
	logger := telemetry.Logger(nil)
	if lp, ok := provider.(interface{ Logger() telemetry.Logger }); ok {
		logger = lp.Logger()
	}

	if logger != nil {
		logger = logger.WithContext(ctx).WithFields(map[string]any{
			"query":         query,
			"candidate_cnt": len(candidates),
		})
		logger.Application().Debug().Msg("rpi admit start")
	}

	start := telemetry.NowFunc()
	admitted, err := provider.Admit(ctx, query, candidates)
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

func ScoreWithLogging(provider RPIProvider, ctx context.Context, query string, admitted []ToolCandidate) ([]ScoredTool, error) {
	logger := telemetry.Logger(nil)
	if lp, ok := provider.(interface{ Logger() telemetry.Logger }); ok {
		logger = lp.Logger()
	}

	if logger != nil {
		logger = logger.WithContext(ctx).WithFields(map[string]any{
			"query":        query,
			"admitted_cnt": len(admitted),
		})
		logger.Application().Debug().Msg("rpi score start")
	}

	start := telemetry.NowFunc()
	scored, err := provider.Score(ctx, query, admitted)
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
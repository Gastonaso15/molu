package rpi

import (
	"context"
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
}
// Package rpi defines the Relevance Provider Interface that decides which
// tools are offered to an agent (Axis 1).
package rpi

import (
	"context"
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
}

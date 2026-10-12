package rpi

import (
	"context"
	"errors"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

type ErrProviderUnavailable struct {
	LastContact  string // RFC3339, empty if unknown
	NextRetryAt  string // RFC3339, empty if unknown
	Attempt      int
	MaxAttempts  int
}

func (e *ErrProviderUnavailable) Error() string {
	return "RPI provider unavailable"
}

func Discover(ctx context.Context, provider RPIProvider, query AdmitQuery, candidates []Candidate) (DiscoveryResult, error) {
	admitted, err := provider.Admit(ctx, query, candidates)
	if err != nil {
		return DiscoveryResult{}, toSubstrateUnavailable(err)
	}

	// Separate admitted and not offered
	var admittedOnly []AdmittedCandidate
	var notOffered []AdmittedCandidate
	for _, a := range admitted {
		if a.Result == AdmissionAdmitted {
			admittedOnly = append(admittedOnly, a)
		} else {
			notOffered = append(notOffered, a)
		}
	}

	// Optional scoring: only if provider declares StageScore
	if provider.StagesImplemented()&StageScore != 0 {
		scorer, ok := provider.(Scorer)
		if !ok {
			// Registry already validated this at creation, but defense in depth
			return DiscoveryResult{}, toSubstrateUnavailable(&ErrProviderUnavailable{})
		}
		scoreQuery := ScoreQuery{AdmitQuery: query}
		reordered, err := scorer.Score(ctx, scoreQuery, admittedOnly)
		if err != nil {
			return DiscoveryResult{}, toSubstrateUnavailable(err)
		}
		admittedOnly = reordered
	}

	// Build Offered with only IDs and descriptions (references, not copies)
	offered := make([]Candidate, 0, len(admittedOnly))
	for _, a := range admittedOnly {
		offered = append(offered, a.Candidate)
	}

	return DiscoveryResult{
		Offered:    offered,
		NotOffered: notOffered,
	}, nil
}

func toSubstrateUnavailable(err error) *taxonomy.TypedError {
	var provErr *ErrProviderUnavailable
	if !errors.As(err, &provErr) {
		// Generic provider error -> empty metadata (do not invent lastSuccessfulContact=now)
		// Operator will see component="rpi-provider" to distinguish from xolu
		te := taxonomy.NewSubstrateUnavailable("", "", 1, 1)
		if te.Details == nil {
			te.Details = make(map[string]any)
		}
		te.Details["component"] = "rpi-provider"
		return te
	}
	te := taxonomy.NewSubstrateUnavailable(
		provErr.LastContact,
		provErr.NextRetryAt,
		provErr.Attempt,
		provErr.MaxAttempts,
	)
	if te.Details == nil {
		te.Details = make(map[string]any)
	}
	te.Details["component"] = "rpi-provider"
	return te
}
package rpi

import (
	"context"
	"errors"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

type ErrProviderUnavailable struct {
	LastContact  string // RFC3339, vacío si desconocido
	NextRetryAt  string // RFC3339, vacío si desconocido
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

	// Separar admitidos y no admitidos
	var admittedOnly []AdmittedCandidate
	var notOffered []AdmittedCandidate
	for _, a := range admitted {
		if a.Result == AdmissionAdmitted {
			admittedOnly = append(admittedOnly, a)
		} else {
			notOffered = append(notOffered, a)
		}
	}

	// Scoring opcional: solo si proveedor declara StageScore
	if provider.StagesImplemented()&StageScore != 0 {
		scorer, ok := provider.(Scorer)
		if !ok {
			// Registry ya validó esto al crear, pero defensa en profundidad
			return DiscoveryResult{}, toSubstrateUnavailable(&ErrProviderUnavailable{})
		}
		scoreQuery := ScoreQuery{AdmitQuery: query}
		reordered, err := scorer.Score(ctx, scoreQuery, admittedOnly)
		if err != nil {
			return DiscoveryResult{}, toSubstrateUnavailable(err)
		}
		admittedOnly = reordered
	}

	// Construir Offered solo con IDs y descripciones (referencias, no copias)
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
		// Error genérico del proveedor → metadata vacía (no inventar lastSuccessfulContact=ahora)
		// El operador verá component="rpi-provider" para distinguir de xolu
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
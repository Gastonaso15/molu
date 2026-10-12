package providers

import (
	"context"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

type ScoreTestDouble struct {
	err         error
	admitDelegate rpi.Admitter
}

func NewScoreTestDouble() *ScoreTestDouble {
	return &ScoreTestDouble{}
}

func (td *ScoreTestDouble) SetError(err error) {
	td.err = err
}

func (td *ScoreTestDouble) SetAdmitDelegate(delegate rpi.Admitter) {
	td.admitDelegate = delegate
}

func (td *ScoreTestDouble) StagesImplemented() rpi.Stage {
	stage := rpi.StageScore
	if td.admitDelegate != nil {
		stage |= rpi.StageAdmit
	}
	return stage
}

func (td *ScoreTestDouble) Admit(ctx context.Context, query rpi.AdmitQuery, candidates []rpi.Candidate) ([]rpi.AdmittedCandidate, error) {
	if td.admitDelegate != nil {
		return td.admitDelegate.Admit(ctx, query, candidates)
	}
	// Default: admitir todos
	out := make([]rpi.AdmittedCandidate, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, rpi.AdmittedCandidate{Candidate: c, Result: rpi.AdmissionAdmitted})
	}
	return out, nil
}

func (td *ScoreTestDouble) Score(ctx context.Context, query rpi.ScoreQuery, admitted []rpi.AdmittedCandidate) ([]rpi.AdmittedCandidate, error) {
	if td.err != nil {
		return nil, td.err
	}
	// Reordenar determinísticamente (reverse) para demostrar que reordena
	reordered := make([]rpi.AdmittedCandidate, len(admitted))
	for i, a := range admitted {
		reordered[len(admitted)-1-i] = a
	}
	return reordered, nil
}
package providers

import (
	"context"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

type AdmitAllProvider struct{}

func NewAdmitAllProvider(config ProviderConfig) (rpi.RPIProvider, error) {
	return &AdmitAllProvider{}, nil
}

func (p *AdmitAllProvider) StagesImplemented() rpi.Stage {
	return rpi.StageAdmit
}

func (p *AdmitAllProvider) Admit(ctx context.Context, query rpi.AdmitQuery, candidates []rpi.Candidate) ([]rpi.AdmittedCandidate, error) {
	out := make([]rpi.AdmittedCandidate, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, rpi.AdmittedCandidate{Candidate: c, Result: rpi.AdmissionAdmitted})
	}
	return out, nil
}
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

type RulesFile struct {
	Version string      `json:"version"`
	Rules   []AllowRule `json:"rules"`
}

type AllowRule struct {
	Tenant     string   `json:"tenant"`
	Role       string   `json:"role"`
	AllowedIDs []string `json:"allowedIds"` // namespace.Nombre
}

type StaticContextRulesProvider struct {
	rules map[string]map[string][]string // tenant -> role -> allowedIDs
}

func NewStaticContextRulesProvider(config ProviderConfig) (rpi.RPIProvider, error) {
	data, err := os.ReadFile(config.RulesFile)
	if err != nil {
		return nil, fmt.Errorf("read rules file: %w", err)
	}
	var rf RulesFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parse rules file: %w", err)
	}
	p := &StaticContextRulesProvider{rules: make(map[string]map[string][]string)}
	for _, rule := range rf.Rules {
		if p.rules[rule.Tenant] == nil {
			p.rules[rule.Tenant] = make(map[string][]string)
		}
		p.rules[rule.Tenant][rule.Role] = rule.AllowedIDs
	}
	return p, nil
}

func (p *StaticContextRulesProvider) StagesImplemented() rpi.Stage {
	return rpi.StageAdmit
}

func (p *StaticContextRulesProvider) Admit(ctx context.Context, query rpi.AdmitQuery, candidates []rpi.Candidate) ([]rpi.AdmittedCandidate, error) {
	allowed := p.rules[query.Tenant][query.Role] // nil si no hay regla para (tenant, rol)
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}

	out := make([]rpi.AdmittedCandidate, 0, len(candidates))
	for _, c := range candidates {
		if _, ok := allowedSet[c.ID]; ok {
			out = append(out, rpi.AdmittedCandidate{Candidate: c, Result: rpi.AdmissionAdmitted})
		} else {
			out = append(out, rpi.AdmittedCandidate{
				Candidate: c,
				Result:    rpi.AdmissionNotOffered,
				Reason:    fmt.Sprintf("not allowed for tenant=%q role=%q", query.Tenant, query.Role),
			})
		}
	}
	return out, nil
}
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

// StaticContextRulesProvider is the V1 production provider: it admits only
// the tools that static rules allow for the requester's tenant and roles.
// Rules map "tenant:role" to tool names, matched exactly and case-sensitively
// (primitives by bare name, domain functions as "namespace.Name"). A tenant
// or role without rules is denied everything. The rules never change after
// construction, so Admit and Score are safe for concurrent use; SetLogger and
// SetMetrics are meant to be called once at startup, before serving.
type StaticContextRulesProvider struct {
	allowed map[string]map[string]struct{} // "tenant:role" -> allowed tool names
	logger  telemetry.Logger
	metrics telemetry.Metrics
}

var _ rpi.RPIProvider = (*StaticContextRulesProvider)(nil)

// NewStaticContextRulesProvider builds the provider from rules keyed by
// "tenant:role". The rules are copied, so later changes by the caller do not
// affect the provider.
func NewStaticContextRulesProvider(rules map[string][]string) *StaticContextRulesProvider {
	allowed := make(map[string]map[string]struct{}, len(rules))
	for key, tools := range rules {
		set := make(map[string]struct{}, len(tools))
		for _, name := range tools {
			set[name] = struct{}{}
		}
		allowed[key] = set
	}
	return &StaticContextRulesProvider{allowed: allowed}
}

// ParseStaticContextRules reads rules in their JSON form, an object mapping
// "tenant:role" to a list of tool names:
//
//	{"acme:operator": ["walk", "work_orders.CreateOrder"]}
//
// Keys need a non-empty tenant and role (the tenant ends at the first ':'),
// and tool names must not be empty.
func ParseStaticContextRules(data []byte) (map[string][]string, error) {
	var rules map[string][]string
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("static-context-rules: invalid JSON: %w", err)
	}
	for key, tools := range rules {
		tenant, role, ok := strings.Cut(key, ":")
		if !ok || tenant == "" || role == "" {
			return nil, fmt.Errorf("static-context-rules: key %q must have the form \"tenant:role\"", key)
		}
		if slices.Contains(tools, "") {
			return nil, fmt.Errorf("static-context-rules: key %q has an empty tool name", key)
		}
	}
	return rules, nil
}

// Admit returns, in catalog order, the candidates allowed for any of the
// requester's roles in its tenant. Denied requests yield an empty, non-nil
// slice.
func (p *StaticContextRulesProvider) Admit(ctx context.Context, actx rpi.AdmitContext, candidates []rpi.ToolCandidate) ([]rpi.ToolCandidate, error) {
	admitted := make([]rpi.ToolCandidate, 0, len(candidates))
	for _, c := range candidates {
		if p.isAllowed(actx, c.Name) {
			admitted = append(admitted, c)
		}
	}
	return admitted, nil
}

// Score assigns 1.0 to the tools allowed for the requester and 0.0 to the
// rest, ordered by descending score and keeping the input order on ties.
func (p *StaticContextRulesProvider) Score(ctx context.Context, actx rpi.AdmitContext, admitted []rpi.ToolCandidate) ([]rpi.ScoredTool, error) {
	scored := make([]rpi.ScoredTool, len(admitted))
	for i, t := range admitted {
		score := 0.0
		if p.isAllowed(actx, t.Name) {
			score = 1.0
		}
		scored[i] = rpi.ScoredTool{ToolCandidate: t, Score: score}
	}
	slices.SortStableFunc(scored, func(a, b rpi.ScoredTool) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return 0
	})
	return scored, nil
}

// isAllowed reports whether any of the requester's roles allows the tool.
func (p *StaticContextRulesProvider) isAllowed(actx rpi.AdmitContext, name string) bool {
	for _, role := range actx.Roles {
		if _, ok := p.allowed[actx.Tenant+":"+role][name]; ok {
			return true
		}
	}
	return false
}

func (p *StaticContextRulesProvider) SetLogger(logger telemetry.Logger) {
	p.logger = logger
}

func (p *StaticContextRulesProvider) SetMetrics(metrics telemetry.Metrics) {
	p.metrics = metrics
}

// Logger returns the logger set with SetLogger, used by rpi.AdmitWithLogging
// and rpi.ScoreWithLogging.
func (p *StaticContextRulesProvider) Logger() telemetry.Logger {
	return p.logger
}

func (p *StaticContextRulesProvider) Metrics() telemetry.Metrics {
	return p.metrics
}

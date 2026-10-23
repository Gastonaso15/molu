package hub

import (
	"context"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// Discovery resolves which catalog tools are offered to a requester: it reads
// the registry, lets the RPI admit the tools allowed for the tenant and roles,
// and returns them ordered by descending relevance. It is transport-agnostic,
// so the MCP layer only passes the requester context and relays the result.
type Discovery struct {
	registry *Registry
	provider rpi.RPIProvider
}

func NewDiscovery(registry *Registry, provider rpi.RPIProvider) *Discovery {
	return &Discovery{registry: registry, provider: provider}
}

// ListTools returns the admitted tools. Tools the provider excludes are
// omitted without trace. Failures are *taxonomy.TypedError: CONTRACT_VIOLATION
// when the tenant is empty (the provider is not consulted) and EMPTY when
// nothing is admitted, which includes an empty catalog. Provider errors are
// returned unchanged.
func (d *Discovery) ListTools(ctx context.Context, actx rpi.AdmitContext) ([]rpi.ScoredTool, error) {
	const method = "tools/list"
	if actx.Tenant == "" {
		return nil, taxonomy.NewContractViolation("", method, []string{"tenant"})
	}

	regs := d.registry.List("")
	candidates := make([]rpi.ToolCandidate, len(regs))
	for i, reg := range regs {
		candidates[i] = rpi.ToolCandidate{
			Name:        reg.Name,
			Description: reg.Description,
			InputSchema: reg.InputSchema,
			Primitive:   reg.Primitive,
			Namespace:   reg.Namespace,
		}
	}

	admitted, err := rpi.AdmitWithLogging(d.provider, ctx, actx, candidates)
	if err != nil {
		return nil, err
	}
	if len(admitted) == 0 {
		return nil, taxonomy.NewEmpty(actx.Tenant, method, nil)
	}

	scored, err := rpi.ScoreWithLogging(d.provider, ctx, actx, admitted)
	if err != nil {
		return nil, err
	}
	return scored, nil
}

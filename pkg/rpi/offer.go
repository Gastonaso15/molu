package rpi

import (
	"context"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// Offer checks whether a catalog tool is part of the surface offered to the
// requester. A tool the provider's Admit excludes yields a NOT_OFFERED typed
// error: the tool exists, but not for this tenant/role. Provider errors are
// returned unchanged. Whether the tool exists at all is up to the caller.
func Offer(ctx context.Context, provider RPIProvider, actx AdmitContext, tool ToolCandidate) error {
	admitted, err := provider.Admit(ctx, actx, []ToolCandidate{tool})
	if err != nil {
		return err
	}
	for _, t := range admitted {
		if t.Name == tool.Name {
			return nil
		}
	}
	return taxonomy.NewNotOffered(actx.Tenant, tool.Name)
}

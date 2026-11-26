package orders

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// SetTemplateEnabledInput is the payload of work_orders.SetTemplateEnabled.
// Enabled is a pointer so that omitting it is a contract error rather than a
// silent "disable".
type SetTemplateEnabledInput struct {
	TemplateID ID    `json:"template_id"`
	Enabled    *bool `json:"enabled"`
}

// SetTemplateEnabledResult reports the template after the change. Changed is
// false when the template already had the requested state (nothing written).
type SetTemplateEnabledResult struct {
	Template Template `json:"template"`
	Changed  bool     `json:"changed"`
}

// SetTemplateEnabled enables or disables a template for new orders, as the
// work order module's enable/disable button does. Only the latest version can
// be switched (a superseded version is already out of use): anything else is
// INVALID_TRANSITION with details.rule=not_latest. Orders that already exist
// are not touched; CreateOrder refuses a disabled template with
// details.rule=template_disabled. The write is guarded by _version, so a
// concurrent change answers stale_state.
func (s *Service) SetTemplateEnabled(ctx context.Context, in SetTemplateEnabledInput) (SetTemplateEnabledResult, error) {
	id := strings.TrimSpace(string(in.TemplateID))
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return SetTemplateEnabledResult{}, &ValidationError{Fields: []string{"template_id"}, Reason: "must be a positive id"}
	}
	if in.Enabled == nil {
		return SetTemplateEnabledResult{}, &ValidationError{Fields: []string{"enabled"}, Reason: "required (true or false)"}
	}
	want := *in.Enabled

	doc, err := s.sub.GetEntity(ctx, TemplateEntityType, id)
	if err != nil {
		if isMissing(err) {
			return SetTemplateEnabledResult{}, &NotFoundError{ID: id, Kind: "work order template"}
		}
		return SetTemplateEnabledResult{}, classify(err, "")
	}
	if latest, _ := doc["latest"].(bool); !latest {
		return SetTemplateEnabledResult{}, rejection("not_latest", "only the latest version of a template can be enabled or disabled", "")
	}
	if templateEnabled(doc) == want && hasBool(doc, "enabled") {
		return SetTemplateEnabledResult{Template: templateFromRow(withID(doc, id))}, nil
	}

	version := intOf(doc["_version"])
	if version <= 0 {
		return SetTemplateEnabledResult{}, errors.New("work order template " + id + " has no _version: cannot change it safely")
	}
	patch := map[string]any{"enabled": want, "_version": version}
	if err := s.sub.PatchEntity(ctx, TemplateEntityType, id, patch); err != nil {
		var xe *XoluError
		if errors.As(err, &xe) && xe.Status == 409 {
			return SetTemplateEnabledResult{}, rejection("stale_state", "the template changed concurrently: read it again and retry", "")
		}
		return SetTemplateEnabledResult{}, classify(err, "")
	}
	doc["enabled"] = want
	return SetTemplateEnabledResult{Template: templateFromRow(withID(doc, id)), Changed: true}, nil
}

func hasBool(doc map[string]any, key string) bool {
	_, ok := doc[key].(bool)
	return ok
}

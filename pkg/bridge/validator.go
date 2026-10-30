package bridge

import (
	"fmt"

	"github.com/xeipuuv/gojsonschema"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// rootField is how gojsonschema names the document root.
const rootField = "(root)"

// ValidateDomainInput checks args against the input schema declared in a domain
// function contract.
//
// It returns nil when args satisfy the schema, and a CONTRACT_VIOLATION
// *taxonomy.TypedError listing the offending fields in details.fields when they
// do not. The tenant and tool are left empty; the caller knows them and may fill
// them in. A schema that cannot be compiled is a publisher defect, not an agent
// input violation, so it is returned as a plain error.
func ValidateDomainInput(schema map[string]any, args map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	if args == nil {
		args = map[string]any{}
	}

	result, err := gojsonschema.Validate(gojsonschema.NewGoLoader(schema), gojsonschema.NewGoLoader(args))
	if err != nil {
		return fmt.Errorf("invalid input schema: %w", err)
	}
	if result.Valid() {
		return nil
	}

	fields := make([]string, 0, len(result.Errors()))
	for _, e := range result.Errors() {
		fields = append(fields, violatingField(e))
	}
	return taxonomy.NewContractViolation("", "", fields)
}

// violatingField names the field an error is about. gojsonschema reports a
// missing required or unexpected property against its parent object, so the
// property name is taken from the error details and joined to the parent path.
func violatingField(e gojsonschema.ResultError) string {
	field := e.Field()
	switch e.Type() {
	case "required", "additional_property_not_allowed":
		if prop, ok := e.Details()["property"].(string); ok {
			if field == rootField {
				return prop
			}
			return field + "." + prop
		}
	}
	return field
}

package schema

import (
	"github.com/xeipuuv/gojsonschema"
)

// ValidationResult holds the result of input validation
type ValidationResult struct {
	Valid       bool
	InvalidFields []string
	CompileError error
}

// ValidateInput: validates args against inputSchema of the primitive
// Returns ValidationResult with invalid fields if validation fails
func ValidateInput(primitives []PrimitiveSchema, primitiveName string, args map[string]interface{}) ValidationResult {
	// Find primitive
	var target *PrimitiveSchema
	for i := range primitives {
		if primitives[i].Name == primitiveName {
			target = &primitives[i]
			break
		}
	}
	if target == nil {
		return ValidationResult{
			Valid:         false,
			InvalidFields: []string{"_primitive_not_found: " + primitiveName},
		}
	}
	// Compile JSON schema
	schemaLoader := gojsonschema.NewGoLoader(target.InputSchema)
	docLoader := gojsonschema.NewGoLoader(args)
	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return ValidationResult{
			Valid:         false,
			InvalidFields: []string{"_schema_compile_error: " + err.Error()},
			CompileError:  err,
		}
	}
	if result.Valid() {
		return ValidationResult{Valid: true}
	}
	// Extract invalid field names
	invalid := make([]string, 0, len(result.Errors()))
	for _, e := range result.Errors() {
		invalid = append(invalid, e.Field())
	}
	return ValidationResult{
		Valid:         false,
		InvalidFields: invalid,
	}
}
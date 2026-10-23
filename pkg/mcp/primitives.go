package mcp

import (
	"github.com/Gastonaso15/molu/pkg/schema"
)

// StandardPrimitives returns the full set of 13 generic primitives defined in §5.1.
// Primitives are domain-neutral and NEVER contain namespaces.
func StandardPrimitives() []schema.PrimitiveSchema {
	return []schema.PrimitiveSchema{
		{
			Name:        "describe",
			Description: "Reads semantic map / operational schemas of substrate",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"scope": map[string]interface{}{
						"type":    "string",
						"enum":    []interface{}{"all", "entities", "machines", "generators", "events"},
						"default": "all",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Optional. Restrict to one entity type, machine definition, generator, or event definition by name.",
					},
				},
			},
		},
		{
			Name:        "get",
			Description: "Retrieves a specific entity by type and ID",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"entity_type", "id"},
				"properties": map[string]interface{}{
					"entity_type": map[string]interface{}{"type": "string"},
					"id":          map[string]interface{}{"type": []interface{}{"string", "integer"}},
				},
			},
		},
		{
			Name:        "list",
			Description: "Lists entities of a given type with optional filters and pagination",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"entity_type"},
				"properties": map[string]interface{}{
					"entity_type": map[string]interface{}{"type": "string"},
					"filter":      map[string]interface{}{"type": "object", "description": "Field-value equality filters."},
					"limit":       map[string]interface{}{"type": "integer", "default": 50, "minimum": 1, "maximum": 500},
					"offset":      map[string]interface{}{"type": "integer", "default": 0, "minimum": 0},
				},
			},
		},
		{
			Name:        "create",
			Description: "Creates a new entity of a given type",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"entity_type", "data"},
				"properties": map[string]interface{}{
					"entity_type": map[string]interface{}{"type": "string"},
					"data":        map[string]interface{}{"type": "object", "description": "Field values for the new entity."},
				},
			},
		},
		{
			Name:        "update",
			Description: "Updates fields of an existing entity with optimistic concurrency",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"entity_type", "id", "changes"},
				"properties": map[string]interface{}{
					"entity_type": map[string]interface{}{"type": "string"},
					"id":          map[string]interface{}{"type": []interface{}{"string", "integer"}},
					"changes":     map[string]interface{}{"type": "object"},
					"version":     map[string]interface{}{"type": "integer", "description": "Optimistic concurrency version."},
				},
			},
		},
		{
			Name:        "query",
			Description: "Executes an OQL or Sulpher query against substrate",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"language", "query"},
				"properties": map[string]interface{}{
					"language": map[string]interface{}{"type": "string", "enum": []interface{}{"oql", "sulpher"}},
					"query":    map[string]interface{}{"type": "string"},
					"params":   map[string]interface{}{"type": "object", "description": "Optional bind parameters."},
				},
			},
		},
		{
			Name:        "walk",
			Description: "Executes an FSM state transition",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"machine_id", "input"},
				"properties": map[string]interface{}{
					"machine_id": map[string]interface{}{"type": []interface{}{"string", "integer"}},
					"input":      map[string]interface{}{"type": "string"},
					"payload":    map[string]interface{}{"type": "object", "description": "Fields available to guards as payload.<field>."},
				},
			},
		},
		{
			Name:        "machine_state",
			Description: "Reads current state, variables, and terminal status of an FSM",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"machine_id"},
				"properties": map[string]interface{}{
					"machine_id": map[string]interface{}{"type": []interface{}{"string", "integer"}},
				},
			},
		},
		{
			Name:        "machine_history",
			Description: "Reads transition history of an FSM",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"machine_id"},
				"properties": map[string]interface{}{
					"machine_id": map[string]interface{}{"type": []interface{}{"string", "integer"}},
					"limit":      map[string]interface{}{"type": "integer", "default": 20, "minimum": 1, "maximum": 500},
				},
			},
		},
		{
			Name:        "cal_check",
			Description: "Preliminary availability check across calendars without booking",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"calendars", "start", "end"},
				"properties": map[string]interface{}{
					"calendars": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Calendar identifiers to intersect.",
					},
					"start": map[string]interface{}{"type": "string", "format": "date-time"},
					"end":   map[string]interface{}{"type": "string", "format": "date-time"},
				},
			},
		},
		{
			Name:        "cal_openings",
			Description: "Finds free time openings across calendars given duration, range, and objective",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"calendars", "duration", "from", "to"},
				"properties": map[string]interface{}{
					"calendars": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"duration": map[string]interface{}{"type": "string", "description": "ISO 8601 duration (e.g. PT1H)."},
					"from":     map[string]interface{}{"type": "string", "format": "date-time"},
					"to":       map[string]interface{}{"type": "string", "format": "date-time"},
					"objective": map[string]interface{}{
						"type":    "string",
						"enum":    []interface{}{"earliest", "first-fit", "emptiest", "longest-clear-margin"},
						"default": "earliest",
					},
					"limit": map[string]interface{}{"type": "integer", "default": 10, "minimum": 1, "maximum": 100},
				},
			},
		},
		{
			Name:        "cal_propose",
			Description: "Creates a proposed booking in the tentative plane",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"calendars", "start", "end"},
				"properties": map[string]interface{}{
					"calendars": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"start":   map[string]interface{}{"type": "string", "format": "date-time"},
					"end":     map[string]interface{}{"type": "string", "format": "date-time"},
					"payload": map[string]interface{}{"type": "object", "description": "Optional application-defined metadata for the booking."},
				},
			},
		},
		{
			Name:        "cal_confirm",
			Description: "Confirms a proposed booking into the binding plane",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"booking_id"},
				"properties": map[string]interface{}{
					"booking_id": map[string]interface{}{"type": []interface{}{"string", "integer"}},
				},
			},
		},
	}
}

// GetStandardPrimitive returns the schema definition for a standard primitive by name.
func GetStandardPrimitive(name string) (schema.PrimitiveSchema, bool) {
	for _, p := range StandardPrimitives() {
		if p.Name == name {
			return p, true
		}
	}
	return schema.PrimitiveSchema{}, false
}

// IsStandardPrimitive checks if a tool name matches any of the 13 standard primitives.
func IsStandardPrimitive(name string) bool {
	_, ok := GetStandardPrimitive(name)
	return ok
}

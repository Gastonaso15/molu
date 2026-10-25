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
						"default":     "",
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"scope": "all",
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
					"entity_type": map[string]interface{}{
						"type":        "string",
						"description": "Entity type name (e.g. 'person', 'skill', 'deals')",
						"default":     "person",
						"examples":    []interface{}{"person", "skill"},
					},
					"id": map[string]interface{}{
						"type":        []interface{}{"string", "integer"},
						"description": "Identifier of the entity (e.g. 1)",
						"default":     1,
						"examples":    []interface{}{1, 2},
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"entity_type": "person",
						"id":          1,
					},
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
					"entity_type": map[string]interface{}{
						"type":        "string",
						"description": "Entity type name (e.g. 'person', 'skill', 'deals')",
						"default":     "person",
						"examples":    []interface{}{"person", "skill"},
					},
					"filter": map[string]interface{}{
						"type":        "object",
						"description": "Field-value equality filters.",
						"default":     map[string]interface{}{},
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of records to return.",
						"default":     10,
						"minimum":     1,
						"maximum":     500,
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Pagination offset.",
						"default":     0,
						"minimum":     0,
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"entity_type": "person",
						"limit":       10,
						"offset":      0,
					},
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
					"entity_type": map[string]interface{}{
						"type":        "string",
						"description": "Entity type name (e.g. 'person', 'skill')",
						"default":     "person",
					},
					"data": map[string]interface{}{
						"type":        "object",
						"description": "Field values for the new entity.",
						"default": map[string]interface{}{
							"name":  "Charlie",
							"level": 4,
						},
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"entity_type": "person",
						"data": map[string]interface{}{
							"name":  "Charlie",
							"level": 4,
						},
					},
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
					"entity_type": map[string]interface{}{
						"type":        "string",
						"description": "Entity type name (e.g. 'person')",
						"default":     "person",
					},
					"id": map[string]interface{}{
						"type":        []interface{}{"string", "integer"},
						"description": "Identifier of the entity to update",
						"default":     1,
					},
					"changes": map[string]interface{}{
						"type":        "object",
						"description": "Fields to update",
						"default": map[string]interface{}{
							"level": 7,
						},
					},
					"version": map[string]interface{}{
						"type":        "integer",
						"description": "Optimistic concurrency version.",
						"default":     1,
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"entity_type": "person",
						"id":          1,
						"changes": map[string]interface{}{
							"level": 7,
						},
					},
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
					"language": map[string]interface{}{
						"type":    "string",
						"enum":    []interface{}{"oql", "sulpher"},
						"default": "oql",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Query text in OQL or Sulpher syntax",
						"default":     "SELECT name, level FROM person WHERE level >= 5",
					},
					"params": map[string]interface{}{
						"type":        "object",
						"description": "Optional bind parameters.",
						"default":     map[string]interface{}{},
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"language": "oql",
						"query":    "SELECT name, level FROM person WHERE level >= 5",
					},
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
					"machine_id": map[string]interface{}{
						"type":        []interface{}{"string", "integer"},
						"description": "Identifier or name of the FSM machine.",
						"default":     "1",
					},
					"input": map[string]interface{}{
						"type":        "string",
						"description": "Transition input event to trigger.",
						"default":     "advance",
					},
					"payload": map[string]interface{}{
						"type":        "object",
						"description": "Fields available to guards as payload.<field>.",
						"default":     map[string]interface{}{},
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"machine_id": "1",
						"input":      "advance",
						"payload":    map[string]interface{}{},
					},
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
					"machine_id": map[string]interface{}{
						"type":        []interface{}{"string", "integer"},
						"description": "Identifier or name of the FSM machine.",
						"default":     "1",
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"machine_id": "1",
					},
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
					"machine_id": map[string]interface{}{
						"type":        []interface{}{"string", "integer"},
						"description": "Identifier or name of the FSM machine.",
						"default":     "1",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of history events to return.",
						"default":     20,
						"minimum":     1,
						"maximum":     500,
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"machine_id": "1",
						"limit":      20,
					},
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
						"default":     []interface{}{"room-a"},
					},
					"start": map[string]interface{}{
						"type":    "string",
						"format":  "date-time",
						"default": "2026-10-25T10:00:00Z",
					},
					"end": map[string]interface{}{
						"type":    "string",
						"format":  "date-time",
						"default": "2026-10-25T11:00:00Z",
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"calendars": []interface{}{"room-a"},
						"start":     "2026-10-25T10:00:00Z",
						"end":       "2026-10-25T11:00:00Z",
					},
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
						"type":    "array",
						"items":   map[string]interface{}{"type": "string"},
						"default": []interface{}{"room-a"},
					},
					"duration": map[string]interface{}{
						"type":        "string",
						"description": "ISO 8601 duration (e.g. PT1H).",
						"default":     "PT1H",
					},
					"from": map[string]interface{}{
						"type":    "string",
						"format":  "date-time",
						"default": "2026-10-25T08:00:00Z",
					},
					"to": map[string]interface{}{
						"type":    "string",
						"format":  "date-time",
						"default": "2026-10-25T18:00:00Z",
					},
					"objective": map[string]interface{}{
						"type":    "string",
						"enum":    []interface{}{"earliest", "first-fit", "emptiest", "longest-clear-margin"},
						"default": "earliest",
					},
					"limit": map[string]interface{}{
						"type":    "integer",
						"default": 10,
						"minimum": 1,
						"maximum": 100,
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"calendars": []interface{}{"room-a"},
						"duration":  "PT1H",
						"from":      "2026-10-25T08:00:00Z",
						"to":        "2026-10-25T18:00:00Z",
						"objective": "earliest",
					},
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
						"type":    "array",
						"items":   map[string]interface{}{"type": "string"},
						"default": []interface{}{"room-a"},
					},
					"start": map[string]interface{}{
						"type":    "string",
						"format":  "date-time",
						"default": "2026-10-25T14:00:00Z",
					},
					"end": map[string]interface{}{
						"type":    "string",
						"format":  "date-time",
						"default": "2026-10-25T15:00:00Z",
					},
					"payload": map[string]interface{}{
						"type":        "object",
						"description": "Optional application-defined metadata for the booking.",
						"default": map[string]interface{}{
							"title": "Sprint Planning",
						},
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"calendars": []interface{}{"room-a"},
						"start":     "2026-10-25T14:00:00Z",
						"end":       "2026-10-25T15:00:00Z",
					},
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
					"booking_id": map[string]interface{}{
						"type":        []interface{}{"string", "integer"},
						"description": "Identifier of the booking to confirm.",
						"default":     "bk-1001",
					},
				},
				"examples": []interface{}{
					map[string]interface{}{
						"booking_id": "bk-1001",
					},
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

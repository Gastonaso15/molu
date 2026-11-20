package schema

// PrimitiveSchema: a generic primitive (walk, find, get) without namespace
type PrimitiveSchema struct {
	Name        string                 `json:"name"`        // "walk" | "find" | "get"
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"` // JSON Schema Draft 2020-12
}

// StateMachine: state machine definition from xolu
type StateMachine struct {
	Name         string       `json:"name"`
	InitialState string       `json:"initialState"`
	States       []string     `json:"states"`
	Transitions  []Transition `json:"transitions"`
}

// Transition: transition between states
type Transition struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Event   string `json:"event"`     // name of primitive that triggers: "walk" | "find" | "get"
	Payload string `json:"payload"`   // reference to PayloadType
	Guard   string `json:"guard,omitempty"` // optional guard expression
}

// PayloadType: payload type definition
type PayloadType struct {
	Name   string                 `json:"name"`
	Schema map[string]interface{} `json:"schema"` // JSON Schema
}

// EntitySchema: the JSON Schema xolu has registered for one entity type
// (GET /api/v1/schema/{entity}).
type EntitySchema struct {
	Name   string                 `json:"name"`
	Schema map[string]interface{} `json:"schema"`
}

// Schemas: what molu knows about xolu's schemas.
//
// xolu does not publish primitives, state machines or payload types: its
// GET /api/v1/schemas lists only the entity types that have a registered
// schema, and the schema of each one is read from GET /api/v1/schema/{entity}.
// Entities holds those. Primitives, StateMachines and PayloadTypes keep the
// shape this package was first written for and stay empty against xolu; the
// 13 generic primitives are molu's own (see mcp.StandardPrimitives).
type Schemas struct {
	Primitives    []PrimitiveSchema `json:"primitives"`
	StateMachines []StateMachine    `json:"stateMachines"`
	PayloadTypes  []PayloadType     `json:"payloadTypes"`
	Entities      []EntitySchema    `json:"-"`
}
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

// Schemas: complete response from GET /schemas
type Schemas struct {
	Primitives    []PrimitiveSchema `json:"primitives"`
	StateMachines []StateMachine    `json:"stateMachines"`
	PayloadTypes  []PayloadType     `json:"payloadTypes"`
}
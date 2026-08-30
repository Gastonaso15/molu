package semantic

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// SemanticStore holds the thread-safe semantic map with atomic swap capability.
type SemanticStore struct {
	mu      sync.RWMutex
	current *SemanticMap
}

// NewSemanticStore initializes a new store with an empty semantic map.
func NewSemanticStore(tenant string) *SemanticStore {
	return &SemanticStore{
		current: &SemanticMap{
			Entities:  make(map[string]*EntityDef),
			Machines:  make(map[string]*MachineDef),
			Sequences: make(map[string]*GenDef),
			Events:    make(map[string]*EventDef),
			Tenant:    tenant,
			ReadAt:    time.Now(),
		},
	}
}

// Get returns the current snapshot of the SemanticMap for reading.
func (s *SemanticStore) Get() *SemanticMap {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Update atomically swaps the current SemanticMap with a new one.
func (s *SemanticStore) Update(newMap *SemanticMap) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = newMap
}

// SemanticMap is the internal representation of xolu's operational schema.
type SemanticMap struct {
	Entities  map[string]*EntityDef  `json:"entities"`
	Machines  map[string]*MachineDef `json:"machines"`
	Sequences map[string]*GenDef     `json:"sequences"`
	Events    map[string]*EventDef   `json:"events"`
	Tenant    string                 `json:"tenant"`
	ReadAt    time.Time              `json:"read_at"`
}

// EntityDef describes an operational entity type and its schema.
type EntityDef struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Fields []FieldDef      `json:"fields,omitempty"`
	Refs   []RefFieldDef   `json:"refs,omitempty"`
	FSMs   []string        `json:"fsms,omitempty"`
}

// FieldDef describes an individual field inside an entity.
type FieldDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Format      string `json:"format,omitempty"`
	Description string `json:"description,omitempty"`
}

// RefFieldDef describes a reference field linking to another entity.
type RefFieldDef struct {
	Field        string `json:"field"`
	TargetEntity string `json:"target_entity"`
}

// MachineDef describes an FSM definition.
type MachineDef struct {
	ID           string               `json:"id"`
	EntityType   string               `json:"entity_type,omitempty"`
	InitialState string               `json:"initial_state"`
	States       map[string]StateDef  `json:"states"`
	Transitions  []TransitionDef      `json:"transitions"`
	Variables    []VariableDef        `json:"variables,omitempty"`
}

// StateDef represents an FSM state.
type StateDef struct {
	Name     string `json:"name"`
	Terminal bool   `json:"terminal,omitempty"`
}

// TransitionDef represents an FSM state transition.
type TransitionDef struct {
	From   string   `json:"from"`
	Input  string   `json:"input"`
	To     string   `json:"to"`
	Guard  string   `json:"guard,omitempty"`
	SetOps []string `json:"set_ops,omitempty"`
	Output string   `json:"output,omitempty"`
}

// VariableDef describes an FSM machine variable.
type VariableDef struct {
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	Default interface{} `json:"default,omitempty"`
}

// GenDef describes a sequence/generator definition.
type GenDef struct {
	Name   string                 `json:"name"`
	Kind   string                 `json:"kind"` // sequence, ulid, cuid, uuid
	Params map[string]interface{} `json:"params,omitempty"`
}

// EventDef describes an event subscription/definition.
type EventDef struct {
	ID           string `json:"id"`
	EventType    string `json:"event_type"`
	LatchSource  string `json:"latch_source,omitempty"`
	TargetAction string `json:"target_action,omitempty"`
}

// FindEntity finds an entity definition by name (case-insensitive).
func (m *SemanticMap) FindEntity(name string) *EntityDef {
	if def, ok := m.Entities[name]; ok {
		return def
	}
	lower := strings.ToLower(name)
	for k, def := range m.Entities {
		if strings.ToLower(k) == lower {
			return def
		}
	}
	return nil
}

// FindMachine finds an FSM definition by id (case-insensitive).
func (m *SemanticMap) FindMachine(id string) *MachineDef {
	if def, ok := m.Machines[id]; ok {
		return def
	}
	lower := strings.ToLower(id)
	for k, def := range m.Machines {
		if strings.ToLower(k) == lower {
			return def
		}
	}
	return nil
}

// Describe returns a filtered view of the semantic map according to scope and name.
func (m *SemanticMap) Describe(scope, name string) (interface{}, error) {
	if scope == "" {
		scope = "all"
	}
	scope = strings.ToLower(scope)

	if name != "" {
		switch scope {
		case "entities":
			if e := m.FindEntity(name); e != nil {
				return e, nil
			}
			return nil, fmt.Errorf("entity type %q not found in semantic map", name)
		case "machines":
			if mach := m.FindMachine(name); mach != nil {
				return mach, nil
			}
			return nil, fmt.Errorf("fsm machine %q not found in semantic map", name)
		case "generators":
			if g, ok := m.Sequences[name]; ok {
				return g, nil
			}
			return nil, fmt.Errorf("generator/sequence %q not found in semantic map", name)
		case "events":
			if ev, ok := m.Events[name]; ok {
				return ev, nil
			}
			return nil, fmt.Errorf("event definition %q not found in semantic map", name)
		case "all":
			if e := m.FindEntity(name); e != nil {
				return e, nil
			}
			if mach := m.FindMachine(name); mach != nil {
				return mach, nil
			}
			if g, ok := m.Sequences[name]; ok {
				return g, nil
			}
			if ev, ok := m.Events[name]; ok {
				return ev, nil
			}
			return nil, fmt.Errorf("element %q not found in semantic map", name)
		default:
			return nil, fmt.Errorf("invalid scope %q", scope)
		}
	}

	switch scope {
	case "all":
		return m, nil
	case "entities":
		return m.Entities, nil
	case "machines":
		return m.Machines, nil
	case "generators":
		return m.Sequences, nil
	case "events":
		return m.Events, nil
	default:
		return nil, fmt.Errorf("invalid scope %q", scope)
	}
}

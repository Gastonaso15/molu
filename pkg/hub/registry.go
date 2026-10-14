// Package hub implements the domain-agnostic tool catalog behind the MCP Hub.
package hub

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// ToolRegistration describes a tool published to the Hub. Primitives (walk,
// find, get) carry no namespace; domain functions are named "namespace.Name".
type ToolRegistration struct {
	PublisherID string         `json:"publisher_id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Primitive   bool           `json:"primitive"`
	Namespace   string         `json:"namespace"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Registry is an in-memory, thread-safe catalog of published tools. It keeps a
// per-publisher view and a flat name index; a name is owned by one publisher.
type Registry struct {
	mu          sync.RWMutex
	byPublisher map[string]map[string]ToolRegistration
	byName      map[string]ToolRegistration
}

func NewRegistry() *Registry {
	return &Registry{
		byPublisher: make(map[string]map[string]ToolRegistration),
		byName:      make(map[string]ToolRegistration),
	}
}

// Register validates and stores reg. Re-registering the same (publisher_id,
// name) overwrites the previous entry without error. Invalid payloads and
// names already owned by another publisher yield CONTRACT_VIOLATION.
func (r *Registry) Register(reg ToolRegistration) error {
	if fields := validate(reg); len(fields) > 0 {
		return taxonomy.NewContractViolation("", "registry.register", fields)
	}
	reg = clone(reg)

	r.mu.Lock()
	defer r.mu.Unlock()

	if owner, ok := r.byName[reg.Name]; ok && owner.PublisherID != reg.PublisherID {
		return taxonomy.NewContractViolation("", "registry.register", []string{"name"})
	}
	tools := r.byPublisher[reg.PublisherID]
	if tools == nil {
		tools = make(map[string]ToolRegistration)
		r.byPublisher[reg.PublisherID] = tools
	}
	tools[reg.Name] = reg
	r.byName[reg.Name] = reg
	return nil
}

// Unregister removes a tool; NOT_FOUND if the publisher does not own it.
func (r *Registry) Unregister(publisherID, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tools := r.byPublisher[publisherID]
	if _, ok := tools[name]; !ok {
		err := taxonomy.NewNotFound("", name)
		err.Message = fmt.Sprintf("tool %q not registered by publisher %q", name, publisherID)
		err.Details = map[string]any{"publisher_id": publisherID, "name": name}
		return err
	}
	delete(tools, name)
	if len(tools) == 0 {
		delete(r.byPublisher, publisherID)
	}
	delete(r.byName, name)
	return nil
}

// Lookup returns a copy of the tool registered under name.
func (r *Registry) Lookup(name string) (ToolRegistration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reg, ok := r.byName[name]
	if !ok {
		return ToolRegistration{}, false
	}
	return clone(reg), true
}

// List returns copies of a publisher's tools, or of every tool when
// publisherID is empty, ordered by name. It never returns nil.
func (r *Registry) List(publisherID string) []ToolRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	src := r.byName
	if publisherID != "" {
		src = r.byPublisher[publisherID]
	}
	out := make([]ToolRegistration, 0, len(src))
	for _, reg := range src {
		out = append(out, clone(reg))
	}
	slices.SortFunc(out, func(a, b ToolRegistration) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// validate returns the offending field names, in a stable order.
func validate(reg ToolRegistration) []string {
	var fields []string
	if reg.PublisherID == "" {
		fields = append(fields, "publisher_id")
	}
	if reg.Name == "" {
		fields = append(fields, "name")
	}
	if reg.Primitive {
		if reg.Namespace != "" {
			fields = append(fields, "namespace")
		}
		if strings.Contains(reg.Name, ".") {
			fields = append(fields, "name")
		}
		return fields
	}
	if reg.Namespace == "" {
		return append(fields, "namespace")
	}
	if reg.Name != "" {
		if fn, ok := strings.CutPrefix(reg.Name, reg.Namespace+"."); !ok || fn == "" {
			fields = append(fields, "name")
		}
	}
	return fields
}

func clone(reg ToolRegistration) ToolRegistration {
	reg.InputSchema = cloneMap(reg.InputSchema)
	reg.Metadata = cloneMap(reg.Metadata)
	return reg
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneValue(e)
		}
		return out
	default:
		return v
	}
}

package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// Names of the MCP tools through which publishers manage their registrations.
const (
	MethodRegister   = "registry.register"
	MethodUnregister = "registry.unregister"
	MethodList       = "registry.list"
)

// ToolDescriptor is the MCP definition of a tool exposed by the Hub itself.
type ToolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// UnregisterParams are the parameters of registry.unregister.
type UnregisterParams struct {
	PublisherID string `json:"publisher_id"`
	Name        string `json:"name"`
}

// ListParams are the parameters of registry.list. An empty PublisherID selects
// the whole catalog.
type ListParams struct {
	PublisherID string `json:"publisher_id,omitempty"`
}

// ListResult is the result of registry.list.
type ListResult struct {
	Tools []ToolRegistration `json:"tools"`
}

// PublisherAPI lets external code register and withdraw tools at runtime. It is
// transport-agnostic: the MCP layer hands it raw tool arguments and sends back
// whatever it returns, so the API stays free of any SDK or domain knowledge.
type PublisherAPI struct {
	registry *Registry
}

func NewPublisherAPI(registry *Registry) *PublisherAPI {
	return &PublisherAPI{registry: registry}
}

// Tools describes the publisher-facing MCP tools, in a stable order.
func (a *PublisherAPI) Tools() []ToolDescriptor {
	str := map[string]any{"type": "string"}
	return []ToolDescriptor{
		{
			Name:        MethodRegister,
			Description: "Register or overwrite a tool in the catalog. Primitives (walk, find, get) have no namespace; domain functions are named namespace.Name.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"publisher_id": str,
					"name":         str,
					"description":  str,
					"inputSchema":  map[string]any{"type": "object"},
					"primitive":    map[string]any{"type": "boolean"},
					"namespace":    str,
					"metadata":     map[string]any{"type": "object"},
				},
				"required":             []any{"publisher_id", "name"},
				"additionalProperties": false,
			},
		},
		{
			Name:        MethodUnregister,
			Description: "Remove a previously registered tool from the catalog.",
			InputSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"publisher_id": str, "name": str},
				"required":             []any{"publisher_id", "name"},
				"additionalProperties": false,
			},
		},
		{
			Name:        MethodList,
			Description: "List registered tools, optionally limited to one publisher. Administrative view: no admission filtering is applied.",
			InputSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"publisher_id": str},
				"additionalProperties": false,
			},
		},
	}
}

// Handle dispatches a publisher tool call. Failures are *taxonomy.TypedError:
// CONTRACT_VIOLATION for bad parameters, NOT_FOUND for unknown registrations
// and NOT_OFFERED for methods that are not part of this API.
func (a *PublisherAPI) Handle(method string, params json.RawMessage) (any, error) {
	switch method {
	case MethodRegister:
		var reg ToolRegistration
		if err := decodeParams(method, params, true, &reg); err != nil {
			return nil, err
		}
		if err := a.registry.Register(reg); err != nil {
			return nil, err
		}
		return map[string]any{"registered": true, "publisher_id": reg.PublisherID, "name": reg.Name}, nil

	case MethodUnregister:
		var p UnregisterParams
		if err := decodeParams(method, params, true, &p); err != nil {
			return nil, err
		}
		var missing []string
		if p.PublisherID == "" {
			missing = append(missing, "publisher_id")
		}
		if p.Name == "" {
			missing = append(missing, "name")
		}
		if len(missing) > 0 {
			return nil, taxonomy.NewContractViolation("", method, missing)
		}
		if err := a.registry.Unregister(p.PublisherID, p.Name); err != nil {
			return nil, err
		}
		return map[string]any{"unregistered": true, "publisher_id": p.PublisherID, "name": p.Name}, nil

	case MethodList:
		var p ListParams
		if err := decodeParams(method, params, false, &p); err != nil {
			return nil, err
		}
		return ListResult{Tools: a.registry.List(p.PublisherID)}, nil
	}
	return nil, taxonomy.NewNotOffered("", method)
}

// decodeParams strictly decodes params into dst. Malformed JSON, non-object
// values and trailing data are reported against the field "params"; unknown
// fields and mistyped fields against their own name.
func decodeParams(method string, params json.RawMessage, required bool, dst any) error {
	trimmed := bytes.TrimSpace(params)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		if required {
			return taxonomy.NewContractViolation("", method, []string{"params"})
		}
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return taxonomy.NewContractViolation("", method, []string{badField(err)})
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return taxonomy.NewContractViolation("", method, []string{"params"})
	}
	return nil
}

// badField names the field a decoding error is about, or "params" when the
// error concerns the payload as a whole.
func badField(err error) string {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return typeErr.Field
	}
	if name, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		return strings.Trim(name, `"`)
	}
	return "params"
}

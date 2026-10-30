package bridge_test

import (
	"errors"
	"testing"

	"github.com/Gastonaso15/molu/pkg/bridge"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

func orderSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"order_id": map[string]any{"type": "string"},
			"quantity": map[string]any{"type": "integer"},
			"note":     map[string]any{"type": "string"},
			"customer": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
				"required": []any{"id"},
			},
		},
		"required":             []any{"order_id", "quantity"},
		"additionalProperties": false,
	}
}

func TestValidateDomainInput(t *testing.T) {
	tests := []struct {
		name       string
		schema     map[string]any
		args       map[string]any
		wantFields []string // nil means the input must be accepted
	}{
		{
			name:   "valid input",
			schema: orderSchema(),
			args:   map[string]any{"order_id": "WO-1", "quantity": 3},
		},
		{
			name:   "valid input with nested object",
			schema: orderSchema(),
			args: map[string]any{
				"order_id": "WO-1",
				"quantity": 3,
				"customer": map[string]any{"id": "C-9"},
			},
		},
		{
			name:       "missing required field",
			schema:     orderSchema(),
			args:       map[string]any{"order_id": "WO-1"},
			wantFields: []string{"quantity"},
		},
		{
			name:       "several missing required fields",
			schema:     orderSchema(),
			args:       map[string]any{},
			wantFields: []string{"order_id", "quantity"},
		},
		{
			name:       "wrong type",
			schema:     orderSchema(),
			args:       map[string]any{"order_id": "WO-1", "quantity": "three"},
			wantFields: []string{"quantity"},
		},
		{
			name:       "missing required field in nested object",
			schema:     orderSchema(),
			args:       map[string]any{"order_id": "WO-1", "quantity": 3, "customer": map[string]any{}},
			wantFields: []string{"customer.id"},
		},
		{
			name:       "additional property rejected",
			schema:     orderSchema(),
			args:       map[string]any{"order_id": "WO-1", "quantity": 3, "extra": true},
			wantFields: []string{"extra"},
		},
		{
			name:   "nil args with no required fields",
			schema: map[string]any{"type": "object"},
			args:   nil,
		},
		{
			name:   "empty args with no required fields",
			schema: map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
			args:   map[string]any{},
		},
		{
			name:       "nil args with required fields",
			schema:     orderSchema(),
			args:       nil,
			wantFields: []string{"order_id", "quantity"},
		},
		{
			name:   "nil schema accepts anything",
			schema: nil,
			args:   map[string]any{"anything": 1},
		},
		{
			name:   "empty schema accepts anything",
			schema: map[string]any{},
			args:   map[string]any{"anything": 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := bridge.ValidateDomainInput(tc.schema, tc.args)
			if tc.wantFields == nil {
				if err != nil {
					t.Fatalf("expected input to be accepted, got %v", err)
				}
				return
			}

			var te *taxonomy.TypedError
			if !errors.As(err, &te) {
				t.Fatalf("expected *taxonomy.TypedError, got %T (%v)", err, err)
			}
			wantCode := taxonomy.PrefixMolu + string(taxonomy.CodeContractViolation)
			if te.Code != wantCode {
				t.Fatalf("code = %q, expected %q", te.Code, wantCode)
			}
			got, ok := te.Details["fields"].([]string)
			if !ok {
				t.Fatalf("details.fields has type %T, expected []string", te.Details["fields"])
			}
			if !sameSet(got, tc.wantFields) {
				t.Fatalf("details.fields = %v, expected %v", got, tc.wantFields)
			}
		})
	}
}

func TestValidateDomainInput_InvalidSchema(t *testing.T) {
	schema := map[string]any{"type": "not-a-real-type"}

	err := bridge.ValidateDomainInput(schema, map[string]any{"a": 1})
	if err == nil {
		t.Fatal("expected an error for an uncompilable schema")
	}
	var te *taxonomy.TypedError
	if errors.As(err, &te) {
		t.Fatalf("a broken publisher schema must not be reported as an agent input violation, got %s", te.Code)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := map[string]int{}
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
	}
	for _, v := range count {
		if v != 0 {
			return false
		}
	}
	return true
}

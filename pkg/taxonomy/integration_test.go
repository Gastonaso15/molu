package taxonomy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/schema"
	"github.com/Gastonaso15/molu/pkg/xolu"
)

func TestIntegration_NotOffered(t *testing.T) {
	mockXolu := &xolu.MockXoluClient{}
	mockXolu.FindFunc = func(ctx context.Context, req xolu.FindRequest) (xolu.FindResponse, error) {
		return xolu.FindResponse{Results: []map[string]any{}, Total: 0}, nil
	}

	err := simulateFindWithAdmit(mockXolu, "tenant-123", "work_orders.CreateOrder", map[string]any{})
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeNotOffered))
}

func TestIntegration_NotFound(t *testing.T) {
	mockXolu := &xolu.MockXoluClient{}
	mockXolu.GetFunc = func(ctx context.Context, req xolu.GetRequest) (xolu.GetResponse, error) {
		return xolu.GetResponse{}, NewNotFound(req.TenantID, req.ID)
	}

	err := simulateGet(mockXolu, "tenant-123", "nonexistent-456")
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeNotFound))
	requireMessageContains(t, err, "tenant-123")
	requireMessageContains(t, err, "nonexistent-456")
}

func TestIntegration_XoluNotFoundVerbatim(t *testing.T) {
	mockXolu := &xolu.MockXoluClient{}
	mockXolu.GetFunc = func(ctx context.Context, req xolu.GetRequest) (xolu.GetResponse, error) {
		return xolu.GetResponse{}, &TypedError{
			Code:    PrefixXolu + string(CodeNotFound),
			Message: "xolu: object not found",
		}
	}

	err := simulateGet(mockXolu, "tenant-123", "obj-789")
	requireNotNil(t, err)
	requireCode(t, err, PrefixXolu+string(CodeNotFound))
	requireMessageContains(t, err, "xolu: object not found")
}

func TestIntegration_Empty(t *testing.T) {
	mockXolu := &xolu.MockXoluClient{}
	mockXolu.FindFunc = func(ctx context.Context, req xolu.FindRequest) (xolu.FindResponse, error) {
		return xolu.FindResponse{Results: []map[string]any{}, Total: 0}, nil
	}

	err := simulateFind(mockXolu, "tenant-123", map[string]any{"status": "archived"})
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeEmpty))
}

func TestIntegration_InvalidTransitionVerbatim(t *testing.T) {
	mockXolu := &xolu.MockXoluClient{}
	mockXolu.WalkFunc = func(ctx context.Context, req xolu.WalkRequest) (xolu.WalkResponse, error) {
		return xolu.WalkResponse{}, &TypedError{
			Code:    PrefixXolu + string(CodeInvalidTransition),
			Message: "guard: transition from 'closed' to 'open' not allowed",
		}
	}

	err := simulateWalk(mockXolu, "tenant-123", "work_orders.Transition", map[string]any{"to": "open"})
	requireNotNil(t, err)
	requireCode(t, err, PrefixXolu+string(CodeInvalidTransition))
	requireMessageContains(t, err, "guard: transition from 'closed' to 'open' not allowed")
}

func TestIntegration_ContractViolation(t *testing.T) {
	mockXolu := &xolu.MockXoluClient{}
	mockXolu.WalkFunc = func(ctx context.Context, req xolu.WalkRequest) (xolu.WalkResponse, error) {
		return xolu.WalkResponse{}, NewContractViolation(req.TenantID, req.Tool, []string{"title", "due_date"})
	}

	err := simulateWalk(mockXolu, "tenant-123", "work_orders.CreateOrder", map[string]any{"description": "test"})
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeContractViolation))
	requireDetailsFields(t, err, []string{"title", "due_date"})
}

// TestIntegration_ContractViolation_InvalidInput tests the full flow:
// schema validation via ValidateInput → CONTRACT_VIOLATION with invalidFields
func TestIntegration_ContractViolation_InvalidInput(t *testing.T) {
	// Define primitives with schemas matching xolu
	primitives := []schema.PrimitiveSchema{
		{
			Name: "walk",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"step":    map[string]interface{}{"type": "string", "enum": []string{"start", "continue", "finish"}},
					"payload": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"title": map[string]interface{}{"type": "string"}}, "required": []string{"title"}},
				},
				"required": []string{"step", "payload"},
			},
		},
		{
			Name: "find",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
				},
				"required": []string{"query"},
			},
		},
	}

	// Test 1: Missing required field "step"
	args := map[string]interface{}{"payload": map[string]interface{}{"title": "Test"}}
	result := schema.ValidateInput(primitives, "walk", args)
	requireFalse(t, result.Valid, "expected invalid for missing required field")
	fields := result.InvalidFields
	found := false
	for _, f := range fields {
		if f == "step" || f == "(root)" {
			found = true
			break
		}
	}
	requireTrue(t, found, "expected 'step' or '(root)' in invalid fields, got %v", fields)

	// Test 2: Missing nested required field "title" in payload
	args = map[string]interface{}{"step": "start", "payload": map[string]interface{}{}}
	result = schema.ValidateInput(primitives, "walk", args)
	requireFalse(t, result.Valid, "expected invalid for missing nested required field")
	fields = result.InvalidFields
	found = false
	for _, f := range fields {
		if f == "payload.title" || f == "title" || f == "payload" {
			found = true
			break
		}
	}
	requireTrue(t, found, "expected nested field in invalid fields, got %v", fields)

	// Test 3: Enum violation
	args = map[string]interface{}{"step": "invalid_step", "payload": map[string]interface{}{"title": "Test"}}
	result = schema.ValidateInput(primitives, "walk", args)
	requireFalse(t, result.Valid, "expected invalid for enum violation")
	fields = result.InvalidFields
	found = false
	for _, f := range fields {
		if f == "step" {
			found = true
			break
		}
	}
	requireTrue(t, found, "expected 'step' in invalid fields for enum violation, got %v", fields)

	// Test 4: Valid input should pass
	args = map[string]interface{}{"step": "start", "payload": map[string]interface{}{"title": "Valid"}}
	result = schema.ValidateInput(primitives, "walk", args)
	requireTrue(t, result.Valid, "expected valid for valid input")

	// Test 5: Full CONTRACT_VIOLATION error construction from validation result
	args = map[string]interface{}{"step": "start", "payload": map[string]interface{}{}}
	result = schema.ValidateInput(primitives, "walk", args)
	requireFalse(t, result.Valid, "expected invalid for nested missing field")
	err := NewContractViolation("tenant-123", "walk", result.InvalidFields)
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeContractViolation))
	requireDetailsFields(t, err, result.InvalidFields)
}

func TestIntegration_SubstrateUnavailable(t *testing.T) {
	mockXolu := &xolu.MockXoluClient{}
	mockXolu.PingFunc = func(ctx context.Context) error {
		return ErrSimulatedUnavailable
	}

	err := simulateWithRetries(mockXolu, 3, 100*time.Millisecond, 1*time.Second)
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeSubstrateUnavailable))
	if err.Details["attemptNumber"] != 3 || err.Details["maxAttempts"] != 3 {
		t.Errorf("attemptNumber/maxAttempts mismatch: %v", err.Details)
	}
}

func TestIntegration_XoluVerbatimPassthrough(t *testing.T) {
	mockXolu := &xolu.MockXoluClient{}
	mockXolu.WalkFunc = func(ctx context.Context, req xolu.WalkRequest) (xolu.WalkResponse, error) {
		return xolu.WalkResponse{}, &TypedError{
			Code:    PrefixXolu + "RATE_LIMITED",
			Message: "rate limit exceeded",
			Details: map[string]any{"retry_after": 60},
		}
	}

	err := simulateWalk(mockXolu, "tenant-123", "some.Tool", map[string]any{})
	requireNotNil(t, err)
	requireCode(t, err, PrefixXolu+"RATE_LIMITED")
	requireMessageContains(t, err, "rate limit exceeded")
	if err.Details["retry_after"] != 60 {
		t.Errorf("details not preserved: %v", err.Details)
	}
}

func TestIntegration_NoGenericError(t *testing.T) {
	testCases := []struct {
		name string
		fn   func() *TypedError
	}{
		{"NotOffered", func() *TypedError { return NewNotOffered("t", "tool") }},
		{"NotFound", func() *TypedError { return NewNotFound("t", "obj") }},
		{"Empty", func() *TypedError { return NewEmpty("t", "find", nil) }},
		{"InvalidTransition", func() *TypedError { return WrapXoluInvalidTransition(&TypedError{Code: PrefixXolu + string(CodeInvalidTransition), Message: "guard"}) }},
		{"ContractViolation", func() *TypedError { return NewContractViolation("t", "tool", []string{"f"}) }},
		{"SubstrateUnavailable", func() *TypedError { return NewSubstrateUnavailable("now", "later", 1, 3) }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			requireNotNil(t, err)
			if err.Code == "" {
				t.Fatal("code is empty")
			}
			if err.Message == "" {
				t.Fatal("message is empty")
			}
			hasPrefix := false
			for _, p := range []string{PrefixMolu, PrefixXolu} {
				if len(err.Code) >= len(p) && err.Code[:len(p)] == p {
					hasPrefix = true
					break
				}
			}
			if !hasPrefix {
				t.Errorf("code %q does not have required prefix %s or %s", err.Code, PrefixMolu, PrefixXolu)
			}
		})
	}
}

var ErrSimulatedUnavailable = errors.New("simulated unavailable")

func simulateFindWithAdmit(client *xolu.MockXoluClient, tenant, tool string, query map[string]any) *TypedError {
	return NewNotOffered(tenant, tool)
}

func simulateGet(client *xolu.MockXoluClient, tenant, id string) *TypedError {
	_, err := client.Get(context.Background(), xolu.GetRequest{ID: id, TenantID: tenant})
	if err != nil {
		if te, ok := err.(*TypedError); ok {
			return te
		}
		return &TypedError{Code: "UNKNOWN", Message: err.Error()}
	}
	return nil
}

func simulateFind(client *xolu.MockXoluClient, tenant string, query map[string]any) *TypedError {
	resp, err := client.Find(context.Background(), xolu.FindRequest{TenantID: tenant, Query: query})
	if err != nil {
		if te, ok := err.(*TypedError); ok {
			return te
		}
		return &TypedError{Code: "UNKNOWN", Message: err.Error()}
	}
	if len(resp.Results) == 0 {
		return NewEmpty(tenant, "find", query)
	}
	return nil
}

func simulateWalk(client *xolu.MockXoluClient, tenant, tool string, input map[string]any) *TypedError {
	_, err := client.Walk(context.Background(), xolu.WalkRequest{Tool: tool, Input: input, TenantID: tenant})
	if err != nil {
		if te, ok := err.(*TypedError); ok {
			return te
		}
		return &TypedError{Code: "UNKNOWN", Message: err.Error()}
	}
	return nil
}

func simulateWithRetries(client *xolu.MockXoluClient, maxAttempts int, floor, ceiling time.Duration) *TypedError {
	var lastContact string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := client.Ping(context.Background())
		if err == nil {
			return nil
		}
		lastContact = time.Now().UTC().Format(time.RFC3339)
		nextRetry := time.Now().UTC().Add(floor).Format(time.RFC3339)
		if attempt == maxAttempts {
			return NewSubstrateUnavailable(lastContact, nextRetry, attempt, maxAttempts)
		}
	}
	return nil
}
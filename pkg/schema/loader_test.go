package schema

import (
	"context"
	"errors"
	"testing"
	"time"
)

// MockSchemaClient for testing
type MockSchemaClient struct {
	schemas       Schemas
	err           error
	callCount     int
	failCount     int // number of times to fail before succeeding
}

func (m *MockSchemaClient) FetchSchemas(ctx context.Context) (Schemas, error) {
	m.callCount++
	if m.failCount > 0 {
		m.failCount--
		return Schemas{}, m.err
	}
	return m.schemas, nil
}

func TestLoadSuccess(t *testing.T) {
	// Arrange
	expectedPrimitives := []PrimitiveSchema{
		{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
		{Name: "find", Description: "Find FSM", InputSchema: map[string]interface{}{"type": "object"}},
		{Name: "get", Description: "Get FSM", InputSchema: map[string]interface{}{"type": "object"}},
	}
	mockClient := &MockSchemaClient{
		schemas: Schemas{
			Primitives: expectedPrimitives,
		},
	}
	loader := NewSchemaLoader(mockClient, LoaderConfig{
		RetryFloor:      10 * time.Millisecond,
		RetryCeiling:    100 * time.Millisecond,
		MaxAttempts:     3,
		Timeout:         5 * time.Second,
	})

	// Act
	err := loader.Load(context.Background())

	// Assert
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	primitives := loader.GetPrimitives()
	if len(primitives) != 3 {
		t.Fatalf("expected 3 primitives, got %d", len(primitives))
	}
	for i, p := range primitives {
		if p.Name != expectedPrimitives[i].Name {
			t.Errorf("primitive[%d]: expected name %q, got %q", i, expectedPrimitives[i].Name, p.Name)
		}
	}
	if mockClient.callCount != 1 {
		t.Errorf("expected FetchSchemas called once, got %d", mockClient.callCount)
	}
}

func TestLoadRetryBackoff(t *testing.T) {
	// Arrange: fail twice, then succeed
	expectedPrimitives := []PrimitiveSchema{
		{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
	}
	mockClient := &MockSchemaClient{
		schemas: Schemas{Primitives: expectedPrimitives},
		err:     errors.New("timeout"),
		failCount: 2,
	}
	loader := NewSchemaLoader(mockClient, LoaderConfig{
		RetryFloor:      10 * time.Millisecond,
		RetryCeiling:    200 * time.Millisecond,
		MaxAttempts:     5,
		Timeout:         5 * time.Second,
	})

	// Act
	start := time.Now()
	err := loader.Load(context.Background())
	elapsed := time.Since(start)

	// Assert
	if err != nil {
		t.Fatalf("Load() returned error after retries: %v", err)
	}
	// Should have taken at least RetryFloor + 2*RetryFloor = 30ms (10 + 20)
	// but with backoff: 10ms + 20ms = ~30ms minimum
	if elapsed < 25*time.Millisecond {
		t.Errorf("expected backoff delay, elapsed too fast: %v", elapsed)
	}
	if mockClient.callCount != 3 {
		t.Errorf("expected 3 calls (2 failures + 1 success), got %d", mockClient.callCount)
	}
}

func TestLoadMaxAttemptsExceeded(t *testing.T) {
	// Arrange: always fail
	mockClient := &MockSchemaClient{
		err:       errors.New("persistent error"),
		failCount: 10, // more than MaxAttempts
	}
	loader := NewSchemaLoader(mockClient, LoaderConfig{
		RetryFloor:      5 * time.Millisecond,
		RetryCeiling:    50 * time.Millisecond,
		MaxAttempts:     3,
		Timeout:         5 * time.Second,
	})

	// Act
	err := loader.Load(context.Background())

	// Assert
	if err == nil {
		t.Fatal("expected error after max attempts, got nil")
	}
	if mockClient.callCount != 3 {
		t.Errorf("expected 3 calls (MaxAttempts=3), got %d", mockClient.callCount)
	}
}

func TestLoadContextCancellation(t *testing.T) {
	// Arrange: fail first, then context cancelled during backoff
	mockClient := &MockSchemaClient{
		err:       errors.New("timeout"),
		failCount: 10, // keep failing
	}
	loader := NewSchemaLoader(mockClient, LoaderConfig{
		RetryFloor:      50 * time.Millisecond,
		RetryCeiling:    200 * time.Millisecond,
		MaxAttempts:     10,
		Timeout:         5 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	// Act
	err := loader.Load(ctx)

	// Assert
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context deadline exceeded or canceled, got: %v", err)
	}
}

func TestRefreshAtomicSwap(t *testing.T) {
	// Arrange: initial load
	initialPrimitives := []PrimitiveSchema{
		{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
	}
	mockClient := &MockSchemaClient{
		schemas: Schemas{Primitives: initialPrimitives},
	}
	loader := NewSchemaLoader(mockClient, LoaderConfig{
		RefreshInterval: 1 * time.Hour,
		RetryFloor:      10 * time.Millisecond,
		RetryCeiling:    100 * time.Millisecond,
		MaxAttempts:     3,
		Timeout:         5 * time.Second,
	})
	err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("initial Load failed: %v", err)
	}

	// Act: concurrent readers during Refresh
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			primitives := loader.GetPrimitives()
			// Atomic swap: readers see either old (1 primitive) or new (2 primitives) - both valid
			if len(primitives) != 1 && len(primitives) != 2 {
				t.Errorf("reader saw corrupted state (not 1 or 2): %v", primitives)
			}
			if len(primitives) > 0 && primitives[0].Name != "walk" {
				t.Errorf("first primitive should always be 'walk': %v", primitives)
			}
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Refresh with new schemas
	mockClient.schemas = Schemas{
		Primitives: []PrimitiveSchema{
			{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
			{Name: "find", Description: "Find FSM", InputSchema: map[string]interface{}{"type": "object"}},
		},
	}
	err = loader.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	<-done

	// Assert: final state has both primitives
	primitives := loader.GetPrimitives()
	if len(primitives) != 2 {
		t.Fatalf("expected 2 primitives after refresh, got %d", len(primitives))
	}
}

func TestRefreshAddRemovePrimitive(t *testing.T) {
	// Arrange
	mockClient := &MockSchemaClient{
		schemas: Schemas{
			Primitives: []PrimitiveSchema{
				{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
				{Name: "find", Description: "Find FSM", InputSchema: map[string]interface{}{"type": "object"}},
			},
		},
	}
	loader := NewSchemaLoader(mockClient, LoaderConfig{
		RefreshInterval: 1 * time.Hour,
		RetryFloor:      10 * time.Millisecond,
		RetryCeiling:    100 * time.Millisecond,
		MaxAttempts:     3,
		Timeout:         5 * time.Second,
	})
	err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("initial Load failed: %v", err)
	}

	// Act: Refresh removes "find", adds "get"
	mockClient.schemas = Schemas{
		Primitives: []PrimitiveSchema{
			{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
			{Name: "get", Description: "Get FSM", InputSchema: map[string]interface{}{"type": "object"}},
		},
	}
	err = loader.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// Assert
	primitives := loader.GetPrimitives()
	if len(primitives) != 2 {
		t.Fatalf("expected 2 primitives after refresh, got %d", len(primitives))
	}
	foundWalk, foundGet := false, false
	for _, p := range primitives {
		if p.Name == "walk" {
			foundWalk = true
		}
		if p.Name == "get" {
			foundGet = true
		}
	}
	if !foundWalk {
		t.Error("expected 'walk' to remain")
	}
	if !foundGet {
		t.Error("expected 'get' to be added")
	}
}

func TestValidateInputSuccess(t *testing.T) {
	primitives := []PrimitiveSchema{
		{
			Name: "walk",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"step": map[string]interface{}{"type": "string"},
				},
				"required": []string{"step"},
			},
		},
	}
	args := map[string]interface{}{"step": "start"}
	result := ValidateInput(primitives, "walk", args)
	if !result.Valid {
		t.Fatalf("expected valid for valid input, got: %v", result.InvalidFields)
	}
}

func TestValidateInputMissingRequired(t *testing.T) {
	primitives := []PrimitiveSchema{
		{
			Name: "walk",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"step": map[string]interface{}{"type": "string"},
				},
				"required": []string{"step"},
			},
		},
	}
	args := map[string]interface{}{}
	result := ValidateInput(primitives, "walk", args)
	if result.Valid {
		t.Fatal("expected invalid for missing required field")
	}
	// gojsonschema returns "(root)" for missing required at root level
	if len(result.InvalidFields) == 0 || result.InvalidFields[0] != "(root)" {
		t.Errorf("expected '(root)' for missing required at root, got %v", result.InvalidFields)
	}
}

func TestValidateInputTypeMismatch(t *testing.T) {
	primitives := []PrimitiveSchema{
		{
			Name: "walk",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"count": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"count"},
			},
		},
	}
	args := map[string]interface{}{"count": "not-a-number"}
	result := ValidateInput(primitives, "walk", args)
	if result.Valid {
		t.Fatal("expected invalid for type mismatch")
	}
	found := false
	for _, f := range result.InvalidFields {
		if f == "count" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'count' in invalidFields, got %v", result.InvalidFields)
	}
}

func TestValidateInputEnumViolation(t *testing.T) {
	primitives := []PrimitiveSchema{
		{
			Name: "walk",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"direction": map[string]interface{}{"type": "string", "enum": []string{"forward", "backward"}},
				},
				"required": []string{"direction"},
			},
		},
	}
	args := map[string]interface{}{"direction": "sideways"}
	result := ValidateInput(primitives, "walk", args)
	if result.Valid {
		t.Fatal("expected invalid for enum violation")
	}
	found := false
	for _, f := range result.InvalidFields {
		if f == "direction" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'direction' in invalidFields, got %v", result.InvalidFields)
	}
}

func TestValidateInputNestedObject(t *testing.T) {
	primitives := []PrimitiveSchema{
		{
			Name: "walk",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"payload": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"step": map[string]interface{}{"type": "string"},
						},
						"required": []string{"step"},
					},
				},
				"required": []string{"payload"},
			},
		},
	}
	// Valid nested
	args := map[string]interface{}{"payload": map[string]interface{}{"step": "start"}}
	result := ValidateInput(primitives, "walk", args)
	if !result.Valid {
		t.Fatalf("expected valid for valid nested input, got: %v", result.InvalidFields)
	}
	// Invalid nested - missing required field inside payload
	args = map[string]interface{}{"payload": map[string]interface{}{}}
	result = ValidateInput(primitives, "walk", args)
	if result.Valid {
		t.Fatal("expected invalid for missing nested required field")
	}
	// gojsonschema returns "payload" for missing required in nested object
	found := false
	for _, f := range result.InvalidFields {
		if f == "payload" || f == "payload.step" || f == "step" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected nested field in invalidFields, got %v", result.InvalidFields)
	}
}
package schema

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSchemaClientTimeout(t *testing.T) {
	// Server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"primitives":[]}`))
	}))
	defer server.Close()

	client := NewXoluSchemaClient(server.URL, 50*time.Millisecond)
	_, err := client.FetchSchemas(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestSchemaClient5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewXoluSchemaClient(server.URL, 5*time.Second)
	_, err := client.FetchSchemas(context.Background())
	if err == nil {
		t.Fatal("expected 5xx error")
	}
}

func TestSchemaClientInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	client := NewXoluSchemaClient(server.URL, 5*time.Second)
	_, err := client.FetchSchemas(context.Background())
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestSchemaClientUnexpectedStructure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"unexpected": "structure"}`))
	}))
	defer server.Close()

	client := NewXoluSchemaClient(server.URL, 5*time.Second)
	schemas, err := client.FetchSchemas(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should parse without error but primitives will be empty
	if len(schemas.Primitives) != 0 {
		t.Errorf("expected empty primitives, got %d", len(schemas.Primitives))
	}
}

func TestLoaderConcurrency(t *testing.T) {
	primitives := []PrimitiveSchema{
		{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
		{Name: "find", Description: "Find FSM", InputSchema: map[string]interface{}{"type": "object"}},
	}
	mockClient := &MockSchemaClient{
		schemas: Schemas{Primitives: primitives},
	}
	loader := NewSchemaLoader(mockClient, LoaderConfig{
		RefreshInterval: 100 * time.Millisecond,
		RetryFloor:      10 * time.Millisecond,
		RetryCeiling:    100 * time.Millisecond,
		MaxAttempts:     3,
		Timeout:         5 * time.Second,
	})
	err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("initial Load failed: %v", err)
	}

	// Start refresh goroutine
	ctx, cancel := context.WithCancel(context.Background())
	loader.Run(ctx)
	defer func() {
		cancel()
		loader.Stop()
	}()

	// Multiple concurrent readers and writers
	done := make(chan bool, 2)
	go func() {
		for i := 0; i < 50; i++ {
			primitives := loader.GetPrimitives()
			if len(primitives) < 2 || len(primitives) > 3 {
				t.Errorf("reader saw invalid count: %d", len(primitives))
			}
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 10; i++ {
			mockClient.schemas = Schemas{
				Primitives: []PrimitiveSchema{
					{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
					{Name: "find", Description: "Find FSM", InputSchema: map[string]interface{}{"type": "object"}},
					{Name: "get", Description: "Get FSM", InputSchema: map[string]interface{}{"type": "object"}},
				},
			}
			_ = loader.Refresh(context.Background())
			time.Sleep(5 * time.Millisecond)
			mockClient.schemas = Schemas{
				Primitives: []PrimitiveSchema{
					{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
					{Name: "find", Description: "Find FSM", InputSchema: map[string]interface{}{"type": "object"}},
				},
			}
			_ = loader.Refresh(context.Background())
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()

	<-done
	<-done
}
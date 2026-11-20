package schema

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
			mockClient.SetSchemas(Schemas{
				Primitives: []PrimitiveSchema{
					{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
					{Name: "find", Description: "Find FSM", InputSchema: map[string]interface{}{"type": "object"}},
					{Name: "get", Description: "Get FSM", InputSchema: map[string]interface{}{"type": "object"}},
				},
			})
			_ = loader.Refresh(context.Background())
			time.Sleep(5 * time.Millisecond)
			mockClient.SetSchemas(Schemas{
				Primitives: []PrimitiveSchema{
					{Name: "walk", Description: "Walk FSM", InputSchema: map[string]interface{}{"type": "object"}},
					{Name: "find", Description: "Find FSM", InputSchema: map[string]interface{}{"type": "object"}},
				},
			})
			_ = loader.Refresh(context.Background())
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()

	<-done
	<-done
}

// xoluSchemasServer imitates xolu 0.30+/0.31: GET /api/v1/schemas lists only
// names, and GET /api/v1/schema/{entity} returns one entity's JSON Schema.
func xoluSchemasServer(t *testing.T, listBody string, details map[string]string, hits *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if hits != nil {
			*hits = append(*hits, r.URL.EscapedPath())
		}
		mu.Unlock()
		if r.URL.Path == "/api/v1/schemas" {
			w.Write([]byte(listBody))
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/api/v1/schema/")
		body, ok := details[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":"XOLU-ST008","message":"No schema found for ` + name + `","status":404}}`))
			return
		}
		if body == "BOOM" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSchemaClient_RealXoluFormat_LoadsEntitySchemas(t *testing.T) {
	srv := xoluSchemasServer(t,
		`{"count":2,"schemas":[{"name":"brands"},{"name":"assets"}]}`,
		map[string]string{
			"assets": `{"type":"object","properties":{"name":{"type":"string"},"brand_id":{"type":"integer"}}}`,
			"brands": `{"type":"object","properties":{"name":{"type":"string"}}}`,
		}, nil)

	schemas, err := NewXoluSchemaClient(srv.URL, 2*time.Second).FetchSchemas(context.Background())
	if err != nil {
		t.Fatalf("FetchSchemas: %v", err)
	}
	if len(schemas.Entities) != 2 || schemas.Entities[0].Name != "assets" || schemas.Entities[1].Name != "brands" {
		t.Fatalf("entities must be loaded and sorted by name, got %+v", schemas.Entities)
	}
	props, _ := schemas.Entities[0].Schema["properties"].(map[string]interface{})
	if _, ok := props["brand_id"]; !ok {
		t.Fatalf("the entity's JSON Schema must be kept, got %+v", schemas.Entities[0].Schema)
	}
	if len(schemas.Primitives) != 0 {
		t.Fatalf("xolu does not define primitives; they stay empty, got %d", len(schemas.Primitives))
	}
}

func TestSchemaClient_RealXoluFormat_NoSchemasMakesNoDetailCalls(t *testing.T) {
	var hits []string
	srv := xoluSchemasServer(t, `{"count":0,"schemas":[]}`, nil, &hits)

	schemas, err := NewXoluSchemaClient(srv.URL, 2*time.Second).FetchSchemas(context.Background())
	if err != nil || len(schemas.Entities) != 0 {
		t.Fatalf("an empty list is a valid answer (seeds without schemas): %v, %+v", err, schemas)
	}
	if len(hits) != 1 {
		t.Fatalf("only the list must be requested, got %v", hits)
	}
}

func TestSchemaClient_RealXoluFormat_EntityWithoutSchemaIsSkipped(t *testing.T) {
	srv := xoluSchemasServer(t,
		`{"count":2,"schemas":[{"name":"assets"},{"name":"ghost"}]}`,
		map[string]string{"assets": `{"type":"object"}`}, nil)

	schemas, err := NewXoluSchemaClient(srv.URL, 2*time.Second).FetchSchemas(context.Background())
	if err != nil {
		t.Fatalf("a 404 for one entity (removed between the two calls) must not fail the load: %v", err)
	}
	if len(schemas.Entities) != 1 || schemas.Entities[0].Name != "assets" {
		t.Fatalf("expected only assets, got %+v", schemas.Entities)
	}
}

func TestSchemaClient_RealXoluFormat_DetailServerErrorFailsTheLoad(t *testing.T) {
	srv := xoluSchemasServer(t,
		`{"count":1,"schemas":[{"name":"assets"}]}`,
		map[string]string{"assets": "BOOM"}, nil)

	if _, err := NewXoluSchemaClient(srv.URL, 2*time.Second).FetchSchemas(context.Background()); err == nil {
		t.Fatal("a 5xx while reading a schema must fail the load so the previous snapshot is kept and the loader retries")
	}
}

func TestSchemaClient_RealXoluFormat_EntityNamesAreEscaped(t *testing.T) {
	var hits []string
	srv := xoluSchemasServer(t, `{"count":1,"schemas":[{"name":"a b/c"}]}`, nil, &hits)
	_, _ = NewXoluSchemaClient(srv.URL, 2*time.Second).FetchSchemas(context.Background())
	want := "/api/v1/schema/a%20b%2Fc"
	found := false
	for _, h := range hits {
		found = found || h == want
	}
	if !found {
		t.Fatalf("a name with a space and a slash must travel escaped as %s, got %v", want, hits)
	}
}

func TestSchemaClient_LegacyFormatStillParsed(t *testing.T) {
	srv := xoluSchemasServer(t, `{"primitives":[{"name":"walk","description":"d","inputSchema":{"type":"object"}}]}`, nil, nil)
	schemas, err := NewXoluSchemaClient(srv.URL, 2*time.Second).FetchSchemas(context.Background())
	if err != nil || len(schemas.Primitives) != 1 || schemas.Primitives[0].Name != "walk" {
		t.Fatalf("the original format must keep working: %v %+v", err, schemas)
	}
}

func TestSchemaClient_ManyEntities(t *testing.T) {
	names := []string{}
	details := map[string]string{}
	list := `{"count":30,"schemas":[`
	for i := 0; i < 30; i++ {
		n := fmt.Sprintf("e%02d", i)
		names = append(names, n)
		details[n] = `{"type":"object"}`
		if i > 0 {
			list += ","
		}
		list += `{"name":"` + n + `"}`
	}
	list += `]}`
	srv := xoluSchemasServer(t, list, details, nil)

	schemas, err := NewXoluSchemaClient(srv.URL, 2*time.Second).FetchSchemas(context.Background())
	if err != nil || len(schemas.Entities) != 30 {
		t.Fatalf("expected 30 entities, got %d (%v)", len(schemas.Entities), err)
	}
	for i, e := range schemas.Entities {
		if e.Name != names[i] {
			t.Fatalf("entities must keep a deterministic order: %d is %q, want %q", i, e.Name, names[i])
		}
	}
}

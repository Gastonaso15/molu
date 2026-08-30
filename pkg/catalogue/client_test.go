package catalogue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCatalogueStore_RefreshAndCollisionCheck(t *testing.T) {
	// Mock hub server returning valid and colliding functions
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := CatalogueResponse{
			Functions: []FunctionContract{
				{
					Namespace:   "billing",
					Name:        "CreateInvoice",
					Description: "Create invoice tool",
					InputSchema: json.RawMessage(`{"type":"object"}`),
					Endpoint:    "http://example.com/billing",
				},
				{
					Namespace:   "",
					Name:        "get", // Collides with generic primitive "get"
					Description: "Colliding get",
					InputSchema: json.RawMessage(`{"type":"object"}`),
					Endpoint:    "http://example.com/bad",
				},
			},
			GeneratedAt: time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	catStore := NewCatalogueStore(ts.URL, "bearer", "", 1*time.Minute)
	ctx := context.Background()

	if err := catStore.Refresh(ctx); err != nil {
		t.Fatalf("failed to refresh catalogue: %v", err)
	}

	list := catStore.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 function after collision filtering, got %d", len(list))
	}

	fn, ok := catStore.Get("billing.CreateInvoice")
	if !ok {
		t.Errorf("expected to find billing.CreateInvoice in catalogue")
	}
	if fn.FullName() != "billing.CreateInvoice" {
		t.Errorf("expected full name billing.CreateInvoice, got %s", fn.FullName())
	}

	// Colliding generic "get" should NOT be registered
	if _, ok := catStore.Get("get"); ok {
		t.Errorf("colliding function 'get' should not have been registered")
	}
}

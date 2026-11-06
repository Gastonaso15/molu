package hub_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/rpi/providers"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

func TestHubHTTPServer_Healthz(t *testing.T) {
	registry := hub.NewRegistry()
	server := hub.NewHubHTTPServer(registry, nil)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var res map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res["status"] != "ok" {
		t.Errorf("expected status ok, got %q", res["status"])
	}
}

func TestHubHTTPServer_RegistryEndpoints(t *testing.T) {
	registry := hub.NewRegistry()
	server := hub.NewHubHTTPServer(registry, nil)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// 1. Register tool
	reg := hub.ToolRegistration{
		PublisherID: "pub-1",
		Name:        "work_orders.CreateOrder",
		Description: "Create a work order",
		InputSchema: map[string]any{"type": "object"},
		Primitive:   false,
		Namespace:   "work_orders",
	}
	body, _ := json.Marshal(reg)
	resp, err := http.Post(ts.URL+"/api/v1/registry/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Register invalid tool -> CONTRACT_VIOLATION (400)
	invalidReg := hub.ToolRegistration{
		PublisherID: "pub-1",
		Name:        "NoNamespaceOrder", // domain tool without namespace
		Primitive:   false,
	}
	invBody, _ := json.Marshal(invalidReg)
	respInv, err := http.Post(ts.URL+"/api/v1/registry/register", "application/json", bytes.NewReader(invBody))
	if err != nil {
		t.Fatalf("register invalid failed: %v", err)
	}
	if respInv.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for contract violation, got %d", respInv.StatusCode)
	}
	var typedErr taxonomy.TypedError
	if err := json.NewDecoder(respInv.Body).Decode(&typedErr); err != nil {
		t.Fatalf("failed to decode typed error: %v", err)
	}
	respInv.Body.Close()
	if !strings.Contains(typedErr.Code, string(taxonomy.CodeContractViolation)) {
		t.Errorf("expected CONTRACT_VIOLATION, got %v", typedErr.Code)
	}

	// 3. List tools
	respList, err := http.Post(ts.URL+"/api/v1/registry/list", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if respList.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", respList.StatusCode)
	}
	var listRes struct {
		Tools []hub.ToolRegistration `json:"tools"`
	}
	if err := json.NewDecoder(respList.Body).Decode(&listRes); err != nil {
		t.Fatalf("failed to decode list: %v", err)
	}
	respList.Body.Close()
	if len(listRes.Tools) != 1 || listRes.Tools[0].Name != "work_orders.CreateOrder" {
		t.Fatalf("expected 1 tool, got %v", listRes.Tools)
	}

	// 4. Unregister tool
	unregPayload, _ := json.Marshal(map[string]string{
		"publisher_id": "pub-1",
		"name":         "work_orders.CreateOrder",
	})
	respUnreg, err := http.Post(ts.URL+"/api/v1/registry/unregister", "application/json", bytes.NewReader(unregPayload))
	if err != nil {
		t.Fatalf("unreg failed: %v", err)
	}
	if respUnreg.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", respUnreg.StatusCode)
	}
	respUnreg.Body.Close()

	// 5. Unregister non-existent tool -> NOT_FOUND (404)
	respUnregNotFound, err := http.Post(ts.URL+"/api/v1/registry/unregister", "application/json", bytes.NewReader(unregPayload))
	if err != nil {
		t.Fatalf("unreg not found failed: %v", err)
	}
	if respUnregNotFound.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", respUnregNotFound.StatusCode)
	}
	respUnregNotFound.Body.Close()
}

func TestHubHTTPServer_DiscoveryEndpoint(t *testing.T) {
	registry := hub.NewRegistry()
	provider := providers.NewAdmitAllProvider()
	discovery := hub.NewDiscovery(registry, provider)
	server := hub.NewHubHTTPServer(registry, discovery)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// 1. Missing tenant -> 400 CONTRACT_VIOLATION
	badCtx, _ := json.Marshal(rpi.AdmitContext{Tenant: ""})
	respBad, err := http.Post(ts.URL+"/api/v1/discovery/tools", "application/json", bytes.NewReader(badCtx))
	if err != nil {
		t.Fatalf("discovery bad ctx failed: %v", err)
	}
	if respBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", respBad.StatusCode)
	}
	respBad.Body.Close()

	// 2. Valid tenant on empty catalog -> 200 with code EMPTY
	validCtx, _ := json.Marshal(rpi.AdmitContext{Tenant: "acme"})
	respEmpty, err := http.Post(ts.URL+"/api/v1/discovery/tools", "application/json", bytes.NewReader(validCtx))
	if err != nil {
		t.Fatalf("discovery empty failed: %v", err)
	}
	if respEmpty.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", respEmpty.StatusCode)
	}
	var emptyRes struct {
		Tools []rpi.ScoredTool `json:"tools"`
		Code  string           `json:"code,omitempty"`
	}
	if err := json.NewDecoder(respEmpty.Body).Decode(&emptyRes); err != nil {
		t.Fatalf("decode empty response failed: %v", err)
	}
	respEmpty.Body.Close()
	if emptyRes.Code != "EMPTY" || len(emptyRes.Tools) != 0 {
		t.Errorf("expected EMPTY code with 0 tools, got %v", emptyRes)
	}

	// 3. Register a tool and query discovery -> 200 with admitted tools
	reg := hub.ToolRegistration{
		PublisherID: "pub-1",
		Name:        "work_orders.CreateOrder",
		Description: "Create a work order",
		InputSchema: map[string]any{"type": "object"},
		Primitive:   false,
		Namespace:   "work_orders",
	}
	regBody, _ := json.Marshal(reg)
	_, _ = http.Post(ts.URL+"/api/v1/registry/register", "application/json", bytes.NewReader(regBody))

	respOffered, err := http.Post(ts.URL+"/api/v1/discovery/tools", "application/json", bytes.NewReader(validCtx))
	if err != nil {
		t.Fatalf("discovery offered failed: %v", err)
	}
	if respOffered.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", respOffered.StatusCode)
	}
	var offeredRes struct {
		Tools []rpi.ScoredTool `json:"tools"`
	}
	if err := json.NewDecoder(respOffered.Body).Decode(&offeredRes); err != nil {
		t.Fatalf("decode offered response failed: %v", err)
	}
	respOffered.Body.Close()
	if len(offeredRes.Tools) != 1 || offeredRes.Tools[0].Name != "work_orders.CreateOrder" {
		t.Fatalf("expected 1 admitted tool, got %v", offeredRes.Tools)
	}
}

func TestHubHTTPServer_Concurrency(t *testing.T) {
	registry := hub.NewRegistry()
	provider := providers.NewAdmitAllProvider()
	discovery := hub.NewDiscovery(registry, provider)
	server := hub.NewHubHTTPServer(registry, discovery)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	const numRoutines = 20
	errCh := make(chan error, numRoutines*2)

	for i := 0; i < numRoutines; i++ {
		go func(id int) {
			reg := hub.ToolRegistration{
				PublisherID: "pub-concurrent",
				Name:        "work_orders.Tool" + string(rune('A'+id)),
				Description: "concurrent tool",
				InputSchema: map[string]any{"type": "object"},
				Namespace:   "work_orders",
			}
			body, _ := json.Marshal(reg)
			resp, err := http.Post(ts.URL+"/api/v1/registry/register", "application/json", bytes.NewReader(body))
			if err != nil {
				errCh <- err
				return
			}
			resp.Body.Close()
			errCh <- nil
		}(i)

		go func() {
			actx, _ := json.Marshal(rpi.AdmitContext{Tenant: "acme"})
			resp, err := http.Post(ts.URL+"/api/v1/discovery/tools", "application/json", bytes.NewReader(actx))
			if err != nil {
				errCh <- err
				return
			}
			resp.Body.Close()
			errCh <- nil
		}()
	}

	for i := 0; i < numRoutines*2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent request error: %v", err)
		}
	}
}


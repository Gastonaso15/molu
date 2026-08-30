package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ha1tch/molu/pkg/catalogue"
)

func TestHub_PublishHeartbeatCatalogueAndExpiry(t *testing.T) {
	hubServer := NewHubServer(":0", "tenant-test", 100*time.Millisecond, 20*time.Millisecond)
	handler := hubServer.Routes()

	// 1. Test /healthz and /readyz
	reqHealth := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rrHealth := httptest.NewRecorder()
	handler.ServeHTTP(rrHealth, reqHealth)
	if rrHealth.Code != http.StatusOK {
		t.Errorf("expected healthz 200, got %d", rrHealth.Code)
	}

	// 2. Test /publish
	contract := catalogue.FunctionContract{
		Namespace:   "billing",
		Name:        "CreateInvoice",
		Description: "Create invoice tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Endpoint:    "http://example.internal/invoice",
		Auth: catalogue.ContractAuth{
			Mode: "none",
		},
		RequiresConfirmation: true,
		Idempotent:           false,
		Cost:                 "moderate",
		Latency:              "sub-second",
	}

	bodyBytes, _ := json.Marshal(contract)
	reqPub := httptest.NewRequest(http.MethodPost, "/publish", bytes.NewReader(bodyBytes))
	reqPub.Header.Set("X-Publisher-ID", "billing-pub")
	rrPub := httptest.NewRecorder()
	handler.ServeHTTP(rrPub, reqPub)

	if rrPub.Code != http.StatusOK {
		t.Fatalf("publish failed with code %d: %s", rrPub.Code, rrPub.Body.String())
	}

	// 3. Test /catalogue
	reqCat := httptest.NewRequest(http.MethodGet, "/catalogue", nil)
	rrCat := httptest.NewRecorder()
	handler.ServeHTTP(rrCat, reqCat)

	if rrCat.Code != http.StatusOK {
		t.Fatalf("catalogue failed with code %d: %s", rrCat.Code, rrCat.Body.String())
	}

	var catResp catalogue.CatalogueResponse
	_ = json.NewDecoder(rrCat.Body).Decode(&catResp)
	if len(catResp.Functions) != 1 {
		t.Fatalf("expected 1 published function, got %d", len(catResp.Functions))
	}
	if catResp.Functions[0].FullName() != "billing.CreateInvoice" {
		t.Errorf("expected function billing.CreateInvoice, got %s", catResp.Functions[0].FullName())
	}

	// 4. Test /heartbeat
	reqHb := httptest.NewRequest(http.MethodPost, "/heartbeat", nil)
	reqHb.Header.Set("X-Publisher-ID", "billing-pub")
	rrHb := httptest.NewRecorder()
	handler.ServeHTTP(rrHb, reqHb)
	if rrHb.Code != http.StatusOK {
		t.Errorf("heartbeat failed with status %d", rrHb.Code)
	}

	// 5. Test expiration sweep
	time.Sleep(150 * time.Millisecond)
	hubServer.reapExpiredPublishers()

	// Verify catalogue is now empty after expiration
	reqCatAfter := httptest.NewRequest(http.MethodGet, "/catalogue", nil)
	rrCatAfter := httptest.NewRecorder()
	handler.ServeHTTP(rrCatAfter, reqCatAfter)

	var catRespAfter catalogue.CatalogueResponse
	_ = json.NewDecoder(rrCatAfter.Body).Decode(&catRespAfter)
	if len(catRespAfter.Functions) != 0 {
		t.Errorf("expected 0 functions after timeout expiry, got %d", len(catRespAfter.Functions))
	}
}

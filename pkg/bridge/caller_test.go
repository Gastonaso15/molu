package bridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

type mockResolver struct {
	secrets map[string]string
}

func (m *mockResolver) Resolve(ref string) (string, error) {
	if val, ok := m.secrets[ref]; ok {
		return val, nil
	}
	return "", ErrSecretNotFound
}

func TestHTTPDomainCaller_Success(t *testing.T) {
	var receivedMethod string
	var receivedContentType string
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"order_id": "WO-999", "status": "created"}`))
	}))
	defer server.Close()

	caller := NewHTTPDomainCaller(nil, nil, 2*time.Second)
	args := map[string]any{"customer": "Acme Corp", "amount": 150.0}

	result, err := caller.Call(context.Background(), server.URL, AuthMetadata{Mode: AuthModeNone}, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedMethod != http.MethodPost {
		t.Errorf("expected POST method, got %s", receivedMethod)
	}
	if !strings.Contains(receivedContentType, "application/json") {
		t.Errorf("expected application/json content-type, got %s", receivedContentType)
	}
	if receivedBody["customer"] != "Acme Corp" {
		t.Errorf("expected customer 'Acme Corp', got %v", receivedBody["customer"])
	}

	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any result, got %T", result)
	}
	if resMap["order_id"] != "WO-999" || resMap["status"] != "created" {
		t.Errorf("unexpected result map: %v", resMap)
	}
}

func TestHTTPDomainCaller_CorrelationHeaders(t *testing.T) {
	var receivedRequestID string
	var receivedTraceParent string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequestID = r.Header.Get("X-Request-ID")
		receivedTraceParent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	caller := NewHTTPDomainCaller(nil, nil, 2*time.Second)

	ctx := telemetry.WithRequestContext(context.Background(), &telemetry.RequestContext{
		RequestID: "req-ulid-12345",
		TraceID:   "0af7651916cd43dd8448eb211c80319c",
		SpanID:    "b7ad6b7169203331",
	})

	_, err := caller.Call(ctx, server.URL, AuthMetadata{Mode: AuthModeNone}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedRequestID != "req-ulid-12345" {
		t.Errorf("expected X-Request-ID 'req-ulid-12345', got '%s'", receivedRequestID)
	}
	expectedTraceParent := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	if receivedTraceParent != expectedTraceParent {
		t.Errorf("expected traceparent '%s', got '%s'", expectedTraceParent, receivedTraceParent)
	}
}

func TestHTTPDomainCaller_AuthHeaders(t *testing.T) {
	tests := []struct {
		name           string
		auth           AuthMetadata
		secrets        map[string]string
		expectedHeader string
		expectedValue  string
	}{
		{
			name: "Bearer token default header",
			auth: AuthMetadata{
				Mode:     AuthModeBearer,
				TokenRef: "service:orders",
			},
			secrets:        map[string]string{"service:orders": "secret-token-123"},
			expectedHeader: "Authorization",
			expectedValue:  "Bearer secret-token-123",
		},
		{
			name: "Bearer token custom header",
			auth: AuthMetadata{
				Mode:     AuthModeBearer,
				Header:   "X-Auth-Token",
				TokenRef: "service:orders",
			},
			secrets:        map[string]string{"service:orders": "custom-secret"},
			expectedHeader: "X-Auth-Token",
			expectedValue:  "Bearer custom-secret",
		},
		{
			name: "API key default header",
			auth: AuthMetadata{
				Mode:     AuthModeAPIKey,
				TokenRef: "partner:api-key",
			},
			secrets:        map[string]string{"partner:api-key": "partner-key-xyz"},
			expectedHeader: "X-API-Key",
			expectedValue:  "partner-key-xyz",
		},
		{
			name: "API key custom header",
			auth: AuthMetadata{
				Mode:     AuthModeAPIKey,
				Header:   "X-Custom-Key",
				TokenRef: "partner:api-key",
			},
			secrets:        map[string]string{"partner:api-key": "partner-key-xyz"},
			expectedHeader: "X-Custom-Key",
			expectedValue:  "partner-key-xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedHeaderValue string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedHeaderValue = r.Header.Get(tt.expectedHeader)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status": "ok"}`))
			}))
			defer server.Close()

			resolver := &mockResolver{secrets: tt.secrets}
			caller := NewHTTPDomainCaller(nil, resolver, 2*time.Second)

			_, err := caller.Call(context.Background(), server.URL, tt.auth, nil)
			if err != nil {
				t.Fatalf("unexpected call error: %v", err)
			}

			if capturedHeaderValue != tt.expectedValue {
				t.Errorf("header %s: expected %q, got %q", tt.expectedHeader, tt.expectedValue, capturedHeaderValue)
			}
		})
	}
}

func TestHTTPDomainCaller_MissingSecret_ContractViolation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not receive any request when secret resolution fails")
	}))
	defer server.Close()

	resolver := &mockResolver{secrets: map[string]string{}}
	caller := NewHTTPDomainCaller(nil, resolver, 2*time.Second)

	ctx := telemetry.WithRequestContext(context.Background(), &telemetry.RequestContext{
		TenantID: "tenant-demo",
		Tool:     "orders.Create",
	})

	auth := AuthMetadata{
		Mode:     AuthModeBearer,
		TokenRef: "missing:token",
	}

	_, err := caller.Call(ctx, server.URL, auth, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	typedErr, ok := err.(*taxonomy.TypedError)
	if !ok {
		t.Fatalf("expected *taxonomy.TypedError, got %T", err)
	}

	if typedErr.Code != taxonomy.PrefixMolu+string(taxonomy.CodeContractViolation) {
		t.Errorf("expected CONTRACT_VIOLATION code, got %s", typedErr.Code)
	}
}

func TestHTTPDomainCaller_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "order not found"}`))
	}))
	defer server.Close()

	caller := NewHTTPDomainCaller(nil, nil, 2*time.Second)

	ctx := telemetry.WithRequestContext(context.Background(), &telemetry.RequestContext{
		TenantID: "tenant-demo",
	})

	_, err := caller.Call(ctx, server.URL, AuthMetadata{Mode: AuthModeNone}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	typedErr, ok := err.(*taxonomy.TypedError)
	if !ok {
		t.Fatalf("expected *taxonomy.TypedError, got %T", err)
	}

	if typedErr.Code != taxonomy.PrefixMolu+string(taxonomy.CodeNotFound) {
		t.Errorf("expected NOT_FOUND code, got %s", typedErr.Code)
	}
}

func TestHTTPDomainCaller_SubstrateUnavailable_5xx(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error": "upstream failure"}`))
			}))
			defer server.Close()

			caller := NewHTTPDomainCaller(nil, nil, 2*time.Second)

			_, err := caller.Call(context.Background(), server.URL, AuthMetadata{Mode: AuthModeNone}, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			typedErr, ok := err.(*taxonomy.TypedError)
			if !ok {
				t.Fatalf("expected *taxonomy.TypedError, got %T", err)
			}

			if typedErr.Code != taxonomy.PrefixMolu+string(taxonomy.CodeSubstrateUnavailable) {
				t.Errorf("expected SUBSTRATE_UNAVAILABLE code, got %s", typedErr.Code)
			}
			if typedErr.Details["lastSuccessfulContact"] == nil || typedErr.Details["nextRetryAt"] == nil {
				t.Errorf("missing retry metadata in details: %v", typedErr.Details)
			}
		})
	}
}

func TestHTTPDomainCaller_SubstrateUnavailable_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	caller := NewHTTPDomainCaller(nil, nil, 20*time.Millisecond)

	_, err := caller.Call(context.Background(), server.URL, AuthMetadata{Mode: AuthModeNone}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	typedErr, ok := err.(*taxonomy.TypedError)
	if !ok {
		t.Fatalf("expected *taxonomy.TypedError, got %T: %v", err, err)
	}

	if typedErr.Code != taxonomy.PrefixMolu+string(taxonomy.CodeSubstrateUnavailable) {
		t.Errorf("expected SUBSTRATE_UNAVAILABLE code, got %s", typedErr.Code)
	}
}

func TestHTTPDomainCaller_SubstrateUnavailable_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body>502 Bad Gateway</body></html>`))
	}))
	defer server.Close()

	caller := NewHTTPDomainCaller(nil, nil, 2*time.Second)

	_, err := caller.Call(context.Background(), server.URL, AuthMetadata{Mode: AuthModeNone}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	typedErr, ok := err.(*taxonomy.TypedError)
	if !ok {
		t.Fatalf("expected *taxonomy.TypedError, got %T: %v", err, err)
	}

	if typedErr.Code != taxonomy.PrefixMolu+string(taxonomy.CodeSubstrateUnavailable) {
		t.Errorf("expected SUBSTRATE_UNAVAILABLE code, got %s", typedErr.Code)
	}
}

func TestHTTPDomainCaller_EmptyCollection(t *testing.T) {
	emptyPayloads := []string{
		`[]`,
		`{"data": []}`,
		`{"items": []}`,
		`{"rows": []}`,
		`{"results": []}`,
		`{"count": 0}`,
		`{"total": 0}`,
	}

	for _, payload := range emptyPayloads {
		t.Run(payload, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(payload))
			}))
			defer server.Close()

			caller := NewHTTPDomainCaller(nil, nil, 2*time.Second)
			ctx := telemetry.WithRequestContext(context.Background(), &telemetry.RequestContext{
				TenantID: "tenant-demo",
				Tool:     "orders.List",
			})

			_, err := caller.Call(ctx, server.URL, AuthMetadata{Mode: AuthModeNone}, map[string]any{"status": "open"})
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			typedErr, ok := err.(*taxonomy.TypedError)
			if !ok {
				t.Fatalf("expected *taxonomy.TypedError, got %T: %v", err, err)
			}

			if typedErr.Code != taxonomy.PrefixMolu+string(taxonomy.CodeEmpty) {
				t.Errorf("expected EMPTY code, got %s", typedErr.Code)
			}
		})
	}
}

func TestHTTPDomainCaller_VerbatimXoluError(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		expectedCode string
		expectedMsg  string
	}{
		{
			name:         "Flat xolu error code",
			status:       http.StatusConflict,
			body:         `{"code": "XOLU-INVALID_TRANSITION", "message": "guard failed: order is cancelled"}`,
			expectedCode: "XOLU-INVALID_TRANSITION",
			expectedMsg:  "guard failed: order is cancelled",
		},
		{
			name:         "Nested error envelope",
			status:       http.StatusBadRequest,
			body:         `{"error": {"code": "XOLU-SUBSTRATE_UNAVAILABLE", "message": "database locked"}}`,
			expectedCode: "XOLU-SUBSTRATE_UNAVAILABLE",
			expectedMsg:  "database locked",
		},
		{
			name:         "Xolu 404 with verbatim code",
			status:       http.StatusNotFound,
			body:         `{"code": "XOLU-NOT_FOUND", "message": "machine instance 45 not found"}`,
			expectedCode: "XOLU-NOT_FOUND",
			expectedMsg:  "machine instance 45 not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			caller := NewHTTPDomainCaller(nil, nil, 2*time.Second)

			_, err := caller.Call(context.Background(), server.URL, AuthMetadata{Mode: AuthModeNone}, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			typedErr, ok := err.(*taxonomy.TypedError)
			if !ok {
				t.Fatalf("expected *taxonomy.TypedError, got %T: %v", err, err)
			}

			if typedErr.Code != tt.expectedCode {
				t.Errorf("expected verbatim code %q, got %q", tt.expectedCode, typedErr.Code)
			}
			if typedErr.Message != tt.expectedMsg {
				t.Errorf("expected message %q, got %q", tt.expectedMsg, typedErr.Message)
			}
		})
	}
}


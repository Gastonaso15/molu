package orders

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "s3cret"

func newTestServer(fs *fakeSubstrate) *httptest.Server {
	return httptest.NewServer(NewServer(NewService(fs), testToken).Handler())
}

func post(t *testing.T, url, token, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestOrderServer_Healthz_NoAuth(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", resp, err)
	}
}

func TestOrderServer_RejectsMissingOrWrongToken(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	for name, tok := range map[string]string{"missing": "", "wrong": "nope"} {
		t.Run(name, func(t *testing.T) {
			resp, _ := post(t, srv.URL+"/api/v1/orders/create", tok, `{"title":"x"}`)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

func TestOrderServer_Create(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	resp, out := post(t, srv.URL+"/api/v1/orders/create", testToken, `{"title":"Replace pump","description":"d"}`)
	if resp.StatusCode != 200 || out["state"] != "Open" || out["title"] != "Replace pump" || out["id"] == "" {
		t.Fatalf("status=%d body=%v", resp.StatusCode, out)
	}
}

func TestOrderServer_Create_BadRequests(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	for name, body := range map[string]string{"invalid json": `{`, "missing title": `{"description":"d"}`, "unknown field": `{"title":"x","bogus":1}`} {
		t.Run(name, func(t *testing.T) {
			resp, _ := post(t, srv.URL+"/api/v1/orders/create", testToken, body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestOrderServer_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/orders/create", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestOrderServer_Get_NotFoundHasNoXoluCode(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	resp, out := post(t, srv.URL+"/api/v1/orders/get", testToken, `{"order_id":"9999"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	// The bridge maps a bare 404 to NOT_FOUND; a XOLU- code here would bypass that.
	if c, _ := out["code"].(string); strings.HasPrefix(c, "XOLU-") {
		t.Fatalf("404 body must not carry a XOLU- code, got %v", out)
	}
}

func TestOrderServer_Get_Found(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	_, created := post(t, srv.URL+"/api/v1/orders/create", testToken, `{"title":"t"}`)
	resp, out := post(t, srv.URL+"/api/v1/orders/get", testToken, `{"order_id":"`+created["id"].(string)+`"}`)
	if resp.StatusCode != 200 || out["title"] != "t" {
		t.Fatalf("status=%d body=%v", resp.StatusCode, out)
	}
}

func TestOrderServer_Advance_InvalidTransition409Verbatim(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	_, created := post(t, srv.URL+"/api/v1/orders/create", testToken, `{"title":"t"}`)
	id := created["id"].(string)

	resp, out := post(t, srv.URL+"/api/v1/orders/advance", testToken, `{"order_id":"`+id+`","input":"complete","payload":{"resolution":"x"}}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%v)", resp.StatusCode, out)
	}
	if out["code"] != "XOLU-INVALID_TRANSITION" || out["message"] != `no transition for input "complete" from state "Open"` {
		t.Fatalf("envelope = %v", out)
	}
	details, _ := out["details"].(map[string]any)
	if details["xolu_code"] != "XOLU-FSM003" || details["state"] != "Open" {
		t.Fatalf("details = %v", details)
	}
}

func TestOrderServer_Advance_Success(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	_, created := post(t, srv.URL+"/api/v1/orders/create", testToken, `{"title":"t"}`)
	resp, out := post(t, srv.URL+"/api/v1/orders/advance", testToken, `{"order_id":"`+created["id"].(string)+`","input":"start"}`)
	if resp.StatusCode != 200 || out["current"] != "InProgress" {
		t.Fatalf("status=%d body=%v", resp.StatusCode, out)
	}
}

func TestOrderServer_Advance_UnknownOrderIs404(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	resp, _ := post(t, srv.URL+"/api/v1/orders/advance", testToken, `{"order_id":"404","input":"start"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestOrderServer_SubstrateDown503WithRetryMetadata(t *testing.T) {
	fs := newFake()
	srv := newTestServer(fs)
	defer srv.Close()
	fs.failWith = errors.New("connection refused")

	resp, out := post(t, srv.URL+"/api/v1/orders/create", testToken, `{"title":"t"}`)
	if resp.StatusCode != http.StatusServiceUnavailable || out["code"] != "XOLU-SUBSTRATE_UNAVAILABLE" {
		t.Fatalf("status=%d body=%v", resp.StatusCode, out)
	}
	details, _ := out["details"].(map[string]any)
	if details["nextRetryAt"] == nil || details["attemptNumber"] == nil || details["maxAttempts"] == nil {
		t.Fatalf("retry metadata missing: %v", details)
	}
}

func TestOrderServer_EchoesRequestID(t *testing.T) {
	srv := newTestServer(newFake())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/orders/create", strings.NewReader(`{"title":"t"}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Request-ID", "req-123")
	resp, _ := http.DefaultClient.Do(req)
	if got := resp.Header.Get("X-Request-ID"); got != "req-123" {
		t.Fatalf("X-Request-ID = %q", got)
	}
}

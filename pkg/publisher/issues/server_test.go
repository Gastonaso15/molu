package issues_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Gastonaso15/molu/pkg/publisher/issues"
)

const testToken = "s3cret-token"

func newTestIssueServer(mx issues.XoluClient) *httptest.Server {
	svc := issues.NewIssueService(mx)
	srv := issues.NewServer(svc, testToken)
	return httptest.NewServer(srv.Handler())
}

func postIssue(t *testing.T, url, token, body string) (*http.Response, map[string]any) {
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

func TestIssueServer_Healthz_NoAuth(t *testing.T) {
	srv := newTestIssueServer(newMockXolu())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz failed: %v, status: %d", err, resp.StatusCode)
	}
}

func TestIssueServer_RejectsMissingOrWrongToken(t *testing.T) {
	srv := newTestIssueServer(newMockXolu())
	defer srv.Close()

	for name, tok := range map[string]string{"missing": "", "wrong": "wrong-token"} {
		t.Run(name, func(t *testing.T) {
			resp, _ := postIssue(t, srv.URL+"/api/v1/issues/create", tok, `{"title":"Sample"}`)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected status 401, got %d", resp.StatusCode)
			}
		})
	}
}

func TestIssueServer_MethodNotAllowed(t *testing.T) {
	srv := newTestIssueServer(newMockXolu())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/issues/create", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", resp.StatusCode)
	}
}

func TestIssueServer_Create_Success(t *testing.T) {
	srv := newTestIssueServer(newMockXolu())
	defer srv.Close()

	resp, out := postIssue(t, srv.URL+"/api/v1/issues/create", testToken, `{"tenant":"tenant-1","title":"Coolant leak","priority":"high","severity":"minor"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", resp.StatusCode, out)
	}
	if out["title"] != "Coolant leak" || out["state"] != "reported" || out["id"] == nil {
		t.Fatalf("unexpected create response: %v", out)
	}
	if out["sla_first_response_deadline"] == nil || out["sla_resolution_deadline"] == nil {
		t.Fatalf("expected SLA deadlines in response: %v", out)
	}
}

func TestIssueServer_Create_BadRequests(t *testing.T) {
	srv := newTestIssueServer(newMockXolu())
	defer srv.Close()

	cases := map[string]string{
		"invalid json":    `{`,
		"missing title":   `{"tenant":"tenant-1"}`,
		"unknown field":   `{"title":"T","unexpected_field":1}`,
		"invalid priority": `{"title":"T","priority":"ultra"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp, _ := postIssue(t, srv.URL+"/api/v1/issues/create", testToken, body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected status 400 for %s, got %d", name, resp.StatusCode)
			}
		})
	}
}

func TestIssueServer_Get_SuccessAndNotFound(t *testing.T) {
	srv := newTestIssueServer(newMockXolu())
	defer srv.Close()

	_, created := postIssue(t, srv.URL+"/api/v1/issues/create", testToken, `{"tenant":"tenant-1","title":"Get test issue"}`)
	issueID := int64(created["id"].(float64))

	// Found
	body, _ := json.Marshal(map[string]any{"tenant": "tenant-1", "issue_id": issueID})
	resp, out := postIssue(t, srv.URL+"/api/v1/issues/get", testToken, string(body))
	if resp.StatusCode != http.StatusOK || out["title"] != "Get test issue" {
		t.Fatalf("get failed: status %d, body %v", resp.StatusCode, out)
	}

	// Not Found
	notFoundBody := `{"tenant":"tenant-1","issue_id":99999}`
	resp, notFoundOut := postIssue(t, srv.URL+"/api/v1/issues/get", testToken, notFoundBody)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing issue, got %d", resp.StatusCode)
	}
	// Execution Bridge expects a bare 404 without a XOLU- code
	if c, _ := notFoundOut["code"].(string); strings.HasPrefix(c, "XOLU-") {
		t.Fatalf("404 response must not carry a XOLU- code, got %v", notFoundOut)
	}
}

func TestIssueServer_Advance_InvalidTransition409Verbatim(t *testing.T) {
	srv := newTestIssueServer(newMockXolu())
	defer srv.Close()

	_, created := postIssue(t, srv.URL+"/api/v1/issues/create", testToken, `{"tenant":"tenant-1","title":"Transition test"}`)
	issueID := int64(created["id"].(float64))

	// Trying to close directly from reported
	advanceBody, _ := json.Marshal(map[string]any{
		"tenant":     "tenant-1",
		"issue_id":   issueID,
		"transition": "close",
	})
	resp, out := postIssue(t, srv.URL+"/api/v1/issues/advance", testToken, string(advanceBody))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d (%v)", resp.StatusCode, out)
	}
	if out["code"] != "XOLU-INVALID_TRANSITION" {
		t.Fatalf("expected XOLU-INVALID_TRANSITION code, got %v", out["code"])
	}
	if out["message"] != "cannot transition from reported via close" {
		t.Fatalf("expected exact verbatim message, got %q", out["message"])
	}
}

func TestIssueServer_Advance_Success(t *testing.T) {
	srv := newTestIssueServer(newMockXolu())
	defer srv.Close()

	_, created := postIssue(t, srv.URL+"/api/v1/issues/create", testToken, `{"tenant":"tenant-1","title":"Advance success test"}`)
	issueID := int64(created["id"].(float64))

	advanceBody, _ := json.Marshal(map[string]any{
		"tenant":     "tenant-1",
		"issue_id":   issueID,
		"transition": "start",
		"actor_id":   55,
	})
	resp, out := postIssue(t, srv.URL+"/api/v1/issues/advance", testToken, string(advanceBody))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %v", resp.StatusCode, out)
	}
	if out["new_state"] != "in_progress" || out["previous_state"] != "reported" {
		t.Fatalf("unexpected states: %v", out)
	}
	issueMap, _ := out["issue"].(map[string]any)
	if issueMap["first_response_at"] == nil {
		t.Fatalf("expected first_response_at populated, got %v", issueMap)
	}
}

func TestIssueServer_SubstrateDown503WithRetryMetadata(t *testing.T) {
	mockX := newMockXolu()
	mockX.substrateErr = errors.New("substrate unreachable")

	srv := newTestIssueServer(mockX)
	defer srv.Close()

	resp, out := postIssue(t, srv.URL+"/api/v1/issues/create", testToken, `{"tenant":"tenant-1","title":"Failing"}`)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %v", resp.StatusCode, out)
	}
	if out["code"] != "XOLU-SUBSTRATE_UNAVAILABLE" {
		t.Fatalf("expected XOLU-SUBSTRATE_UNAVAILABLE, got %v", out["code"])
	}
	details, _ := out["details"].(map[string]any)
	if details["nextRetryAt"] == nil || details["attemptNumber"] == nil || details["maxAttempts"] == nil {
		t.Fatalf("missing retry metadata: %v", details)
	}
}

func TestIssueServer_EchoesRequestID(t *testing.T) {
	srv := newTestIssueServer(newMockXolu())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/issues/create", strings.NewReader(`{"title":"T"}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Request-ID", "req-issue-999")
	resp, _ := http.DefaultClient.Do(req)
	if got := resp.Header.Get("X-Request-ID"); got != "req-issue-999" {
		t.Fatalf("X-Request-ID header mismatch, got %q", got)
	}
}


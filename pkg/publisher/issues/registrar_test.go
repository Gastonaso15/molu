package issues_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/publisher/issues"
)

func fastIssueBackoff() issues.Backoff {
	return issues.Backoff{Initial: time.Millisecond, Max: 5 * time.Millisecond, MaxAttempts: 6}
}

func newRealHubForIssues(t *testing.T) (*httptest.Server, *hub.Registry) {
	t.Helper()
	reg := hub.NewRegistry()
	provider, err := hub.NewRPIProvider("admit-all", "", "")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(hub.NewHubHTTPServer(reg, hub.NewDiscovery(reg, provider)).Handler())
	t.Cleanup(srv.Close)
	return srv, reg
}

func TestIssueRegistrar_RegistersThreeNamespacedTools(t *testing.T) {
	srv, reg := newRealHubForIssues(t)
	r := issues.NewRegistrar(srv.URL, "http://publisher:8093/", fastIssueBackoff())

	if err := r.Register(context.Background()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	tools := reg.List(issues.PublisherID)
	if len(tools) != 3 {
		t.Fatalf("registered %d tools, want 3: %+v", len(tools), tools)
	}

	byName := map[string]hub.ToolRegistration{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}

	want := map[string]string{
		"issues.CreateIssue":  "http://publisher:8093/api/v1/issues/create",
		"issues.GetIssue":     "http://publisher:8093/api/v1/issues/get",
		"issues.AdvanceIssue": "http://publisher:8093/api/v1/issues/advance",
	}

	for name, endpoint := range want {
		tl, ok := byName[name]
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tl.Namespace != issues.Namespace || tl.Primitive || tl.Description == "" || tl.InputSchema["type"] != "object" {
			t.Errorf("%s: bad registration metadata %+v", name, tl)
		}
		if tl.Metadata["endpoint"] != endpoint {
			t.Errorf("%s endpoint = %v, want %s", name, tl.Metadata["endpoint"], endpoint)
		}
		auth, _ := tl.Metadata["auth"].(map[string]any)
		if auth["mode"] != "bearer" || auth["token_ref"] != issues.TokenRef {
			t.Errorf("%s auth = %v", name, auth)
		}
	}
}

func TestIssueRegistrar_RegisterIsIdempotent(t *testing.T) {
	srv, reg := newRealHubForIssues(t)
	r := issues.NewRegistrar(srv.URL, "http://p", fastIssueBackoff())
	for i := 0; i < 2; i++ {
		if err := r.Register(context.Background()); err != nil {
			t.Fatalf("Register #%d: %v", i, err)
		}
	}
	if n := len(reg.List(issues.PublisherID)); n != 3 {
		t.Fatalf("%d tools after re-register, want 3", n)
	}
}

func TestIssueRegistrar_RetriesUntilHubIsUp(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := issues.NewRegistrar(srv.URL, "http://p", fastIssueBackoff()).Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if calls.Load() < 5 { // 2 failures + 3 tools
		t.Fatalf("only %d calls; backoff did not retry", calls.Load())
	}
}

func TestIssueRegistrar_ContractRejectionIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "contract violation", http.StatusBadRequest)
	}))
	defer srv.Close()

	if err := issues.NewRegistrar(srv.URL, "http://p", fastIssueBackoff()).Register(context.Background()); err == nil {
		t.Fatal("expected error on 400 Bad Request")
	}
	if calls.Load() != 1 {
		t.Fatalf("%d calls; 400 must not be retried", calls.Load())
	}
}

func TestIssueRegistrar_GivesUpAfterMaxAttempts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	b := fastIssueBackoff()
	b.MaxAttempts = 3
	if err := issues.NewRegistrar(srv.URL, "http://p", b).Register(context.Background()); err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
}

func TestIssueRegistrar_StopsWhenContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	b := issues.Backoff{Initial: 50 * time.Millisecond, Max: 50 * time.Millisecond, MaxAttempts: 100}
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()

	start := time.Now()
	err := issues.NewRegistrar(srv.URL, "http://p", b).Register(ctx)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("err=%v after %v; expected prompt cancellation", err, time.Since(start))
	}
}

func TestIssueRegistrar_UnregisterRemovesAllTools(t *testing.T) {
	srv, reg := newRealHubForIssues(t)
	r := issues.NewRegistrar(srv.URL, "http://p", fastIssueBackoff())
	if err := r.Register(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Unregister(context.Background()); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}
	if n := len(reg.List(issues.PublisherID)); n != 0 {
		t.Fatalf("%d tools left after unregister", n)
	}
}

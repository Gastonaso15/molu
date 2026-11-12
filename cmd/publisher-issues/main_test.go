package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/publisher/issues"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestRun_RejectsMissingConfig(t *testing.T) {
	err := run(context.Background(), env(map[string]string{}), nil)
	if err == nil {
		t.Fatal("want configuration error")
	}
	for _, name := range []string{"MOLU_PUBLISHER_ISSUES_HUB_URL", "MOLU_PUBLISHER_ISSUES_XOLU_URL", "MOLU_PUBLISHER_ISSUES_API_TOKEN"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error should mention %s: %v", name, err)
		}
	}
}

func TestRun_RegistersServesAndUnregistersOnShutdown(t *testing.T) {
	reg := hub.NewRegistry()
	provider, _ := hub.NewRPIProvider("admit-all", "", "")
	hubSrv := httptest.NewServer(hub.NewHubHTTPServer(reg, hub.NewDiscovery(reg, provider)).Handler())
	defer hubSrv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	addrCh := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, env(map[string]string{
			"MOLU_PUBLISHER_ISSUES_HTTP_ADDR": "127.0.0.1:0",
			"MOLU_PUBLISHER_ISSUES_HUB_URL":   hubSrv.URL,
			"MOLU_PUBLISHER_ISSUES_XOLU_URL":  "http://127.0.0.1:1", // not contacted during registration
			"MOLU_PUBLISHER_ISSUES_API_TOKEN": "tok",
		}), func(a string) { addrCh <- a })
	}()

	addr := <-addrCh
	deadline := time.Now().Add(3 * time.Second)
	for len(reg.List(issues.PublisherID)) != 3 {
		if time.Now().After(deadline) {
			t.Fatal("tools were not registered in the hub")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if resp, err := http.Get("http://" + addr + "/healthz"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", resp, err)
	}

	tl := reg.List(issues.PublisherID)[0]
	if ep, _ := tl.Metadata["endpoint"].(string); !strings.HasPrefix(ep, "http://"+addr+"/api/v1/issues/") {
		t.Errorf("endpoint %q should be derived from the bound address %s", ep, addr)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v on graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after cancel")
	}

	if n := len(reg.List(issues.PublisherID)); n != 0 {
		t.Fatalf("%d tools still registered after shutdown", n)
	}
}


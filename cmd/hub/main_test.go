package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestHubMain_RunAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	env := map[string]string{
		"MOLU_HUB_HTTP_ADDR":     ":18091",
		"MOLU_HUB_RPI_PROVIDER": "admit-all",
	}
	getenv := func(key string) string {
		return env[key]
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, getenv)
	}()

	// Wait for server to start listening
	var resp *http.Response
	var err error
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		resp, err = http.Get("http://127.0.0.1:18091/healthz")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	if err != nil {
		t.Fatalf("failed to connect to Hub HTTP server: %v", err)
	}

	// Trigger shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run returned error on shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Hub server shutdown")
	}
}


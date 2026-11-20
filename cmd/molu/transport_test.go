package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/config"
	"github.com/Gastonaso15/molu/pkg/mcp"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

func testLogger() telemetry.Logger {
	cfg := telemetry.LoadConfigFromEnv()
	cfg.Component = telemetry.ComponentMoluFront
	return telemetry.NewLogger(cfg)
}

func startHTTP(t *testing.T, cfg *config.Config) (base string, stop func() error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, ln, mcp.NewServer(mcp.ServerConfig{}), cfg, testLogger()) }()
	return "http://" + ln.Addr().String(), func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(8 * time.Second):
			t.Fatal("serveHTTP did not return after cancel")
			return nil
		}
	}
}

func TestServeHTTP_BearerTokenFromFile(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base, stop := startHTTP(t, &config.Config{Transport: "streamable-http", HTTPAuth: "bearer", HTTPBearerTokenFile: tokenFile})

	call := func(token string) int {
		req, _ := http.NewRequest(http.MethodPost, base+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := call(""); got != http.StatusUnauthorized {
		t.Errorf("no token: want 401, got %d", got)
	}
	if got := call("file-token"); got != http.StatusOK {
		t.Errorf("token from file (trailing newline trimmed): want 200, got %d", got)
	}
	if err := stop(); err != nil {
		t.Fatalf("clean shutdown expected, got %v", err)
	}
}

func TestServeHTTP_MissingTokenFileFails(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	err := serveHTTP(context.Background(), ln, mcp.NewServer(mcp.ServerConfig{}),
		&config.Config{HTTPAuth: "bearer", HTTPBearerTokenFile: "/nonexistent/token"}, testLogger())
	if err == nil {
		t.Fatal("an unreadable bearer token file must stop startup, not leave the endpoint open")
	}
}

// Graceful shutdown must not wait out its timeout because of an open SSE stream.
func TestServeHTTP_ShutdownWithOpenStream(t *testing.T) {
	base, stop := startHTTP(t, &config.Config{Transport: "streamable-http", HTTPAuth: "none"})

	req, _ := http.NewRequest(http.MethodGet, base+"/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	bufio.NewReader(resp.Body).ReadString('\n') // stream is live

	start := time.Now()
	if err := stop(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("shutdown took %v: open streams were not released", d)
	}
	io.Copy(io.Discard, resp.Body)
}

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/schema"
)

func newHTTPTestServer(t *testing.T, cfg HTTPConfig) (*Server, *httptest.Server) {
	t.Helper()
	srv := NewServer(ServerConfig{Provider: &mockPrimitiveProvider{primitives: []schema.PrimitiveSchema{{Name: "walk", Description: "d"}}}})
	h, err := NewHTTPHandler(srv, cfg)
	if err != nil {
		t.Fatalf("NewHTTPHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(func() { h.Close(); ts.Close() })
	return srv, ts
}

func post(t *testing.T, url, body string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

const listReq = `{"jsonrpc":"2.0","id":7,"method":"tools/list"}`

func TestHTTPTransport_PostJSON(t *testing.T) {
	_, ts := newHTTPTestServer(t, HTTPConfig{})
	resp := post(t, ts.URL+"/mcp", listReq, nil)

	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, content-type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var reply struct {
		ID     int `json:"id"`
		Result struct {
			Tools []ToolDescriptor `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if reply.ID != 7 || len(reply.Result.Tools) != 1 || reply.Result.Tools[0].Name != "walk" {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}

func TestHTTPTransport_PostSSE(t *testing.T) {
	_, ts := newHTTPTestServer(t, HTTPConfig{})
	resp := post(t, ts.URL+"/mcp", listReq, map[string]string{"Accept": "application/json, text/event-stream"})

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type %q, want text/event-stream", ct)
	}
	raw, _ := io.ReadAll(resp.Body)
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "data: ") || !strings.Contains(line, `"id":7`) {
		t.Fatalf("expected one SSE data event with the reply, got %q", raw)
	}
}

func TestHTTPTransport_NotificationGets202(t *testing.T) {
	_, ts := newHTTPTestServer(t, HTTPConfig{})
	resp := post(t, ts.URL+"/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("a notification has no reply: want 202, got %d", resp.StatusCode)
	}
}

func TestHTTPTransport_ParseErrorKeepsJSONRPCShape(t *testing.T) {
	_, ts := newHTTPTestServer(t, HTTPConfig{})
	resp := post(t, ts.URL+"/mcp", `{not json`, nil)
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), "-32700") {
		t.Fatalf("a malformed body must get the JSON-RPC parse error, got %d %s", resp.StatusCode, raw)
	}
}

func TestHTTPTransport_MethodsAndPath(t *testing.T) {
	_, ts := newHTTPTestServer(t, HTTPConfig{})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/mcp", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PUT: want 405, got %d", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/other")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path: want 404, got %d", resp.StatusCode)
	}
}

func TestHTTPTransport_BearerAuth(t *testing.T) {
	_, ts := newHTTPTestServer(t, HTTPConfig{Auth: "bearer", BearerToken: "s3cret"})

	for name, hdr := range map[string]map[string]string{
		"no header":    nil,
		"wrong token":  {"Authorization": "Bearer nope"},
		"wrong scheme": {"Authorization": "Basic s3cret"},
	} {
		if resp := post(t, ts.URL+"/mcp", listReq, hdr); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d", name, resp.StatusCode)
		}
	}
	if resp := post(t, ts.URL+"/mcp", listReq, map[string]string{"Authorization": "Bearer s3cret"}); resp.StatusCode != 200 {
		t.Fatalf("valid token: want 200, got %d", resp.StatusCode)
	}
	// GET is protected too.
	resp, _ := http.Get(ts.URL + "/mcp")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET without token: want 401, got %d", resp.StatusCode)
	}
}

func TestHTTPTransport_ConfigValidation(t *testing.T) {
	srv := NewServer(ServerConfig{})
	if _, err := NewHTTPHandler(srv, HTTPConfig{Auth: "bearer"}); err == nil {
		t.Error("bearer without a token must be rejected")
	}
	if _, err := NewHTTPHandler(srv, HTTPConfig{Auth: "mtls"}); err == nil {
		t.Error("mtls is not implemented and must be rejected, not silently open")
	}
	if _, err := NewHTTPHandler(srv, HTTPConfig{Auth: "none"}); err != nil {
		t.Errorf("none must be accepted: %v", err)
	}
}

func readEvent(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				done <- b.String()
				return
			}
			if line == "\n" {
				done <- b.String()
				return
			}
			b.WriteString(line)
		}
	}()
	select {
	case s := <-done:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for an SSE event")
		return ""
	}
}

func TestHTTPTransport_GetSSE_ListChangedNotification(t *testing.T) {
	srv, ts := newHTTPTestServer(t, HTTPConfig{Heartbeat: time.Hour})

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}
	br := bufio.NewReader(resp.Body)
	if first := readEvent(t, br); !strings.HasPrefix(first, ":") {
		t.Fatalf("stream must open with a comment so clients see it is live, got %q", first)
	}

	srv.UpdatePrimitives([]schema.PrimitiveSchema{{Name: "walk"}, {Name: "get"}})
	ev := readEvent(t, br)
	if !strings.HasPrefix(ev, "data: ") || !strings.Contains(ev, "notifications/tools/list_changed") {
		t.Fatalf("expected the list_changed notification, got %q", ev)
	}
}

func TestHTTPTransport_GetSSE_NeedsEventStreamAccept(t *testing.T) {
	_, ts := newHTTPTestServer(t, HTTPConfig{})
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	req.Header.Set("Accept", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("want 406, got %d", resp.StatusCode)
	}
}

func TestHTTPTransport_GetSSE_Heartbeat(t *testing.T) {
	_, ts := newHTTPTestServer(t, HTTPConfig{Heartbeat: 20 * time.Millisecond})
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	readEvent(t, br) // opening comment
	if hb := readEvent(t, br); !strings.HasPrefix(hb, ":") {
		t.Fatalf("expected a heartbeat comment, got %q", hb)
	}
}

func TestHTTPTransport_GetSSE_ClientDisconnectReleasesSubscriber(t *testing.T) {
	srv := NewServer(ServerConfig{})
	h, _ := NewHTTPHandler(srv, HTTPConfig{Heartbeat: time.Hour})
	ts := httptest.NewServer(h)
	defer ts.Close()
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readEvent(t, bufio.NewReader(resp.Body))
	if n := h.subscriberCount(); n != 1 {
		t.Fatalf("want 1 subscriber, got %d", n)
	}
	cancel()
	resp.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for h.subscriberCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber leaked after client disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHTTPTransport_CloseEndsOpenStreams(t *testing.T) {
	srv := NewServer(ServerConfig{})
	h, _ := NewHTTPHandler(srv, HTTPConfig{Heartbeat: time.Hour})
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	readEvent(t, br)

	h.Close()
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, br); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close must end open SSE streams so graceful shutdown does not hang")
	}
}

func TestHTTPTransport_SlowSubscriberDoesNotBlockNotifications(t *testing.T) {
	srv := NewServer(ServerConfig{})
	h, _ := NewHTTPHandler(srv, HTTPConfig{Heartbeat: time.Hour})
	defer h.Close()
	ch, unsub := h.subscribe()
	defer unsub()
	_ = ch // never drained

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			_ = srv.NotifyToolsListChanged()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a stalled subscriber blocked the server")
	}
}

func TestHTTPTransport_ConcurrentPosts(t *testing.T) {
	_, ts := newHTTPTestServer(t, HTTPConfig{})
	var wg sync.WaitGroup
	errs := make(chan string, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(listReq))
			if err != nil {
				errs <- err.Error()
				return
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 || !strings.Contains(string(b), `"id":7`) {
				errs <- string(b)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("concurrent request failed: %s", e)
	}
}

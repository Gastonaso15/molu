package mcp

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultHTTPPath      = "/mcp"
	defaultHeartbeat     = 15 * time.Second
	defaultMaxBodyBytes  = 4 << 20
	subscriberBufferSize = 16
)

// HTTPConfig configures the streamable-http transport.
type HTTPConfig struct {
	// Path is the single MCP endpoint. Default "/mcp".
	Path string
	// Auth is "none" (default) or "bearer". "mtls" is accepted by the
	// configuration loader but not implemented here, so it is rejected.
	Auth string
	// BearerToken is the expected token when Auth is "bearer".
	BearerToken string
	// Heartbeat is the interval of the keep-alive comment on GET streams.
	// Default 15s.
	Heartbeat time.Duration
	// MaxBodyBytes caps a POST body. Default 4 MiB.
	MaxBodyBytes int64
}

// HTTPHandler serves a Server over streamable HTTP:
//
//	POST <path>  one JSON-RPC message; the reply is application/json, or a
//	             single SSE event when the client accepts text/event-stream.
//	             A notification (no id) gets 202 and no body.
//	GET  <path>  a persistent SSE stream for server-initiated messages
//	             (notifications/tools/list_changed), with a heartbeat.
//
// Routing and error taxonomy are the ones of Server.HandleMessage, so a typed
// error looks the same over HTTP as over stdio. The handler is stateless: it
// keeps no session between requests.
type HTTPHandler struct {
	srv *Server
	cfg HTTPConfig

	mu   sync.Mutex
	subs map[chan []byte]struct{}

	done      chan struct{}
	closeOnce sync.Once
}

// NewHTTPHandler builds the handler and routes the server's notifications to
// the open GET streams (it takes over Server.SetOutput).
func NewHTTPHandler(srv *Server, cfg HTTPConfig) (*HTTPHandler, error) {
	if cfg.Path == "" {
		cfg.Path = defaultHTTPPath
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = defaultHeartbeat
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	switch cfg.Auth {
	case "", "none":
		cfg.Auth = "none"
	case "bearer":
		if cfg.BearerToken == "" {
			return nil, errors.New("streamable-http: bearer auth needs a token")
		}
	default:
		return nil, fmt.Errorf("streamable-http: auth mode %q is not supported (use none or bearer)", cfg.Auth)
	}

	h := &HTTPHandler{srv: srv, cfg: cfg, subs: map[chan []byte]struct{}{}, done: make(chan struct{})}
	srv.SetOutput(broadcaster{h})
	return h, nil
}

// Close ends every open GET stream. Call it before http.Server.Shutdown, which
// otherwise waits for those long-lived requests until its timeout.
func (h *HTTPHandler) Close() {
	h.closeOnce.Do(func() { close(h.done) })
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != h.cfg.Path {
		http.NotFound(w, r)
		return
	}
	if !h.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="molu"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodPost:
		h.handlePost(w, r)
	case http.MethodGet:
		h.handleStream(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HTTPHandler) authorized(r *http.Request) bool {
	if h.cfg.Auth != "bearer" {
		return true
	}
	const prefix = "Bearer "
	v := r.Header.Get("Authorization")
	if !strings.HasPrefix(v, prefix) {
		return false
	}
	// Hash first so the comparison time does not depend on the token length.
	got := sha256.Sum256([]byte(v[len(prefix):]))
	want := sha256.Sum256([]byte(h.cfg.BearerToken))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

func acceptsEventStream(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func (h *HTTPHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes))
	if err != nil {
		http.Error(w, "request body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}

	var out bytes.Buffer
	if err := h.srv.HandleMessage(r.Context(), body, &out); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	reply := bytes.TrimSpace(out.Bytes())
	if len(reply) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if acceptsEventStream(r) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprintf(w, "data: %s\n\n", reply)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(reply)
}

func (h *HTTPHandler) handleStream(w http.ResponseWriter, r *http.Request) {
	if !acceptsEventStream(r) {
		http.Error(w, "this endpoint streams text/event-stream", http.StatusNotAcceptable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch, unsubscribe := h.subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// An opening comment makes the stream observable before any event.
	fmt.Fprint(w, ": molu stream open\n\n")
	flusher.Flush()

	ticker := time.NewTicker(h.cfg.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.done:
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (h *HTTPHandler) subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, subscriberBufferSize)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *HTTPHandler) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// publish fans a message out to every stream. A subscriber whose buffer is
// full drops the message: a stalled client must never block the server, and a
// missed list_changed is recovered by the next tools/list.
func (h *HTTPHandler) publish(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

// broadcaster is the io.Writer the Server writes its notifications to.
type broadcaster struct{ h *HTTPHandler }

func (b broadcaster) Write(p []byte) (int, error) {
	for _, line := range bytes.Split(p, []byte("\n")) {
		if line = bytes.TrimSpace(line); len(line) > 0 {
			b.h.publish(append([]byte(nil), line...))
		}
	}
	return len(p), nil
}

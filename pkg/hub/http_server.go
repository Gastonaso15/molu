package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

// HubHTTPServer exposes the Hub catalog and discovery service over HTTP REST.
type HubHTTPServer struct {
	registry   *Registry
	discovery  *Discovery
	authorizer *PublisherAuthorizer
	logger     telemetry.Logger
	metrics    telemetry.Metrics
	mux        *http.ServeMux
}

// NewHubHTTPServer creates a new HTTP server wrapping the catalog and discovery.
func NewHubHTTPServer(registry *Registry, discovery *Discovery) *HubHTTPServer {
	s := &HubHTTPServer{
		registry:  registry,
		discovery: discovery,
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

// SetAuthorizer attaches a namespace-scoped publisher authorizer (§2.6).
func (s *HubHTTPServer) SetAuthorizer(auth *PublisherAuthorizer) {
	s.authorizer = auth
}

// SetTelemetry attaches structured logger and Prometheus metrics to the Hub server.
func (s *HubHTTPServer) SetTelemetry(logger telemetry.Logger, metrics telemetry.Metrics) {
	s.logger = logger
	s.metrics = metrics
}

// Handler returns the underlying http.Handler wrapped with correlation and telemetry.
func (s *HubHTTPServer) Handler() http.Handler {
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" && s.metrics != nil {
			s.metrics.Handler().ServeHTTP(w, r)
			return
		}
		start := time.Now()
		// Echo X-Request-ID even when no telemetry logger is configured.
		if reqID := r.Header.Get("X-Request-ID"); reqID != "" && w.Header().Get("X-Request-ID") == "" {
			w.Header().Set("X-Request-ID", reqID)
		}
		s.mux.ServeHTTP(w, r)
		if s.metrics != nil {
			s.metrics.IncRequests(telemetry.ComponentHubRegistry, r.URL.Path, "", "OK")
			s.metrics.ObserveLatency(telemetry.ComponentHubRegistry, r.URL.Path, float64(time.Since(start).Milliseconds()))
		}
	})
	if s.logger != nil {
		h = telemetry.NewMiddleware(s.logger).Wrap(h)
	}
	return h
}

func (s *HubHTTPServer) routes() {
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/api/v1/registry/register", s.handleRegister)
	s.mux.HandleFunc("/api/v1/registry/unregister", s.handleUnregister)
	s.mux.HandleFunc("/api/v1/registry/list", s.handleList)
	s.mux.HandleFunc("/api/v1/discovery/tools", s.handleDiscoveryTools)
}

func (s *HubHTTPServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *HubHTTPServer) authorizePublisher(w http.ResponseWriter, r *http.Request, namespace, toolName string) bool {
	if !s.authorizer.Enabled() {
		return true
	}
	token := ExtractBearerToken(r)
	err := s.authorizer.Authorize(token, namespace)
	if err == nil {
		return true
	}
	if s.logger != nil {
		s.logger.WithContext(r.Context()).Security().Warn().
			Str("decision", telemetry.DecisionDeny).
			Str("namespace", namespace).
			Str("tool", toolName).
			Str("reason", err.Error()).
			Msg("publisher authorization denied")
	}
	if errors.Is(err, ErrUnauthorized) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return false
	}
	writeJSON(w, http.StatusForbidden, taxonomy.NewNotOffered("", toolName))
	return false
}

func (s *HubHTTPServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reg ToolRegistration
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		errResp := taxonomy.NewContractViolation("", "registry.register", []string{"body"})
		writeJSON(w, http.StatusBadRequest, errResp)
		return
	}

	ns := reg.Namespace
	if ns == "" {
		if prefix, _, ok := strings.Cut(reg.Name, "."); ok {
			ns = prefix
		}
	}
	if !s.authorizePublisher(w, r, ns, reg.Name) {
		return
	}

	if err := s.registry.RegisterExternal(reg); err != nil {
		writeTypedError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "name": reg.Name})
}

func (s *HubHTTPServer) handleUnregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var p struct {
		PublisherID string `json:"publisher_id"`
		Name        string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		errResp := taxonomy.NewContractViolation("", "registry.unregister", []string{"body"})
		writeJSON(w, http.StatusBadRequest, errResp)
		return
	}

	ns, _, _ := strings.Cut(p.Name, ".")
	if !s.authorizePublisher(w, r, ns, p.Name) {
		return
	}

	if err := s.registry.Unregister(p.PublisherID, p.Name); err != nil {
		writeTypedError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *HubHTTPServer) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var p struct {
		PublisherID string `json:"publisher_id,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&p)

	tools := s.registry.List(p.PublisherID)
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (s *HubHTTPServer) handleDiscoveryTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var actx rpi.AdmitContext
	if err := json.NewDecoder(r.Body).Decode(&actx); err != nil {
		errResp := taxonomy.NewContractViolation("", "tools/list", []string{"body"})
		writeJSON(w, http.StatusBadRequest, errResp)
		return
	}

	if s.discovery == nil {
		writeJSON(w, http.StatusOK, map[string]any{"tools": []rpi.ScoredTool{}})
		return
	}

	scored, err := s.discovery.ListTools(r.Context(), actx)
	if err != nil {
		var te *taxonomy.TypedError
		if errors.As(err, &te) {
			if strings.Contains(te.Code, string(taxonomy.CodeEmpty)) {
				writeJSON(w, http.StatusOK, map[string]any{
					"tools": []rpi.ScoredTool{},
					"code":  "EMPTY",
				})
				return
			}
			if strings.Contains(te.Code, string(taxonomy.CodeContractViolation)) {
				writeJSON(w, http.StatusBadRequest, te)
				return
			}
			writeJSON(w, http.StatusInternalServerError, te)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"tools": scored})
}

func writeTypedError(w http.ResponseWriter, err error) {
	var te *taxonomy.TypedError
	if errors.As(err, &te) {
		status := http.StatusInternalServerError
		if strings.Contains(te.Code, string(taxonomy.CodeContractViolation)) {
			status = http.StatusBadRequest
		} else if strings.Contains(te.Code, string(taxonomy.CodeNotFound)) {
			status = http.StatusNotFound
		} else if strings.Contains(te.Code, string(taxonomy.CodeNotOffered)) {
			status = http.StatusForbidden
		}
		writeJSON(w, status, te)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}


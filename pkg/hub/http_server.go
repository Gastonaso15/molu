package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// HubHTTPServer exposes the Hub catalog and discovery service over HTTP REST.
type HubHTTPServer struct {
	registry  *Registry
	discovery *Discovery
	mux       *http.ServeMux
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

// Handler returns the underlying http.Handler.
func (s *HubHTTPServer) Handler() http.Handler {
	return s.mux
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

	if err := s.registry.Register(reg); err != nil {
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


package issues

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

const maxBodyBytes = 1 << 20 // 1 MB

// Server exposes the IssueService over HTTP for the Molu Execution Bridge.
type Server struct {
	svc   *IssueService
	token string
}

// NewServer creates a new Server instance.
func NewServer(svc *IssueService, token string) *Server {
	return &Server{
		svc:   svc,
		token: token,
	}
}

// Handler configures routes and returns the top-level HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.Handle("/api/v1/issues/create", s.guard(http.HandlerFunc(s.create)))
	mux.Handle("/api/v1/issues/get", s.guard(http.HandlerFunc(s.get)))
	mux.Handle("/api/v1/issues/advance", s.guard(http.HandlerFunc(s.advance)))

	return withRequestID(mux)
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || s.token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "missing or invalid bearer token"})
			return
		}

		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "method not allowed"})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var in CreateIssueRequest
	if !decode(w, r, &in) {
		return
	}
	issue, err := s.svc.CreateIssue(r.Context(), in)
	respond(w, issue, err)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	var in GetIssueRequest
	if !decode(w, r, &in) {
		return
	}
	issue, err := s.svc.GetIssue(r.Context(), in)
	respond(w, issue, err)
}

func (s *Server) advance(w http.ResponseWriter, r *http.Request) {
	var in AdvanceIssueRequest
	if !decode(w, r, &in) {
		return
	}
	res, err := s.svc.AdvanceIssue(r.Context(), in)
	respond(w, res, err)
}

func respond(w http.ResponseWriter, ok any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, ok)
		return
	}

	var te *taxonomy.TypedError
	if errors.As(err, &te) {
		switch {
		case strings.HasSuffix(te.Code, string(taxonomy.CodeNotFound)):
			// Bare 404 without a XOLU- code so the Execution Bridge maps it to NOT_FOUND
			writeJSON(w, http.StatusNotFound, map[string]any{"message": te.Message})
			return

		case strings.HasSuffix(te.Code, string(taxonomy.CodeInvalidTransition)):
			// 409 Conflict with the exact verbatim XOLU- code and diagnostic message
			writeJSON(w, http.StatusConflict, map[string]any{
				"code":    te.Code,
				"message": te.Message,
				"details": te.Details,
			})
			return

		case strings.HasSuffix(te.Code, string(taxonomy.CodeSubstrateUnavailable)):
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"code":    "XOLU-SUBSTRATE_UNAVAILABLE",
				"message": te.Message,
				"details": te.Details,
			})
			return

		case strings.HasSuffix(te.Code, string(taxonomy.CodeContractViolation)):
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"message":        te.Message,
				"invalid_fields": te.Details["fields"],
			})
			return
		}
	}

	writeJSON(w, http.StatusInternalServerError, map[string]any{"message": err.Error()})
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid JSON body: " + err.Error()})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get("X-Request-ID"); id != "" {
			w.Header().Set("X-Request-ID", id)
		}
		next.ServeHTTP(w, r)
	})
}

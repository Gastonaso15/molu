package issues_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/publisher/issues"
)

type simXoluIssues struct {
	mu           sync.Mutex
	wrapperKey   string // "definitions", "items", "data", "defs", "results", or "" for bare array
	defs         []map[string]any
	defPostCalls int
	nextDefID    int64
	nextMachID   int64
	nextEntityID int64
}

func (s *simXoluIssues) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/fsm/def", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Method == http.MethodGet {
			if s.wrapperKey == "" {
				_ = json.NewEncoder(w).Encode(s.defs)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{s.wrapperKey: s.defs})
			return
		}
		if r.Method == http.MethodPost {
			s.defPostCalls++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.nextDefID++
			id := s.nextDefID
			s.defs = append(s.defs, map[string]any{
				"id":   id,
				"name": body["name"],
			})
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "name": body["name"]})
		}
	})
	mux.HandleFunc("/api/v2/fsm/machine", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.nextMachID++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": s.nextMachID, "state": "reported"})
	})
	mux.HandleFunc("/api/v1/issues", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.nextEntityID++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": s.nextEntityID})
	})
	return mux
}

func TestHTTPSubstrate_EnsureMachineDefinition_ReusesAcrossWrappers(t *testing.T) {
	wrappers := []string{"definitions", "items", "data", "defs", "results", ""}
	for _, wk := range wrappers {
		name := wk
		if name == "" {
			name = "bare_array"
		}
		t.Run(name, func(t *testing.T) {
			sim := &simXoluIssues{wrapperKey: wk}
			srv := httptest.NewServer(sim.handler())
			defer srv.Close()

			sub := issues.NewHTTPSubstrate(srv.URL, "", 5*time.Second)
			svc := issues.NewIssueService(sub)

			for i := 1; i <= 2; i++ {
				if _, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
					Tenant: "tenant-alpha",
					Title:  "Issue reuse check",
				}); err != nil {
					t.Fatalf("CreateIssue #%d failed: %v", i, err)
				}
			}

			sim.mu.Lock()
			defCount := len(sim.defs)
			postCalls := sim.defPostCalls
			sim.mu.Unlock()

			if defCount != 1 || postCalls != 1 {
				t.Fatalf("wrapper %q: expected 1 FSM definition and 1 POST, got defs=%d postCalls=%d", name, defCount, postCalls)
			}
		})
	}
}

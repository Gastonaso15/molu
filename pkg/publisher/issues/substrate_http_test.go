package issues_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/publisher/issues"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
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

func TestHTTPSubstrate_WalkMachine_InvalidTransitionShapes(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		body         string
		wantXoluCode string
		wantMsg      string
		wantDetail   string
		wantState    string
	}{
		{
			name:         "fsm004_string_error_400",
			status:       http.StatusBadRequest,
			body:         `{"error":"XOLU-FSM004","message":"no transition for input \"close\" from state \"in_progress\"","detail":"allowed: hold, resolve, reject","state":"in_progress","available_inputs":["hold","resolve","reject"]}`,
			wantXoluCode: "XOLU-FSM004",
			wantMsg:      `no transition for input "close" from state "in_progress"`,
			wantDetail:   "allowed: hold, resolve, reject",
			wantState:    "in_progress",
		},
		{
			name:         "fsm003_nested_object_422",
			status:       http.StatusUnprocessableEntity,
			body:         `{"error":{"code":"XOLU-FSM003","message":"guard rejected","detail":"motivo required"}}`,
			wantXoluCode: "XOLU-FSM003",
			wantMsg:      "guard rejected",
			wantDetail:   "motivo required",
		},
		{
			name:         "invalid_transition_409",
			status:       http.StatusConflict,
			body:         `{"code":"XOLU-INVALID_TRANSITION","message":"cannot transition from reported via close"}`,
			wantXoluCode: "XOLU-INVALID_TRANSITION",
			wantMsg:      "cannot transition from reported via close",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v2/fsm/def" && r.Method == http.MethodGet:
					_ = json.NewEncoder(w).Encode(map[string]any{"definitions": []any{map[string]any{"id": int64(1), "name": issues.DefinitionName}}})
				case r.URL.Path == "/api/v2/fsm/machine" && r.Method == http.MethodPost:
					_ = json.NewEncoder(w).Encode(map[string]any{"id": int64(77), "state": "in_progress"})
				case r.URL.Path == "/api/v1/issues" && r.Method == http.MethodPost:
					_ = json.NewEncoder(w).Encode(map[string]any{"id": int64(10)})
				case r.URL.Path == "/api/v1/issues/10" && r.Method == http.MethodGet:
					_ = json.NewEncoder(w).Encode(map[string]any{"id": int64(10), "title": "t", "state": "in_progress", "machine_id": int64(77)})
				case r.URL.Path == "/api/v2/fsm/machine/77/walk" && r.Method == http.MethodPost:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			sub := issues.NewHTTPSubstrate(srv.URL, "", 5*time.Second)
			svc := issues.NewIssueService(sub)

			_, err := svc.AdvanceIssue(context.Background(), issues.AdvanceIssueRequest{
				Tenant:     "dev",
				IssueID:    10,
				Transition: "close",
			})
			if err == nil {
				t.Fatal("expected TypedError, got nil")
			}
			var te *taxonomy.TypedError
			if !errors.As(err, &te) {
				t.Fatalf("expected *taxonomy.TypedError, got %T: %v", err, err)
			}
			if te.Code != taxonomy.PrefixXolu+string(taxonomy.CodeInvalidTransition) {
				t.Fatalf("expected XOLU-INVALID_TRANSITION, got %q (msg=%q)", te.Code, te.Message)
			}
			if te.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", te.Message, tc.wantMsg)
			}
			if gotCode, _ := te.Details["xolu_code"].(string); gotCode != tc.wantXoluCode {
				t.Errorf("details.xolu_code = %q, want %q (details=%v)", gotCode, tc.wantXoluCode, te.Details)
			}
			if tc.wantDetail != "" {
				if gotDetail, _ := te.Details["detail"].(string); gotDetail != tc.wantDetail {
					t.Errorf("details.detail = %q, want %q", gotDetail, tc.wantDetail)
				}
			}
			if tc.wantState != "" {
				if gotState, _ := te.Details["state"].(string); gotState != tc.wantState {
					t.Errorf("details.state = %q, want %q", gotState, tc.wantState)
				}
			}
		})
	}
}

func TestHTTPSubstrate_WalkMachine_404And5xx(t *testing.T) {
	t.Run("404_machine_is_not_found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"XOLU-FSM002","message":"machine not found"}`))
		}))
		defer srv.Close()

		sub := issues.NewHTTPSubstrate(srv.URL, "", 5*time.Second)
		_, err := sub.WalkMachine(context.Background(), "dev", 999, "start", nil)
		var te *taxonomy.TypedError
		if !errors.As(err, &te) || te.Code != taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeNotFound) {
			t.Fatalf("expected NOT_FOUND, got %v", err)
		}
	})

	t.Run("500_is_substrate_unavailable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`internal error`))
		}))
		defer srv.Close()

		sub := issues.NewHTTPSubstrate(srv.URL, "", 5*time.Second)
		_, err := sub.WalkMachine(context.Background(), "dev", 1, "start", nil)
		var te *taxonomy.TypedError
		if !errors.As(err, &te) || te.Code != taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeSubstrateUnavailable) {
			t.Fatalf("expected SUBSTRATE_UNAVAILABLE, got %v", err)
		}
	})
}

func TestHTTPSubstrate_NonFSM4xx_NotSubstrateUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v2/fsm/def" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"definitions": []any{map[string]any{"id": int64(1), "name": issues.DefinitionName}}})
		case r.URL.Path == "/api/v2/fsm/machine" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": int64(77), "state": "reported"})
		case r.URL.Path == "/api/v1/issues" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"XOLU-VL001","message":"invalid payload"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	sub := issues.NewHTTPSubstrate(srv.URL, "", 5*time.Second)
	svc := issues.NewIssueService(sub)

	_, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
		Tenant: "dev",
		Title:  "Bad substrate payload",
	})
	if err == nil {
		t.Fatal("expected error on 400 from xolu")
	}
	var te *taxonomy.TypedError
	if errors.As(err, &te) && te.Code == taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeSubstrateUnavailable) {
		t.Fatalf("unexpected SUBSTRATE_UNAVAILABLE for 400 client error: %v", err)
	}
}

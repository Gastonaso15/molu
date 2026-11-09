package orders

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPSubstrate_EnsureDefinition_ReusesExisting(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		_, _ = w.Write([]byte(`{"items":[{"id":7,"name":"Other"},{"id":42,"name":"WorkOrderLifecycle"}]}`))
	}))
	defer srv.Close()

	id, err := NewHTTPSubstrate(srv.URL, "", 0).EnsureDefinition(context.Background(), DefinitionName, LifecycleDefinition())
	if err != nil || id != "42" || posts != 0 {
		t.Fatalf("id=%q err=%v posts=%d", id, err, posts)
	}
}

func TestHTTPSubstrate_EnsureDefinition_CreatesWhenMissing(t *testing.T) {
	var created map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&created)
		_, _ = w.Write([]byte(`{"id":1234567890123,"name":"WorkOrderLifecycle"}`))
	}))
	defer srv.Close()

	id, err := NewHTTPSubstrate(srv.URL, "", 0).EnsureDefinition(context.Background(), DefinitionName, LifecycleDefinition())
	if err != nil || id != "1234567890123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if created["name"] != DefinitionName || created["initial"] != "Open" {
		t.Fatalf("definition not sent: %v", created)
	}
}

func TestHTTPSubstrate_CreateMachineAndWalk(t *testing.T) {
	var entityBody, machineBody, walkBody map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/v1/work_order":
			_ = json.NewDecoder(r.Body).Decode(&entityBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":55,"message":"created"}`))
		case "/api/v2/fsm/machine":
			_ = json.NewDecoder(r.Body).Decode(&machineBody)
			_, _ = w.Write([]byte(`{"id":1001,"state":"Open","ref":"work_order:55"}`))
		case "/api/v2/fsm/machine/1001/walk":
			_ = json.NewDecoder(r.Body).Decode(&walkBody)
			_, _ = w.Write([]byte(`{"previous":"Open","current":"InProgress","terminal":false,"outputs":["x"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	sub := NewHTTPSubstrate(srv.URL, "tok", 0)

	m, err := sub.CreateMachine(context.Background(), "42", EntityType, map[string]any{"title": "t"})
	if err != nil || m.ID != "1001" || m.State != "Open" || m.Ref != "work_order:55" {
		t.Fatalf("machine=%+v err=%v", m, err)
	}
	if entityBody["title"] != "t" {
		t.Errorf("entity body = %v", entityBody)
	}
	if machineBody["definition"] != float64(42) || machineBody["ref"] != "work_order:55" { // number, bound by ref
		t.Errorf("machine body = %#v", machineBody)
	}
	if auth != "Bearer tok" {
		t.Errorf("auth = %q", auth)
	}

	wr, err := sub.Walk(context.Background(), "1001", "start", map[string]any{"a": 1})
	if err != nil || wr.Previous != "Open" || wr.Current != "InProgress" || len(wr.Outputs) != 1 {
		t.Fatalf("walk=%+v err=%v", wr, err)
	}
	if walkBody["input"] != "start" {
		t.Errorf("walk body = %v", walkBody)
	}
}

func TestHTTPSubstrate_CreateMachineFailureRemovesEntity(t *testing.T) {
	var deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/work_order" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":77}`))
		case r.URL.Path == "/api/v2/fsm/machine":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error":"XOLU-FSM006","message":"bad definition"}`))
		case r.Method == http.MethodDelete:
			deleted = r.URL.Path
		}
	}))
	defer srv.Close()

	_, err := NewHTTPSubstrate(srv.URL, "", 0).CreateMachine(context.Background(), "42", EntityType, map[string]any{"title": "t"})
	var xe *XoluError
	if !errors.As(err, &xe) || xe.Code != "XOLU-FSM006" {
		t.Fatalf("want the machine error, got %v", err)
	}
	if deleted != "/api/v1/work_order/77" {
		t.Fatalf("orphan entity not removed, delete path = %q", deleted)
	}
}

func TestHTTPSubstrate_FSMErrorEnvelopeIsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"XOLU-FSM004","message":"transition guard rejected","detail":"guard: x evaluated false","state":"InProgress","available_inputs":["cancel"]}`))
	}))
	defer srv.Close()

	_, err := NewHTTPSubstrate(srv.URL, "", 0).Walk(context.Background(), "1", "complete", nil)
	var xe *XoluError
	if !errors.As(err, &xe) {
		t.Fatalf("want XoluError, got %v", err)
	}
	if xe.Status != 422 || xe.Code != "XOLU-FSM004" || xe.Message != "transition guard rejected" ||
		xe.Detail != "guard: x evaluated false" || xe.State != "InProgress" || len(xe.AvailableInputs) != 1 {
		t.Fatalf("xe = %+v", xe)
	}
}

func TestHTTPSubstrate_NestedAndFlatErrorEnvelopes(t *testing.T) {
	for name, body := range map[string]string{
		"nested": `{"error":{"code":"XOLU-ST001","message":"entity not found"}}`,
		"flat":   `{"code":"XOLU-ST001","message":"entity not found"}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			_, err := NewHTTPSubstrate(srv.URL, "", 0).GetEntity(context.Background(), EntityType, "9")
			var xe *XoluError
			if !errors.As(err, &xe) || xe.Code != "XOLU-ST001" || xe.Message != "entity not found" || xe.Status != 404 {
				t.Fatalf("xe = %+v (%v)", xe, err)
			}
		})
	}
}

func TestHTTPSubstrate_UnreachableIsUnavailableThroughService(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens any more

	_, err := NewService(NewHTTPSubstrate(url, "", 0)).GetOrder(context.Background(), "1")
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("want UnavailableError, got %v", err)
	}
}

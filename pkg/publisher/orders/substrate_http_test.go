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
		_, _ = w.Write([]byte(`{"items":[{"id":7,"name":"Other"},{"id":42,"name":"seam_work_order_lifecycle"}]}`))
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
		_, _ = w.Write([]byte(`{"id":1234567890123,"name":"seam_work_order_lifecycle"}`))
	}))
	defer srv.Close()

	id, err := NewHTTPSubstrate(srv.URL, "", 0).EnsureDefinition(context.Background(), DefinitionName, LifecycleDefinition())
	if err != nil || id != "1234567890123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if created["name"] != DefinitionName || created["initial"] != "draft" {
		t.Fatalf("definition not sent: %v", created)
	}
}

func TestHTTPSubstrate_MachineEntityAndWalk(t *testing.T) {
	var entityBody, machineBody, walkBody, patchBody map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/api/v1/work_orders" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&entityBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":55,"message":"created"}`))
		case r.URL.Path == "/api/v1/work_orders/55" && r.Method == http.MethodPatch:
			_ = json.NewDecoder(r.Body).Decode(&patchBody)
			_, _ = w.Write([]byte(`{"message":"updated"}`))
		case r.URL.Path == "/api/v1/work_orders/55" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"title":"t","state":"draft","machine_id":1001}`))
		case r.URL.Path == "/api/v2/fsm/machine":
			_ = json.NewDecoder(r.Body).Decode(&machineBody)
			_, _ = w.Write([]byte(`{"id":1001,"state":"draft"}`))
		case r.URL.Path == "/api/v2/fsm/machine/1001/walk":
			_ = json.NewDecoder(r.Body).Decode(&walkBody)
			_, _ = w.Write([]byte(`{"previous":"draft","current":"in_progress","terminal":false,"outputs":["x"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	sub := NewHTTPSubstrate(srv.URL, "tok", 0)
	ctx := context.Background()

	m, err := sub.CreateMachine(ctx, "42")
	if err != nil || m.ID != "1001" || m.State != "draft" {
		t.Fatalf("machine=%+v err=%v", m, err)
	}
	if machineBody["definition"] != float64(42) || machineBody["ref"] != nil { // number, not bound by ref
		t.Errorf("machine body = %#v", machineBody)
	}
	if auth != "Bearer tok" {
		t.Errorf("auth = %q", auth)
	}

	id, err := sub.CreateEntity(ctx, EntityType, map[string]any{"title": "t", "machine_id": int64(1001)})
	if err != nil || id != "55" || entityBody["title"] != "t" || entityBody["machine_id"] != float64(1001) {
		t.Fatalf("id=%q err=%v body=%v", id, err, entityBody)
	}

	doc, err := sub.GetEntity(ctx, EntityType, "55")
	if err != nil || doc["state"] != "draft" || idString(doc["machine_id"]) != "1001" {
		t.Fatalf("doc=%v err=%v", doc, err)
	}

	if err := sub.PatchEntity(ctx, EntityType, "55", map[string]any{"state": "in_progress"}); err != nil || patchBody["state"] != "in_progress" {
		t.Fatalf("patch err=%v body=%v", err, patchBody)
	}

	wr, err := sub.Walk(ctx, "1001", "start", map[string]any{"a": 1})
	if err != nil || wr.Previous != "draft" || wr.Current != "in_progress" || len(wr.Outputs) != 1 {
		t.Fatalf("walk=%+v err=%v", wr, err)
	}
	if walkBody["input"] != "start" {
		t.Errorf("walk body = %v", walkBody)
	}
}

func TestHTTPSubstrate_ListEntitiesUsesOQL(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/oql/query" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		query = body["query"]
		_, _ = w.Write([]byte(`{"status":"ok","data":[{"id":3,"name":"A","version":2,"latest":true,"workflow_states":[{"name":"Open"},{"name":"Done"}]}]}`))
	}))
	defer srv.Close()

	svc := NewService(NewHTTPSubstrate(srv.URL, "", 0))
	got, err := svc.ListTemplates(context.Background(), ListTemplatesInput{})
	if err != nil || len(got.Templates) != 1 {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if query != "SELECT TOP 100 * FROM work_order_templates ORDER BY id DESC" {
		t.Errorf("query = %q", query)
	}
	if tpl := got.Templates[0]; tpl.ID != "3" || tpl.Version != 2 || len(tpl.WorkflowStates) != 2 {
		t.Errorf("template = %+v", tpl)
	}
}

func TestHTTPSubstrate_FSMErrorEnvelopeIsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"XOLU-FSM004","message":"transition guard rejected","detail":"guard: x evaluated false","state":"in_progress","available_inputs":["cancel"]}`))
	}))
	defer srv.Close()

	_, err := NewHTTPSubstrate(srv.URL, "", 0).Walk(context.Background(), "1", "complete", nil)
	var xe *XoluError
	if !errors.As(err, &xe) {
		t.Fatalf("want XoluError, got %v", err)
	}
	if xe.Status != 422 || xe.Code != "XOLU-FSM004" || xe.Message != "transition guard rejected" ||
		xe.Detail != "guard: x evaluated false" || xe.State != "in_progress" || len(xe.AvailableInputs) != 1 {
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

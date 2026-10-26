package xolu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// describeFake serves the xolu routes that describe reads, with the shapes of
// xolu v0.30.44, and records every path it is asked for.
type describeFake struct {
	mu      sync.Mutex
	paths   []string
	v2Off   bool // /api/v2/* answers 404, as when XOLU_API_V2_ENABLED is unset
	failAll bool // every route answers 500

	suggestionStatus int // when set, schema-suggestion answers with this status
}

func (f *describeFake) called(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.paths {
		if p == path {
			return true
		}
	}
	return false
}

func (f *describeFake) server(t *testing.T) *HTTPXoluClient {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		notFound := func() {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"XOLU-ST008","message":"No schema found","status":404}}`))
		}
		if f.failAll {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if f.v2Off && strings.HasPrefix(r.URL.Path, "/api/v2/") {
			notFound()
			return
		}
		switch r.URL.Path {
		case "/api/v1/entities":
			_ = json.NewEncoder(w).Encode(map[string]any{"count": 2, "entities": []any{
				map[string]any{"entity_type": "assets", "count": 16, "has_schema": true, "adapted": false},
				map[string]any{"entity_type": "regions", "count": 5, "has_schema": false, "adapted": false},
			}})
		case "/api/v1/schema/assets":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":       "object",
				"required":   []any{"name"},
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			})
		case "/api/v1/entity/regions/schema-suggestion":
			if f.suggestionStatus != 0 {
				w.WriteHeader(f.suggestionStatus)
				_, _ = w.Write([]byte(`{"error":{"code":"XOLU-ST010","message":"no data to infer a schema from","status":400}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entity_type": "regions", "sampled_rows": 5, "total_rows": 5,
				"suggested_schema": map[string]any{
					"type":       "object",
					"required":   []any{"description"},
					"properties": map[string]any{"description": map[string]any{"type": "string"}},
				},
				"field_analysis": []any{
					map[string]any{"field": "description", "inferred_type": "string", "coverage": 1, "confidence": "high"},
				},
			})
		case "/api/v2/fsm/def":
			_ = json.NewEncoder(w).Encode(map[string]any{"definitions": []any{
				map[string]any{"id": 1, "name": "Vehicle lifecycle", "created_at": "2026-10-26T12:52:31Z"},
				map[string]any{"id": 2, "name": "Heavy machinery lifecycle", "created_at": "2026-10-26T12:52:31Z"},
			}})
		case "/api/v2/fsm/def/1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "created_at": "2026-10-26T12:52:31Z",
				"spec": map[string]any{"name": "Vehicle lifecycle", "initial": "available"}})
		case "/api/v2/fsm/def/2":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 2, "created_at": "2026-10-26T12:52:31Z",
				"spec": map[string]any{"name": "Heavy machinery lifecycle", "initial": "operative"}})
		case "/api/v2/gen/seq":
			_ = json.NewEncoder(w).Encode(map[string]any{"count": 1, "sequences": []any{
				map[string]any{"name": "work_order", "next": 7},
			}})
		case "/api/v2/event/def":
			_ = json.NewEncoder(w).Encode(map[string]any{"subscriptions": []any{
				map[string]any{"id": "ev-1", "name": "overheat"},
			}})
		default:
			notFound()
		}
	}))
	t.Cleanup(ts.Close)
	return NewHTTPXoluClient(ts.URL, 2*time.Second)
}

func describe(t *testing.T, c *HTTPXoluClient, args map[string]any) (map[string]any, error) {
	t.Helper()
	return c.Execute(context.Background(), "describe", args)
}

func list(t *testing.T, res map[string]any, key string) []any {
	t.Helper()
	v, ok := res[key].([]any)
	if !ok {
		t.Fatalf("expected %q to be a list, got %T in %v", key, res[key], res)
	}
	return v
}

func typedCode(err error) string {
	var te *taxonomy.TypedError
	if errors.As(err, &te) {
		return te.Code
	}
	return ""
}

func TestDescribe_DefaultScopeIsAll(t *testing.T) {
	f := &describeFake{}
	res, err := describe(t, f.server(t), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, k := range []string{"entities", "machines", "generators", "events"} {
		list(t, res, k)
	}
	if _, ok := res["unavailable"]; ok {
		t.Errorf("nothing is unavailable, got %v", res["unavailable"])
	}
}

func TestDescribe_EntitiesCarrySchemaOnlyWhenRegistered(t *testing.T) {
	f := &describeFake{}
	res, err := describe(t, f.server(t), map[string]any{"scope": "entities"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("scope entities should return only entities, got keys %v", res)
	}
	byName := map[string]map[string]any{}
	for _, e := range list(t, res, "entities") {
		m := e.(map[string]any)
		byName[m["entity_type"].(string)] = m
	}
	if _, ok := byName["assets"]["schema"].(map[string]any); !ok {
		t.Errorf("assets has a schema, expected it inlined, got %v", byName["assets"])
	}
	if _, ok := byName["regions"]["schema"]; ok {
		t.Errorf("regions has no schema, it must not carry one: %v", byName["regions"])
	}
	if byName["regions"]["has_schema"] != false || byName["regions"]["count"] != float64(5) {
		t.Errorf("has_schema and count must be kept, got %v", byName["regions"])
	}
	if f.called("/api/v1/schema/regions") {
		t.Error("the schema of an entity without one must not be requested")
	}
	if f.called("/api/v2/fsm/def") {
		t.Error("scope entities must not touch the v2 routes")
	}
}

func TestDescribe_MachinesIncludeTheFullDefinition(t *testing.T) {
	f := &describeFake{}
	res, err := describe(t, f.server(t), map[string]any{"scope": "machines"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	machines := list(t, res, "machines")
	if len(machines) != 2 {
		t.Fatalf("expected 2 machines, got %v", machines)
	}
	m := machines[0].(map[string]any)
	if m["name"] != "Vehicle lifecycle" {
		t.Errorf("expected the name, got %v", m["name"])
	}
	if spec, _ := m["spec"].(map[string]any); spec["initial"] != "available" {
		t.Errorf("expected the spec of the definition, got %v", m)
	}
}

func TestDescribe_GeneratorsAndEvents(t *testing.T) {
	f := &describeFake{}
	c := f.server(t)
	res, err := describe(t, c, map[string]any{"scope": "generators"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g := list(t, res, "generators"); g[0].(map[string]any)["name"] != "work_order" {
		t.Errorf("unexpected generators %v", g)
	}
	res, err = describe(t, c, map[string]any{"scope": "events"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e := list(t, res, "events"); e[0].(map[string]any)["id"] != "ev-1" {
		t.Errorf("unexpected events %v", e)
	}
}

func TestDescribe_NameFiltersCaseInsensitivelyAndFetchesOnlyMatches(t *testing.T) {
	f := &describeFake{}
	c := f.server(t)

	res, err := describe(t, c, map[string]any{"scope": "entities", "name": "ASSETS"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e := list(t, res, "entities"); len(e) != 1 || e[0].(map[string]any)["entity_type"] != "assets" {
		t.Errorf("expected only assets, got %v", e)
	}

	res, err = describe(t, c, map[string]any{"scope": "machines", "name": "heavy machinery lifecycle"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m := list(t, res, "machines"); len(m) != 1 || m[0].(map[string]any)["id"] != float64(2) {
		t.Errorf("expected only machine 2, got %v", m)
	}
	if f.called("/api/v2/fsm/def/1") {
		t.Error("the definition of a machine that does not match must not be requested")
	}

	res, err = describe(t, c, map[string]any{"scope": "machines", "name": "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m := list(t, res, "machines"); len(m) != 1 || m[0].(map[string]any)["id"] != float64(1) {
		t.Errorf("a machine can also be named by id, got %v", m)
	}
}

func TestDescribe_NameWithScopeAllSearchesEveryGroup(t *testing.T) {
	f := &describeFake{}
	res, err := describe(t, f.server(t), map[string]any{"name": "work_order"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g := list(t, res, "generators"); len(g) != 1 {
		t.Errorf("expected the generator, got %v", g)
	}
	if e := list(t, res, "entities"); len(e) != 0 {
		t.Errorf("no entity is called work_order, got %v", e)
	}
}

func TestDescribe_UnknownNameIsNotFound(t *testing.T) {
	f := &describeFake{}
	_, err := describe(t, f.server(t), map[string]any{"name": "nope"})
	if !strings.Contains(typedCode(err), string(taxonomy.CodeNotFound)) {
		t.Fatalf("expected NOT_FOUND, got %v", err)
	}
}

func TestDescribe_ScopeAllReportsWhatXoluDoesNotOffer(t *testing.T) {
	f := &describeFake{v2Off: true}
	res, err := describe(t, f.server(t), map[string]any{"scope": "all"})
	if err != nil {
		t.Fatalf("a missing v2 must not fail describe: %v", err)
	}
	if e := list(t, res, "entities"); len(e) != 2 {
		t.Errorf("entities must still be described, got %v", e)
	}
	un, ok := res["unavailable"].(map[string]any)
	if !ok {
		t.Fatalf("expected an unavailable section, got %v", res)
	}
	for _, k := range []string{"machines", "generators", "events"} {
		if _, ok := un[k]; !ok {
			t.Errorf("expected %q in unavailable, got %v", k, un)
		}
		if _, ok := res[k]; ok {
			t.Errorf("%q must not be present when unavailable", k)
		}
	}
}

func TestDescribe_ExplicitScopeFailsWhenXoluDoesNotOfferIt(t *testing.T) {
	f := &describeFake{v2Off: true}
	if _, err := describe(t, f.server(t), map[string]any{"scope": "machines"}); err == nil {
		t.Fatal("asking explicitly for machines without v2 must fail")
	}
}

func TestDescribe_UnknownScopeIsContractViolation(t *testing.T) {
	f := &describeFake{}
	_, err := describe(t, f.server(t), map[string]any{"scope": "everything"})
	if !strings.Contains(typedCode(err), string(taxonomy.CodeContractViolation)) {
		t.Fatalf("expected CONTRACT_VIOLATION, got %v", err)
	}
}

func TestDescribe_SubstrateFailurePropagatesEvenWithScopeAll(t *testing.T) {
	f := &describeFake{failAll: true}
	_, err := describe(t, f.server(t), map[string]any{"scope": "all"})
	if !strings.Contains(typedCode(err), string(taxonomy.CodeSubstrateUnavailable)) {
		t.Fatalf("expected SUBSTRATE_UNAVAILABLE, got %v", err)
	}
}

func suggestionOf(t *testing.T, res map[string]any) map[string]any {
	t.Helper()
	e := list(t, res, "entities")
	if len(e) != 1 {
		t.Fatalf("expected one entity, got %v", e)
	}
	s, ok := e[0].(map[string]any)["suggestion"].(map[string]any)
	if !ok {
		t.Fatalf("expected a suggestion on %v", e[0])
	}
	return s
}

func TestDescribe_NamedEntityWithoutSchemaCarriesAnInferredSuggestion(t *testing.T) {
	f := &describeFake{}
	res, err := describe(t, f.server(t), map[string]any{"scope": "entities", "name": "regions"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := suggestionOf(t, res)
	if s["inferred"] != true {
		t.Errorf("a suggestion must be marked as inferred, got %v", s)
	}
	if s["sampled_rows"] != float64(5) || s["total_rows"] != float64(5) {
		t.Errorf("rows sampled and total must be kept, got %v", s)
	}
	schema, _ := s["suggested_schema"].(map[string]any)
	if schema["type"] != "object" || len(list(t, schema, "required")) != 1 {
		t.Errorf("expected the suggested schema, got %v", s)
	}
	if len(list(t, s, "field_analysis")) != 1 {
		t.Errorf("expected the field analysis, got %v", s)
	}
	if _, ok := res["entities"].([]any)[0].(map[string]any)["schema"]; ok {
		t.Error("an inferred suggestion must not be passed off as a registered schema")
	}
}

func TestDescribe_SuggestionIsOnlyRequestedForANamedEntityWithoutSchema(t *testing.T) {
	f := &describeFake{}
	c := f.server(t)

	if _, err := describe(t, c, map[string]any{"scope": "entities"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.called("/api/v1/entity/regions/schema-suggestion") {
		t.Error("without name, no suggestion may be requested: one call per entity")
	}

	res, err := describe(t, c, map[string]any{"scope": "entities", "name": "assets"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := list(t, res, "entities")[0].(map[string]any)["suggestion"]; ok {
		t.Error("an entity with a registered schema needs no suggestion")
	}
	if f.called("/api/v1/entity/assets/schema-suggestion") {
		t.Error("no suggestion may be requested for an entity that has a schema")
	}
}

func TestDescribe_SuggestionThatXoluCannotMakeIsReportedNotFatal(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusBadRequest} {
		f := &describeFake{suggestionStatus: status}
		res, err := describe(t, f.server(t), map[string]any{"scope": "entities", "name": "regions"})
		if err != nil {
			t.Fatalf("status %d: describe must not fail: %v", status, err)
		}
		e := list(t, res, "entities")[0].(map[string]any)
		if msg, _ := e["suggestion_unavailable"].(string); msg == "" {
			t.Errorf("status %d: expected suggestion_unavailable, got %v", status, e)
		}
		if _, ok := e["suggestion"]; ok {
			t.Errorf("status %d: no suggestion expected, got %v", status, e)
		}
	}
}

func TestDescribe_SuggestionSubstrateFailurePropagates(t *testing.T) {
	f := &describeFake{suggestionStatus: http.StatusInternalServerError}
	_, err := describe(t, f.server(t), map[string]any{"scope": "entities", "name": "regions"})
	if !strings.Contains(typedCode(err), string(taxonomy.CodeSubstrateUnavailable)) {
		t.Fatalf("expected SUBSTRATE_UNAVAILABLE, got %v", err)
	}
}

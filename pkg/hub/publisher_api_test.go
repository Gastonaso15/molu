package hub

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

func newAPI() (*PublisherAPI, *Registry) {
	r := NewRegistry()
	return NewPublisherAPI(r), r
}

func violationFields(t *testing.T, err error) []string {
	t.Helper()
	te := requireTyped(t, err, taxonomy.CodeContractViolation)
	fields, _ := te.Details["fields"].([]string)
	return fields
}

const validRegisterParams = `{
	"publisher_id": "pub-1",
	"name": "work_orders.CreateOrder",
	"namespace": "work_orders",
	"description": "creates an order",
	"inputSchema": {"type": "object"},
	"metadata": {"owner": "team-a"}
}`

func TestHandleRegisterStoresTool(t *testing.T) {
	api, r := newAPI()
	if _, err := api.Handle(MethodRegister, json.RawMessage(validRegisterParams)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := r.Lookup("work_orders.CreateOrder")
	if !ok || got.PublisherID != "pub-1" || got.Metadata["owner"] != "team-a" {
		t.Fatalf("tool not stored correctly: %+v ok=%v", got, ok)
	}
}

func TestHandleRegisterIsIdempotent(t *testing.T) {
	api, r := newAPI()
	for i := 0; i < 2; i++ {
		if _, err := api.Handle(MethodRegister, json.RawMessage(validRegisterParams)); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if n := len(r.List("")); n != 1 {
		t.Fatalf("expected 1 tool, got %d", n)
	}
}

func TestHandleRegisterContractViolations(t *testing.T) {
	cases := map[string]struct {
		params string
		fields []string
	}{
		"malformed json":    {`{not json`, []string{"params"}},
		"empty params":      {``, []string{"params"}},
		"null params":       {`null`, []string{"params"}},
		"not an object":     {`[1,2]`, []string{"params"}},
		"unknown field":     {`{"publisher_id":"p","name":"walk","primitive":true,"bogus":1}`, []string{"bogus"}},
		"wrong field type":  {`{"publisher_id":"p","name":"walk","primitive":"yes"}`, []string{"primitive"}},
		"name convention":   {`{"publisher_id":"p","name":"CreateOrder","namespace":"work_orders"}`, []string{"name"}},
		"missing publisher": {`{"name":"walk","primitive":true}`, []string{"publisher_id"}},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			api, r := newAPI()
			_, err := api.Handle(MethodRegister, json.RawMessage(tc.params))
			if got := violationFields(t, err); !reflect.DeepEqual(got, tc.fields) {
				t.Fatalf("expected fields %v, got %v", tc.fields, got)
			}
			if n := len(r.List("")); n != 0 {
				t.Fatalf("nothing should be stored, got %d", n)
			}
		})
	}
}

func TestHandleUnregister(t *testing.T) {
	api, r := newAPI()
	if _, err := api.Handle(MethodRegister, json.RawMessage(validRegisterParams)); err != nil {
		t.Fatal(err)
	}
	params := json.RawMessage(`{"publisher_id":"pub-1","name":"work_orders.CreateOrder"}`)
	if _, err := api.Handle(MethodUnregister, params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.Lookup("work_orders.CreateOrder"); ok {
		t.Fatal("tool should have been removed")
	}
	// Second call: the tool is gone, so NOT_FOUND with the addressed pair.
	_, err := api.Handle(MethodUnregister, params)
	te := requireTyped(t, err, taxonomy.CodeNotFound)
	if te.Details["publisher_id"] != "pub-1" || te.Details["name"] != "work_orders.CreateOrder" {
		t.Fatalf("unexpected details: %v", te.Details)
	}
}

func TestHandleUnregisterContractViolations(t *testing.T) {
	cases := map[string]struct {
		params string
		fields []string
	}{
		"malformed":     {`{`, []string{"params"}},
		"missing both":  {`{}`, []string{"publisher_id", "name"}},
		"missing name":  {`{"publisher_id":"p"}`, []string{"name"}},
		"unknown field": {`{"publisher_id":"p","name":"n","extra":true}`, []string{"extra"}},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			api, _ := newAPI()
			_, err := api.Handle(MethodUnregister, json.RawMessage(tc.params))
			if got := violationFields(t, err); !reflect.DeepEqual(got, tc.fields) {
				t.Fatalf("expected fields %v, got %v", tc.fields, got)
			}
		})
	}
}

func listResult(t *testing.T, res any) []ToolRegistration {
	t.Helper()
	out, ok := res.(ListResult)
	if !ok {
		t.Fatalf("expected ListResult, got %T", res)
	}
	return out.Tools
}

func TestHandleListAdministrativeView(t *testing.T) {
	api, _ := newAPI()
	for _, p := range []string{
		validRegisterParams,
		`{"publisher_id":"pub-2","name":"walk","primitive":true}`,
	} {
		if _, err := api.Handle(MethodRegister, json.RawMessage(p)); err != nil {
			t.Fatal(err)
		}
	}
	for label, params := range map[string]string{"empty": ``, "null": `null`, "empty object": `{}`} {
		res, err := api.Handle(MethodList, json.RawMessage(params))
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if n := len(listResult(t, res)); n != 2 {
			t.Fatalf("%s: expected 2 tools, got %d", label, n)
		}
	}
	res, err := api.Handle(MethodList, json.RawMessage(`{"publisher_id":"pub-2"}`))
	if err != nil {
		t.Fatal(err)
	}
	if tools := listResult(t, res); len(tools) != 1 || tools[0].Name != "walk" {
		t.Fatalf("expected only pub-2's walk, got %+v", tools)
	}
}

func TestHandleListEmptyCatalogIsEmptyArrayNotError(t *testing.T) {
	api, _ := newAPI()
	res, err := api.Handle(MethodList, nil)
	if err != nil {
		t.Fatalf("empty catalog must not be an error: %v", err)
	}
	raw, _ := json.Marshal(res)
	if string(raw) != `{"tools":[]}` {
		t.Fatalf("expected {\"tools\":[]}, got %s", raw)
	}
}

func TestHandleListRejectsUnknownField(t *testing.T) {
	api, _ := newAPI()
	_, err := api.Handle(MethodList, json.RawMessage(`{"tenant":"t"}`))
	if got := violationFields(t, err); !reflect.DeepEqual(got, []string{"tenant"}) {
		t.Fatalf("unexpected fields %v", got)
	}
}

func TestHandleUnknownMethodIsNotOffered(t *testing.T) {
	api, _ := newAPI()
	_, err := api.Handle("registry.drop", nil)
	te := requireTyped(t, err, taxonomy.CodeNotOffered)
	if te.Details["tool"] != "registry.drop" {
		t.Fatalf("unexpected details: %v", te.Details)
	}
}

func TestToolsDescribesPublisherSurface(t *testing.T) {
	api, _ := newAPI()
	tools := api.Tools()
	names := make([]string, len(tools))
	for i, d := range tools {
		names[i] = d.Name
		if d.Description == "" || d.InputSchema["type"] != "object" {
			t.Fatalf("incomplete descriptor: %+v", d)
		}
		if _, err := json.Marshal(d); err != nil {
			t.Fatalf("descriptor must be JSON-serializable: %v", err)
		}
	}
	want := []string{MethodRegister, MethodUnregister, MethodList}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("expected %v, got %v", want, names)
	}
	// Publisher-surface names are administrative domain-free tools, but they
	// must not collide with anything a publisher could register under the
	// primitive rule (no namespace): they all carry the "registry." namespace.
	for _, n := range names {
		if len(n) < 9 || n[:9] != "registry." {
			t.Fatalf("unexpected tool name %q", n)
		}
	}
}

func TestPublisherAPIConcurrentHandle(t *testing.T) {
	api, r := newAPI()
	var wg sync.WaitGroup
	for p := 0; p < 8; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg, _ := json.Marshal(ToolRegistration{
				PublisherID: "pub-" + string(rune('a'+p)),
				Name:        "walk" + string(rune('a'+p)),
				Primitive:   true,
			})
			unreg, _ := json.Marshal(map[string]string{"publisher_id": "pub-" + string(rune('a'+p)), "name": "walk" + string(rune('a'+p))})
			for i := 0; i < 200; i++ {
				if _, err := api.Handle(MethodRegister, reg); err != nil {
					t.Errorf("register: %v", err)
					return
				}
				api.Handle(MethodList, nil)
				if _, err := api.Handle(MethodUnregister, unreg); err != nil {
					t.Errorf("unregister: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if n := len(r.List("")); n != 0 {
		t.Fatalf("expected empty catalog, got %d", n)
	}
}

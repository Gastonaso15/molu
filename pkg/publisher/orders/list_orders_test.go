package orders

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// FindEntities is the in-memory double of Substrate.FindEntities: it applies
// the conditions the way xolu's OQL would (equality on ids and strings, string
// comparison on timestamps), newest first, honouring the limit.
func (f *fakeSubstrate) FindEntities(_ context.Context, entityType string, q EntityQuery) ([]map[string]any, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, doc := range f.entities[entityType] {
		ok := true
		for _, c := range q.Where {
			got := idString(doc[c.Field])
			want := idString(c.Value)
			switch c.Op {
			case "=":
				ok = ok && got == want
			case ">=":
				ok = ok && got != "" && got >= want
			case "<=":
				ok = ok && got != "" && got <= want
			default:
				t := "unsupported op " + c.Op
				panic(t)
			}
		}
		if ok {
			out = append(out, doc)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ci, cj := idString(out[i]["created_at"]), idString(out[j]["created_at"])
		if ci != cj {
			return ci > cj
		}
		return len(idString(out[i]["id"])) > len(idString(out[j]["id"])) ||
			(len(idString(out[i]["id"])) == len(idString(out[j]["id"])) && idString(out[i]["id"]) > idString(out[j]["id"]))
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func seedOrders(fs *fakeSubstrate) {
	fs.put(EntityType, "1", map[string]any{"title": "Legacy", "state": "draft", "machine_id": 1, "created_at": "2026-11-01T10:00:00Z"})
	fs.put(EntityType, "2", map[string]any{
		"title": "Tpl crew", "state": "Pendiente", "workflow_mode": "template", "workflow_index": 0,
		"template_id": 1, "template_version": 3, "assignment_type": AssignmentDirect,
		"responsible_type": "crew", "responsible_id": 7, "assigned_to": "Cuadrilla A",
		"asset_ids": []any{11}, "created_at": "2026-11-05T10:00:00Z",
	})
	fs.put(EntityType, "3", map[string]any{
		"title": "Backlog", "state": "Pendiente", "workflow_mode": "template", "workflow_index": 0,
		"template_id": 1, "template_version": 3, "assignment_type": AssignmentBoard,
		"responsible_type": "crew", "responsible_id": 0, "asset_ids": []any{12}, "created_at": "2026-11-10T10:00:00Z",
	})
	fs.put(EntityType, "4", map[string]any{
		"title": "Claimed", "state": "En curso", "workflow_mode": "template", "workflow_index": 1,
		"template_id": 2, "template_version": 1, "assignment_type": AssignmentBoard,
		"responsible_type": "crew", "responsible_id": 7, "claimed_by_user_id": 9, "asset_ids": []any{11, 12},
		"created_at": "2026-11-12T10:00:00Z",
	})
}

func listIDs(t *testing.T, svc *Service, in ListOrdersInput) OrderList {
	t.Helper()
	out, err := svc.ListOrders(context.Background(), in)
	if err != nil {
		t.Fatalf("ListOrders(%+v): %v", in, err)
	}
	return out
}

func idsOf(l OrderList) string {
	var ids []string
	for _, o := range l.Orders {
		ids = append(ids, o.ID)
	}
	return strings.Join(ids, ",")
}

func TestListOrders_NewestFirstAndTotal(t *testing.T) {
	fs := newFake()
	seedOrders(fs)
	out := listIDs(t, NewService(fs), ListOrdersInput{})
	if idsOf(out) != "4,3,2,1" || out.Total != 4 {
		t.Fatalf("ids=%s total=%d", idsOf(out), out.Total)
	}
}

func TestListOrders_Filters(t *testing.T) {
	fs := newFake()
	seedOrders(fs)
	svc := NewService(fs)
	yes := true
	cases := []struct {
		name string
		in   ListOrdersInput
		want string
	}{
		{"state", ListOrdersInput{State: "Pendiente"}, "3,2"},
		{"template", ListOrdersInput{TemplateID: "1"}, "3,2"},
		{"assignment type", ListOrdersInput{AssignmentType: AssignmentBoard}, "4,3"},
		{"responsible", ListOrdersInput{ResponsibleType: "crew", ResponsibleID: "7"}, "4,2"},
		{"backlog", ListOrdersInput{Backlog: &yes}, "3"},
		{"asset", ListOrdersInput{AssetID: "11"}, "4,2"},
		{"asset other", ListOrdersInput{AssetID: "12"}, "4,3"},
		{"created from", ListOrdersInput{CreatedFrom: "2026-11-10"}, "4,3"},
		{"created to", ListOrdersInput{CreatedTo: "2026-11-05T23:59:59Z"}, "2,1"},
		{"combined", ListOrdersInput{State: "Pendiente", AssetID: "11"}, "2"},
		{"limit", ListOrdersInput{Limit: 2}, "4,3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := idsOf(listIDs(t, svc, c.in)); got != c.want {
				t.Errorf("ids = %s, want %s", got, c.want)
			}
		})
	}
}

func TestListOrders_AssetFilterAppliesBeforeLimit(t *testing.T) {
	fs := newFake()
	seedOrders(fs)
	// The newest order does not carry asset 11; the limit must not hide older matches.
	out := listIDs(t, NewService(fs), ListOrdersInput{AssetID: "11", Limit: 1})
	if idsOf(out) != "4" {
		t.Fatalf("ids = %s", idsOf(out))
	}
	fs.put(EntityType, "5", map[string]any{"title": "Newest", "state": "x", "created_at": "2026-11-20T10:00:00Z", "asset_ids": []any{99}})
	out = listIDs(t, NewService(fs), ListOrdersInput{AssetID: "11", Limit: 1})
	if idsOf(out) != "4" || out.Total != 1 {
		t.Fatalf("ids = %s total=%d", idsOf(out), out.Total)
	}
}

func TestListOrders_SummaryFields(t *testing.T) {
	fs := newFake()
	seedOrders(fs)
	svc := NewService(fs)
	yes := true
	out := listIDs(t, svc, ListOrdersInput{Backlog: &yes})
	o := out.Orders[0]
	if !o.AwaitingClaim || o.CreatedAt != "2026-11-10T10:00:00Z" || o.TemplateID != "1" || o.Title != "Backlog" {
		t.Fatalf("summary = %+v", o)
	}
	claimed := listIDs(t, svc, ListOrdersInput{State: "En curso"}).Orders[0]
	if claimed.AwaitingClaim || claimed.ResponsibleID != "7" {
		t.Fatalf("claimed = %+v", claimed)
	}
}

func TestListOrders_EmptyAndEntityNotYetCreated(t *testing.T) {
	svc := NewService(newFake())
	out := listIDs(t, svc, ListOrdersInput{})
	if out.Total != 0 || out.Orders == nil || len(out.Orders) != 0 {
		t.Fatalf("empty = %+v", out)
	}
	fs := newFake()
	fs.failWith = &XoluError{Status: 400, Code: "XOLU-QL004", Message: "entity 'work_orders' does not exist"}
	out = listIDs(t, NewService(fs), ListOrdersInput{})
	if out.Total != 0 || out.Orders == nil {
		t.Fatalf("missing entity = %+v", out)
	}
}

func TestListOrders_Validation(t *testing.T) {
	svc := NewService(newFake())
	no := false
	yes := true
	cases := map[string]ListOrdersInput{
		"limit":           {Limit: 1000},
		"created_from":    {CreatedFrom: "yesterday"},
		"created_to":      {CreatedTo: "2026-13-45"},
		"backlog":         {Backlog: &yes, AssignmentType: AssignmentDirect},
		"responsible_id":  {ResponsibleID: "7"}, // needs responsible_type
		"responsible_typ": {ResponsibleType: "robot"},
		"backlog false":   {Backlog: &no, ResponsibleType: "bogus"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.ListOrders(context.Background(), in); err == nil {
				t.Fatalf("want ValidationError for %+v", in)
			}
		})
	}
}

func TestListOrders_SubstrateDown(t *testing.T) {
	fs := newFake()
	fs.failWith = &XoluError{Status: 503, Message: "down"}
	_, err := NewService(fs).ListOrders(context.Background(), ListOrdersInput{})
	if _, ok := err.(*UnavailableError); !ok {
		t.Fatalf("err = %T %v", err, err)
	}
}

func TestOrderServer_List(t *testing.T) {
	fs := newFake()
	seedOrders(fs)
	srv := newTestServer(fs)
	defer srv.Close()
	resp, out := post(t, srv.URL+"/api/v1/orders/list", testToken, `{"backlog":true}`)
	if resp.StatusCode != 200 || out["total"].(float64) != 1 {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	first := out["orders"].([]any)[0].(map[string]any)
	if first["id"] != "3" || first["awaiting_claim"] != true {
		t.Fatalf("order = %v", first)
	}
	resp, out = post(t, srv.URL+"/api/v1/orders/list", testToken, `{"limit":9999}`)
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d %v", resp.StatusCode, out)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/list", "", `{}`)
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	empty := newTestServer(newFake())
	defer empty.Close()
	_, out = post(t, empty.URL+"/api/v1/orders/list", testToken, `{}`)
	if out["total"].(float64) != 0 {
		t.Fatalf("empty = %v", out)
	}
}

func TestHTTPSubstrate_FindEntitiesBuildsOQL(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		query = body["query"]
		_, _ = w.Write([]byte(`{"data":[{"id":3,"title":"A"}]}`))
	}))
	defer srv.Close()
	h := NewHTTPSubstrate(srv.URL, "", 0)
	rows, err := h.FindEntities(context.Background(), EntityType, EntityQuery{
		Where: []Condition{
			{Field: "state", Op: "=", Value: "O'Brien"},
			{Field: "responsible_id", Op: "=", Value: int64(7)},
			{Field: "created_at", Op: ">=", Value: "2026-11-01T00:00:00Z"},
		},
		Limit: 25,
	})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	want := "SELECT TOP 25 * FROM work_orders WHERE state = 'O''Brien' AND responsible_id = 7 AND created_at >= '2026-11-01T00:00:00Z' ORDER BY created_at DESC, id DESC"
	if query != want {
		t.Errorf("query =\n %s\nwant\n %s", query, want)
	}
	// No conditions: no WHERE clause.
	if _, err := h.FindEntities(context.Background(), EntityType, EntityQuery{Limit: 5}); err != nil {
		t.Fatal(err)
	}
	if query != "SELECT TOP 5 * FROM work_orders ORDER BY created_at DESC, id DESC" {
		t.Errorf("query = %s", query)
	}
}

func TestHTTPSubstrate_FindEntitiesRejectsUnsafeNames(t *testing.T) {
	h := NewHTTPSubstrate("http://127.0.0.1:1", "", 0)
	_, err := h.FindEntities(context.Background(), EntityType, EntityQuery{Where: []Condition{{Field: "state; DROP", Op: "=", Value: "x"}}})
	if err == nil || strings.Contains(err.Error(), "refused") {
		t.Fatalf("err = %v, want a validation error before any request", err)
	}
	_, err = h.FindEntities(context.Background(), EntityType, EntityQuery{Where: []Condition{{Field: "state", Op: "LIKE", Value: "x"}}})
	if err == nil || strings.Contains(err.Error(), "refused") {
		t.Fatalf("err = %v", err)
	}
}

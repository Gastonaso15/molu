package orders

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func seedPeople(fs *fakeSubstrate) {
	fs.put("crews", "1", map[string]any{"name": "Electricistas", "description": "Mantenimiento electrico", "responsible_user_id": 10, "members": []any{10, 11, 12}})
	fs.put("crews", "2", map[string]any{"name": "Sanitarios", "description": "", "responsible_user_id": 20, "members": []any{12}})
	fs.put("crews", "3", map[string]any{"name": "Albañiles"}) // no responsible, no members
	fs.put("users", "10", map[string]any{"name": "Ana", "email": "ana@x", "password_hash": "SECRET-HASH"})
	fs.put("users", "11", map[string]any{"name": "Beto", "password_hash": "SECRET-HASH"})
	fs.put("users", "20", map[string]any{"name": "Carla", "password_hash": "SECRET-HASH"})
	// user 12 does not exist in xolu

	fs.put("outsourced", "5", map[string]any{
		"name": "Tercero SA", "description": "Pintura", "responsible_user_id": 20,
		"signature": "data:image/png;base64,AAAA", "signature_clarification": "Juan Perez, apoderado",
	})
	fs.put("outsourced", "6", map[string]any{"name": "Acme", "description": "Limpieza"})
}

func crewNames(l CrewList) string {
	var n []string
	for _, c := range l.Crews {
		n = append(n, c.Name)
	}
	return strings.Join(n, ",")
}

func TestListCrews_SortedByNameWithFilters(t *testing.T) {
	fs := newFake()
	seedPeople(fs)
	svc := NewService(fs)
	ctx := context.Background()
	cases := []struct {
		name string
		in   ListCrewsInput
		want string
	}{
		{"all", ListCrewsInput{}, "Albañiles,Electricistas,Sanitarios"},
		{"name contains, case-insensitive", ListCrewsInput{Name: "ELEC"}, "Electricistas"},
		{"member", ListCrewsInput{MemberUserID: "12"}, "Electricistas,Sanitarios"},
		{"responsible", ListCrewsInput{ResponsibleUserID: "20"}, "Sanitarios"},
		{"combined", ListCrewsInput{MemberUserID: "12", ResponsibleUserID: "10"}, "Electricistas"},
		{"limit", ListCrewsInput{Limit: 2}, "Albañiles,Electricistas"},
		{"no match", ListCrewsInput{Name: "zzz"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := svc.ListCrews(ctx, c.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := crewNames(out); got != c.want || out.Total != len(out.Crews) || out.Crews == nil {
				t.Errorf("crews = %q total=%d, want %q", got, out.Total, c.want)
			}
		})
	}
	out, _ := svc.ListCrews(ctx, ListCrewsInput{Limit: 2})
	if !out.Truncated {
		t.Error("limit 2 of 3 should be truncated")
	}
	out, _ = svc.ListCrews(ctx, ListCrewsInput{})
	e := out.Crews[1]
	if e.ID != "1" || e.Description != "Mantenimiento electrico" || e.Responsible == nil || e.Responsible.UserID != "10" ||
		len(e.Members) != 3 || e.Members[2].UserID != "12" {
		t.Errorf("electricistas = %+v", e)
	}
	// The list does not resolve user names.
	if e.Responsible.Name != "" || e.Members[0].Name != "" {
		t.Errorf("list resolved names: %+v", e)
	}
	if out.Crews[0].Responsible != nil || len(out.Crews[0].Members) != 0 || out.Crews[0].Members == nil {
		t.Errorf("empty crew = %+v", out.Crews[0])
	}
}

func TestListCrews_EmptyMissingEntityAndValidation(t *testing.T) {
	out, err := NewService(newFake()).ListCrews(context.Background(), ListCrewsInput{})
	if err != nil || out.Total != 0 || out.Crews == nil {
		t.Fatalf("empty = %+v err=%v", out, err)
	}
	fs := newFake()
	fs.failWith = &XoluError{Status: 400, Code: "XOLU-QL004", Message: "entity 'crews' does not exist"}
	out, err = NewService(fs).ListCrews(context.Background(), ListCrewsInput{})
	if err != nil || out.Total != 0 {
		t.Fatalf("missing entity = %+v err=%v", out, err)
	}
	for name, in := range map[string]ListCrewsInput{
		"limit": {Limit: 1000}, "member_user_id": {MemberUserID: "abc"}, "responsible_user_id": {ResponsibleUserID: "-1"},
	} {
		_, err := NewService(newFake()).ListCrews(context.Background(), in)
		wantInvalid(t, err, name)
	}
	fs = newFake()
	fs.failWith = &XoluError{Status: 503, Message: "down"}
	_, err = NewService(fs).ListCrews(context.Background(), ListCrewsInput{})
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %T %v", err, err)
	}
}

func TestGetCrew_ResolvesMemberNamesWithoutLeakingUserData(t *testing.T) {
	fs := newFake()
	seedPeople(fs)
	c, err := NewService(fs).GetCrew(context.Background(), GetCrewInput{CrewID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Electricistas" || c.Responsible == nil || c.Responsible.Name != "Ana" ||
		len(c.Members) != 3 || c.Members[0].Name != "Ana" || c.Members[1].Name != "Beto" || c.Members[2].Name != "" {
		t.Fatalf("crew = %+v", c)
	}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), "SECRET-HASH") || strings.Contains(string(raw), "ana@x") {
		t.Fatalf("user data leaked: %s", raw)
	}
}

func TestGetCrew_NotFoundValidationAndMissingUsersEntity(t *testing.T) {
	fs := newFake()
	seedPeople(fs)
	delete(fs.entities, "users")
	c, err := NewService(fs).GetCrew(context.Background(), GetCrewInput{CrewID: "1"})
	if err != nil || c.Members[0].Name != "" || len(c.Members) != 3 {
		t.Fatalf("crew = %+v err=%v", c, err)
	}
	_, err = NewService(fs).GetCrew(context.Background(), GetCrewInput{CrewID: "404"})
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Error() != `crew "404" not found` {
		t.Fatalf("err = %T %v", err, err)
	}
	_, err = NewService(fs).GetCrew(context.Background(), GetCrewInput{})
	wantInvalid(t, err, "crew_id")
}

func TestListAndGetOutsourced(t *testing.T) {
	fs := newFake()
	seedPeople(fs)
	svc := NewService(fs)
	ctx := context.Background()
	l, err := svc.ListOutsourced(ctx, ListOutsourcedInput{})
	if err != nil || l.Total != 2 || l.Outsourced[0].Name != "Acme" || l.Outsourced[1].Name != "Tercero SA" {
		t.Fatalf("list = %+v err=%v", l, err)
	}
	o := l.Outsourced[1]
	if !o.HasSignature || o.SignatureClarification != "Juan Perez, apoderado" || o.Responsible == nil || o.Responsible.UserID != "20" || o.Description != "Pintura" {
		t.Errorf("tercero = %+v", o)
	}
	if l.Outsourced[0].HasSignature || l.Outsourced[0].Responsible != nil {
		t.Errorf("acme = %+v", l.Outsourced[0])
	}
	raw, _ := json.Marshal(l)
	if strings.Contains(string(raw), "base64") {
		t.Fatalf("the signature image must not travel: %s", raw)
	}
	byName, _ := svc.ListOutsourced(ctx, ListOutsourcedInput{Name: "terc"})
	byUser, _ := svc.ListOutsourced(ctx, ListOutsourcedInput{ResponsibleUserID: "20"})
	if byName.Total != 1 || byUser.Total != 1 || byUser.Outsourced[0].ID != "5" {
		t.Errorf("filters: %+v %+v", byName, byUser)
	}
	g, err := svc.GetOutsourced(ctx, GetOutsourcedInput{OutsourcedID: "5"})
	if err != nil || g.Responsible == nil || g.Responsible.Name != "Carla" || !g.HasSignature {
		t.Fatalf("get = %+v err=%v", g, err)
	}
	raw, _ = json.Marshal(g)
	if strings.Contains(string(raw), "SECRET-HASH") || strings.Contains(string(raw), "base64") {
		t.Fatalf("leak: %s", raw)
	}
	_, err = svc.GetOutsourced(ctx, GetOutsourcedInput{OutsourcedID: "404"})
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Error() != `outsourced provider "404" not found` {
		t.Fatalf("err = %T %v", err, err)
	}
	_, err = svc.GetOutsourced(ctx, GetOutsourcedInput{})
	wantInvalid(t, err, "outsourced_id")
	empty, err := NewService(newFake()).ListOutsourced(ctx, ListOutsourcedInput{})
	if err != nil || empty.Total != 0 || empty.Outsourced == nil {
		t.Fatalf("empty = %+v err=%v", empty, err)
	}
}

func TestOrderServer_PeopleRoutes(t *testing.T) {
	fs := newFake()
	seedPeople(fs)
	srv := newTestServer(fs)
	defer srv.Close()
	resp, out := post(t, srv.URL+"/api/v1/orders/crews/list", testToken, `{"member_user_id":12}`)
	if resp.StatusCode != 200 || out["total"].(float64) != 2 {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	resp, out = post(t, srv.URL+"/api/v1/orders/crews/get", testToken, `{"crew_id":1}`)
	if resp.StatusCode != 200 || out["name"] != "Electricistas" {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/crews/get", testToken, `{"crew_id":404}`)
	if resp.StatusCode != 404 {
		t.Errorf("crew 404: %d", resp.StatusCode)
	}
	resp, out = post(t, srv.URL+"/api/v1/orders/outsourced/list", testToken, `{}`)
	if resp.StatusCode != 200 || out["total"].(float64) != 2 {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	resp, out = post(t, srv.URL+"/api/v1/orders/outsourced/get", testToken, `{"outsourced_id":"5"}`)
	if resp.StatusCode != 200 || out["signature_clarification"] != "Juan Perez, apoderado" {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/outsourced/list", "", `{}`)
	if resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}

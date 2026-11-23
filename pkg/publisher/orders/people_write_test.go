package orders

import (
	"context"
	"errors"
	"testing"
)

func strPtr(s string) *string { return &s }
func idPtr(id ID) *ID         { return &id }

func seedPeopleWrite(fs *fakeSubstrate) {
	fs.put(usersEntity, "1", map[string]any{"name": "Ana", "email": "ana@example.com", "password_hash": "hash", "active": true})
	fs.put(usersEntity, "2", map[string]any{"name": "Luis", "active": true})
	fs.put(usersEntity, "3", map[string]any{"name": "Sofia", "active": true})
	fs.put(usersEntity, "4", map[string]any{"name": "Inactivo", "active": false})
}

func TestCreateCrew_SuccessDeduplicatesAndIncludesResponsible(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedPeopleWrite(fs)
	svc := NewService(fs)

	// Responsible (3) is not in members [1, 2, 1]: duplicates are collapsed and
	// responsible is automatically added (matching the UI rule).
	crew, err := svc.CreateCrew(ctx, CreateCrewInput{
		Name:              "Electricistas",
		Description:       "Turno noche",
		ResponsibleUserID: "3",
		MemberUserIDs:     []ID{"1", "2", "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if crew.ID == "" || crew.Name != "Electricistas" || crew.Description != "Turno noche" {
		t.Fatalf("crew = %+v", crew)
	}
	if crew.Responsible == nil || crew.Responsible.UserID != "3" || crew.Responsible.Name != "Sofia" {
		t.Fatalf("responsible = %+v", crew.Responsible)
	}
	if len(crew.Members) != 3 || crew.Members[0].Name != "Ana" || crew.Members[1].Name != "Luis" || crew.Members[2].Name != "Sofia" {
		t.Fatalf("members = %+v", crew.Members)
	}

	doc := fs.entities[crewEntity][crew.ID]
	if doc["name"] != "Electricistas" || intOf(doc["responsible_user_id"]) != 3 {
		t.Fatalf("stored crew = %v", doc)
	}
	if got := idList(doc["members"]); len(got) != 3 || got[0] != "1" || got[1] != "2" || got[2] != "3" {
		t.Fatalf("stored members = %v", doc["members"])
	}
}

func TestCreateCrew_Validation(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedPeopleWrite(fs)
	svc := NewService(fs)

	cases := []struct {
		name  string
		in    CreateCrewInput
		field string
	}{
		{"empty name", CreateCrewInput{Name: "  ", ResponsibleUserID: "1", MemberUserIDs: []ID{"1", "2"}}, "name"},
		{"missing responsible", CreateCrewInput{Name: "C", MemberUserIDs: []ID{"1", "2"}}, "responsible_user_id"},
		{"unknown responsible", CreateCrewInput{Name: "C", ResponsibleUserID: "99", MemberUserIDs: []ID{"1", "2"}}, "responsible_user_id"},
		{"inactive responsible", CreateCrewInput{Name: "C", ResponsibleUserID: "4", MemberUserIDs: []ID{"1", "2"}}, "responsible_user_id"},
		{"less than two members", CreateCrewInput{Name: "C", ResponsibleUserID: "1", MemberUserIDs: []ID{"1"}}, "member_user_ids"},
		{"duplicates collapse below two", CreateCrewInput{Name: "C", ResponsibleUserID: "1", MemberUserIDs: []ID{"2", "2"}}, "member_user_ids"},
		{"bad member id", CreateCrewInput{Name: "C", ResponsibleUserID: "1", MemberUserIDs: []ID{"1", "abc"}}, "member_user_ids"},
		{"unknown member", CreateCrewInput{Name: "C", ResponsibleUserID: "1", MemberUserIDs: []ID{"1", "99"}}, "member_user_ids"},
		{"inactive member", CreateCrewInput{Name: "C", ResponsibleUserID: "1", MemberUserIDs: []ID{"1", "4"}}, "member_user_ids"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateCrew(ctx, tc.in)
			wantInvalid(t, err, tc.field)
		})
	}
}

func TestUpdateCrew_PartialFullAndErrors(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedPeopleWrite(fs)
	fs.put(crewEntity, "7", map[string]any{
		"name": "Inicial", "description": "d", "responsible_user_id": int64(1),
		"members": []any{int64(1), int64(2)}, "_version": int64(1),
	})
	svc := NewService(fs)

	// Partial update: rename + switch responsible to 3 (must auto-add 3 to existing members).
	updated, err := svc.UpdateCrew(ctx, UpdateCrewInput{
		CrewID:            "7",
		Name:              strPtr("Renombrada"),
		ResponsibleUserID: idPtr("3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Renombrada" || updated.Description != "d" || updated.Responsible.UserID != "3" || len(updated.Members) != 3 {
		t.Fatalf("updated = %+v", updated)
	}

	// Explicit member_user_ids with <2 distinct users is rejected.
	tooFew := []ID{"1"}
	_, err = svc.UpdateCrew(ctx, UpdateCrewInput{CrewID: "7", MemberUserIDs: &tooFew})
	wantInvalid(t, err, "member_user_ids")

	// Empty update is rejected.
	_, err = svc.UpdateCrew(ctx, UpdateCrewInput{CrewID: "7"})
	wantInvalid(t, err, "crew_id")

	// Unknown crew -> NotFoundError.
	_, err = svc.UpdateCrew(ctx, UpdateCrewInput{CrewID: "999", Name: strPtr("X")})
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Kind != "crew" {
		t.Fatalf("want NotFoundError(crew), got %T %v", err, err)
	}

	// Version conflict -> stale_state.
	fs.patchErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "conflict"}
	_, err = svc.UpdateCrew(ctx, UpdateCrewInput{CrewID: "7", Description: strPtr("nueva")})
	wantRule(t, err, "stale_state")
}

func TestCreateOutsourced_SuccessAndValidation(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedPeopleWrite(fs)
	svc := NewService(fs)

	o1, err := svc.CreateOutsourced(ctx, CreateOutsourcedInput{
		Name:                   "Elevadores SA",
		Description:            "Mantenimiento ascensores",
		ResponsibleUserID:      "1",
		Signature:              "data:image/png;base64,AAAA",
		SignatureClarification: "Ing. Perez",
	})
	if err != nil {
		t.Fatal(err)
	}
	if o1.ID == "" || o1.Name != "Elevadores SA" || !o1.HasSignature || o1.SignatureClarification != "Ing. Perez" ||
		o1.Responsible == nil || o1.Responsible.UserID != "1" || o1.Responsible.Name != "Ana" {
		t.Fatalf("outsourced = %+v", o1)
	}
	if fs.entities[outsourcedEntity][o1.ID]["signature"] != "data:image/png;base64,AAAA" {
		t.Fatalf("signature not stored: %v", fs.entities[outsourcedEntity][o1.ID])
	}

	// Without responsible user is also valid.
	o2, err := svc.CreateOutsourced(ctx, CreateOutsourcedInput{Name: "Sin cuenta"})
	if err != nil || o2.Responsible != nil || o2.HasSignature {
		t.Fatalf("o2 = %+v err=%v", o2, err)
	}

	// Validation
	_, err = svc.CreateOutsourced(ctx, CreateOutsourcedInput{Name: "  "})
	wantInvalid(t, err, "name")
	_, err = svc.CreateOutsourced(ctx, CreateOutsourcedInput{Name: "X", ResponsibleUserID: "99"})
	wantInvalid(t, err, "responsible_user_id")
	_, err = svc.CreateOutsourced(ctx, CreateOutsourcedInput{Name: "X", ResponsibleUserID: "4"})
	wantInvalid(t, err, "responsible_user_id")
}

func TestUpdateOutsourced_LinkUnlinkAndErrors(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedPeopleWrite(fs)
	fs.put(outsourcedEntity, "5", map[string]any{
		"name": "Viejo", "description": "d", "responsible_user_id": int64(1),
		"signature": "sig", "signature_clarification": "A", "_version": int64(1),
	})
	svc := NewService(fs)

	// Update name and unlink representative account via "".
	u1, err := svc.UpdateOutsourced(ctx, UpdateOutsourcedInput{
		OutsourcedID:      "5",
		Name:              strPtr("Nuevo SA"),
		ResponsibleUserID: idPtr(""),
	})
	if err != nil || u1.Name != "Nuevo SA" || u1.Responsible != nil || !u1.HasSignature {
		t.Fatalf("u1 = %+v err=%v", u1, err)
	}
	if intOf(fs.entities[outsourcedEntity]["5"]["responsible_user_id"]) != 0 {
		t.Fatalf("responsible_user_id should be 0: %v", fs.entities[outsourcedEntity]["5"])
	}

	// Link representative account 2.
	u2, err := svc.UpdateOutsourced(ctx, UpdateOutsourcedInput{
		OutsourcedID:      "5",
		ResponsibleUserID: idPtr("2"),
	})
	if err != nil || u2.Responsible == nil || u2.Responsible.UserID != "2" || u2.Responsible.Name != "Luis" {
		t.Fatalf("u2 = %+v err=%v", u2, err)
	}

	// Empty update rejected.
	_, err = svc.UpdateOutsourced(ctx, UpdateOutsourcedInput{OutsourcedID: "5"})
	wantInvalid(t, err, "outsourced_id")

	// Missing outsourced -> NotFoundError.
	_, err = svc.UpdateOutsourced(ctx, UpdateOutsourcedInput{OutsourcedID: "999", Name: strPtr("X")})
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Kind != "outsourced provider" {
		t.Fatalf("want NotFoundError(outsourced provider), got %T %v", err, err)
	}

	// Version conflict -> stale_state.
	fs.patchErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "conflict"}
	_, err = svc.UpdateOutsourced(ctx, UpdateOutsourcedInput{OutsourcedID: "5", Description: strPtr("otra")})
	wantRule(t, err, "stale_state")
}

func TestOrderServer_PeopleWrite(t *testing.T) {
	fs := newFake()
	seedPeopleWrite(fs)
	srv := newTestServer(fs)
	defer srv.Close()

	// CreateCrew
	resp, out := post(t, srv.URL+"/api/v1/orders/crews/create", testToken,
		`{"name":"Plomeros","responsible_user_id":1,"member_user_ids":[1,2]}`)
	if resp.StatusCode != 200 || out["name"] != "Plomeros" {
		t.Fatalf("create crew: %d %v", resp.StatusCode, out)
	}
	crewID := out["id"].(string)

	// UpdateCrew
	resp, out = post(t, srv.URL+"/api/v1/orders/crews/update", testToken,
		`{"crew_id":"`+crewID+`","description":"Guardia"}`)
	if resp.StatusCode != 200 || out["description"] != "Guardia" {
		t.Fatalf("update crew: %d %v", resp.StatusCode, out)
	}

	// CreateOutsourced
	resp, out = post(t, srv.URL+"/api/v1/orders/outsourced/create", testToken,
		`{"name":"Clima SRL","responsible_user_id":2}`)
	if resp.StatusCode != 200 || out["name"] != "Clima SRL" {
		t.Fatalf("create outsourced: %d %v", resp.StatusCode, out)
	}
	outID := out["id"].(string)

	// UpdateOutsourced
	resp, out = post(t, srv.URL+"/api/v1/orders/outsourced/update", testToken,
		`{"outsourced_id":"`+outID+`","signature_clarification":"Sr. Gomez"}`)
	if resp.StatusCode != 200 || out["signature_clarification"] != "Sr. Gomez" {
		t.Fatalf("update outsourced: %d %v", resp.StatusCode, out)
	}

	// Not found / unauthorized
	resp, _ = post(t, srv.URL+"/api/v1/orders/crews/update", testToken, `{"crew_id":999,"name":"X"}`)
	if resp.StatusCode != 404 {
		t.Errorf("missing crew: %d", resp.StatusCode)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/outsourced/create", "", `{}`)
	if resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}
